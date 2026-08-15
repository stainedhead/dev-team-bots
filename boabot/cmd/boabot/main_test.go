package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/team"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	buzzinfra "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/buzz"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret"
)

// fakeSecretStore is a minimal domain.SecretStore test double.
type fakeSecretStore struct {
	values map[domain.SecretRef]string
}

func (f *fakeSecretStore) Get(_ context.Context, ref domain.SecretRef) (string, error) {
	if v, ok := f.values[ref]; ok {
		return v, nil
	}
	return "", errors.New("not found")
}

// TestApplyCredentialFromStore_SetsEnvOnHit verifies that a resolved secret
// is applied to the named environment variable.
func TestApplyCredentialFromStore_SetsEnvOnHit(t *testing.T) {
	store := &fakeSecretStore{values: map[domain.SecretRef]string{
		{Name: "anthropic_api_key"}: "sk-from-store",
	}}
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	t.Cleanup(func() { _ = os.Unsetenv("ANTHROPIC_API_KEY") })

	applyCredentialFromStore(context.Background(), store, "anthropic_api_key", "ANTHROPIC_API_KEY")

	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "sk-from-store" {
		t.Errorf("ANTHROPIC_API_KEY: got %q, want sk-from-store", got)
	}
}

// TestApplyCredentialFromStore_LeavesEnvUnchangedOnMiss verifies that a
// SecretStore miss is a no-op (FR-046: no observable behaviour change).
func TestApplyCredentialFromStore_LeavesEnvUnchangedOnMiss(t *testing.T) {
	store := &fakeSecretStore{values: map[domain.SecretRef]string{}}
	_ = os.Unsetenv("BOABOT_BACKUP_TOKEN")

	applyCredentialFromStore(context.Background(), store, "boabot_backup_token", "BOABOT_BACKUP_TOKEN")

	if got := os.Getenv("BOABOT_BACKUP_TOKEN"); got != "" {
		t.Errorf("BOABOT_BACKUP_TOKEN: got %q, want empty", got)
	}
}

// TestResolveEnvCredentials_CallsBothSecrets verifies that both migrated
// applyCredential call sites (ANTHROPIC_API_KEY, BOABOT_BACKUP_TOKEN) now go
// through SecretStore.Get (FR-046).
func TestResolveEnvCredentials_CallsBothSecrets(t *testing.T) {
	store := &fakeSecretStore{values: map[domain.SecretRef]string{
		{Name: "anthropic_api_key"}:   "sk-x",
		{Name: "boabot_backup_token"}: "ghp-x",
	}}
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("BOABOT_BACKUP_TOKEN")
	t.Cleanup(func() {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("BOABOT_BACKUP_TOKEN")
	})

	resolveEnvCredentials(context.Background(), store)

	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "sk-x" {
		t.Errorf("ANTHROPIC_API_KEY: got %q, want sk-x", got)
	}
	if got := os.Getenv("BOABOT_BACKUP_TOKEN"); got != "ghp-x" {
		t.Errorf("BOABOT_BACKUP_TOKEN: got %q, want ghp-x", got)
	}
}

// TestBuildSecretProviders_DefaultOrder verifies the default chain order
// (FR-040): env, systemd, keystore, file — with a resolvable credentials
// path and no world-readable file present.
func TestBuildSecretProviders_DefaultOrder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	providers, err := buildSecretProviders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"env", "systemd", "keystore", "file"}
	if len(providers) != len(want) {
		t.Fatalf("got %d providers, want %d", len(providers), len(want))
	}
	for i, p := range providers {
		if p.Name() != want[i] {
			t.Errorf("provider[%d]: got %q, want %q", i, p.Name(), want[i])
		}
	}
}

// TestBuildSecretProviders_WorldReadableCredentialsFileIsFatal verifies
// FR-046's "no observable behaviour change" AC: a world-readable
// ~/.boabot/credentials remains fatal, with the pre-existing error message
// preserved.
func TestBuildSecretProviders_WorldReadableCredentialsFileIsFatal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	credDir := filepath.Join(dir, ".boabot")
	if err := os.MkdirAll(credDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	credPath := filepath.Join(credDir, "credentials")
	if err := os.WriteFile(credPath, []byte("[default]\nanthropic_api_key=x\n"), 0644); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	_, err := buildSecretProviders()
	if err == nil {
		t.Fatal("expected an error for a world-readable credentials file, got nil")
	}
	got := err.Error()
	if !strings.Contains(got, "world-readable") || !strings.Contains(got, "chmod 600") {
		t.Errorf("error message: got %q, want it to preserve the existing world-readable/chmod hint", got)
	}
}

// TestEndToEnd_EnvOnlyDeployment exercises the real provider chain (not a
// fake) for the FR-046 PRD acceptance criterion: "an existing deployment
// using only env vars ... starts with a byte-identical config to before
// this change." A fake domain.SecretStore would bypass the two seams most
// likely to silently break this (env.Provider's ref-name-to-env-var-name
// derivation, and the historical env-var names themselves), so this test
// goes through buildSecretProviders + secret.New + resolveEnvCredentials
// exactly as run() does.
func TestEndToEnd_EnvOnlyDeployment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // no ~/.boabot/credentials present under here
	t.Setenv("ANTHROPIC_API_KEY", "sk-env")
	_ = os.Unsetenv("BOABOT_BACKUP_TOKEN")
	t.Cleanup(func() { _ = os.Unsetenv("BOABOT_BACKUP_TOKEN") })

	providers, err := buildSecretProviders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolveEnvCredentials(context.Background(), secret.New(providers))

	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "sk-env" {
		t.Errorf("ANTHROPIC_API_KEY: got %q, want sk-env (env var must be left exactly as set)", got)
	}
	if got := os.Getenv("BOABOT_BACKUP_TOKEN"); got != "" {
		t.Errorf("BOABOT_BACKUP_TOKEN: got %q, want empty (no source configures it)", got)
	}
}

// TestEndToEnd_CredentialsFileOnlyDeployment exercises the real provider
// chain for the FR-046 PRD acceptance criterion's second half: "one using
// only ~/.boabot/credentials ... starts with a byte-identical config to
// before this change." This asserts the file provider's key convention
// (bare ref.Name when Bot is empty) still matches the credentials-file keys
// the pre-migration applyCredential(creds, "anthropic_api_key", ...) call
// used to read.
func TestEndToEnd_CredentialsFileOnlyDeployment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("BOABOT_BACKUP_TOKEN")
	t.Cleanup(func() {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("BOABOT_BACKUP_TOKEN")
	})

	credDir := filepath.Join(dir, ".boabot")
	if err := os.MkdirAll(credDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	credPath := filepath.Join(credDir, "credentials")
	content := "[default]\nanthropic_api_key=sk-file\nboabot_backup_token=ghp-file\n"
	if err := os.WriteFile(credPath, []byte(content), 0600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	providers, err := buildSecretProviders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolveEnvCredentials(context.Background(), secret.New(providers))

	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "sk-file" {
		t.Errorf("ANTHROPIC_API_KEY: got %q, want sk-file", got)
	}
	if got := os.Getenv("BOABOT_BACKUP_TOKEN"); got != "ghp-file" {
		t.Errorf("BOABOT_BACKUP_TOKEN: got %q, want ghp-file", got)
	}
}

// buzzFakeStore is a domain.SecretStore test double for the buildBuzzMonitor
// tests below: it resolves a fixed set of refs, records every ref it was
// asked to resolve (so a test can assert exactly zero calls happened), and
// reports an all-miss lookup as a *secret.NotFoundError so LoadAPIToken's
// errors.As-based clean-miss distinction is exercised faithfully rather
// than through a generic error.
type buzzFakeStore struct {
	values map[domain.SecretRef]string
	calls  []domain.SecretRef
}

func (f *buzzFakeStore) Get(_ context.Context, ref domain.SecretRef) (string, error) {
	f.calls = append(f.calls, ref)
	if v, ok := f.values[ref]; ok {
		return v, nil
	}
	return "", &secret.NotFoundError{Ref: ref}
}

func genTestKeypair(t *testing.T) (nostr.SecretKey, string) {
	t.Helper()
	sk := nostr.Generate()
	return sk, nip19.EncodeNsec(sk)
}

// TestBuildBuzzMonitor_Disabled verifies FR-036: with buzz.enabled: false,
// buildBuzzMonitor returns nil and never calls SecretStore.Get at all --
// the concrete evidence that no Nostr code path executes and no relay
// connection is attempted.
func TestBuildBuzzMonitor_Disabled(t *testing.T) {
	store := &buzzFakeStore{values: map[domain.SecretRef]string{}}
	cfg := config.Config{Buzz: config.BuzzConfig{Enabled: false, BotName: "buzzbot", RelayURL: "wss://relay.example.com"}}

	mon := buildBuzzMonitor(context.Background(), cfg, store, queue.NewRouter(), "", t.TempDir(), nil, nil, nil)

	if mon != nil {
		t.Fatal("expected nil Monitor when buzz.enabled is false")
	}
	if len(store.calls) != 0 {
		t.Errorf("expected zero SecretStore.Get calls when Buzz is disabled, got %d: %v", len(store.calls), store.calls)
	}
}

// TestBuildBuzzMonitor_MissingBotNameOrRelayURL verifies buildBuzzMonitor
// refuses to activate (and never touches the SecretStore) when Buzz is
// enabled but bot_name or relay_url is missing.
func TestBuildBuzzMonitor_MissingBotNameOrRelayURL(t *testing.T) {
	cases := []config.BuzzConfig{
		{Enabled: true, RelayURL: "wss://relay.example.com"}, // missing BotName
		{Enabled: true, BotName: "buzzbot"},                  // missing RelayURL
	}
	for _, bc := range cases {
		store := &buzzFakeStore{values: map[domain.SecretRef]string{}}
		cfg := config.Config{Buzz: bc}

		mon := buildBuzzMonitor(context.Background(), cfg, store, queue.NewRouter(), "", t.TempDir(), nil, nil, nil)

		if mon != nil {
			t.Fatalf("expected nil Monitor for incomplete BuzzConfig %+v", bc)
		}
		if len(store.calls) != 0 {
			t.Errorf("expected zero SecretStore.Get calls for incomplete BuzzConfig %+v, got %d", bc, len(store.calls))
		}
	}
}

// TestBuildBuzzMonitor_KeypairLoadFailure verifies FR-003's fail-closed
// behaviour: a SecretStore miss on the private key returns a nil Monitor
// (Buzz declines to start) rather than an error or a panic, so the caller
// in run() can log it and let every other channel/bot keep starting.
func TestBuildBuzzMonitor_KeypairLoadFailure(t *testing.T) {
	store := &buzzFakeStore{values: map[domain.SecretRef]string{}} // no private key configured
	cfg := config.Config{Buzz: config.BuzzConfig{Enabled: true, BotName: "buzzbot", RelayURL: "wss://relay.example.com"}}

	mon := buildBuzzMonitor(context.Background(), cfg, store, queue.NewRouter(), "", t.TempDir(), nil, nil, nil)

	if mon != nil {
		t.Fatal("expected nil Monitor when the private key fails to resolve")
	}
}

// TestNewBuzzMonitorBuilder_KeyLoadFailure_ReturnsTrueNilInterface guards
// against a Go typed-nil-interface pitfall: newBuzzMonitorBuilder's closure
// returns domain.ChannelMonitor (an interface), but internally calls
// buildBuzzMonitor, which returns *buzzinfra.Monitor (a concrete pointer). A
// bare `return buildBuzzMonitor(...)` converts a nil *buzzinfra.Monitor into
// a *non-nil* domain.ChannelMonitor interface value (type=*buzzinfra.Monitor,
// value=nil) -- `mon == nil` at the TeamManager.Run() call site then never
// catches it, so a monitor for a persona whose key failed to load is
// registered anyway and panics with a nil-pointer dereference the first
// time anything calls .Start() on it (observed in production: every bot in
// the team crash-loops indefinitely, not just the affected persona, because
// they all share TeamManager's monitor list).
func TestNewBuzzMonitorBuilder_KeyLoadFailure_ReturnsTrueNilInterface(t *testing.T) {
	store := &buzzFakeStore{values: map[domain.SecretRef]string{}} // no private key configured
	cfg := config.Config{Buzz: config.BuzzConfig{Enabled: true, BotName: "buzzbot", RelayURL: "wss://relay.example.com"}}
	builder := newBuzzMonitorBuilder(store, "", t.TempDir())

	mon := builder(context.Background(), team.BotEntry{Name: "buzzbot", Type: "buzzbot", Enabled: true}, cfg, queue.NewRouter(), nil, nil, nil, nil)

	if mon != nil {
		t.Fatalf("expected a true nil domain.ChannelMonitor interface when the private key fails to resolve, got a non-nil interface wrapping %#v (this is the typed-nil pitfall the comment above describes)", mon)
	}
}

// TestBuildBuzzMonitor_EnabledSuccess verifies the happy path: a valid
// private key and required settings produce a non-nil Monitor, and the
// target bot's queue is registered with the router.
func TestBuildBuzzMonitor_EnabledSuccess(t *testing.T) {
	_, nsec := genTestKeypair(t)
	store := &buzzFakeStore{values: map[domain.SecretRef]string{
		{Name: "buzz_private_key", Bot: "buzzbot"}: nsec,
	}}
	cfg := config.Config{
		Bot: config.BotConfig{Name: "buzzbot", BotType: "tech-lead"},
		Buzz: config.BuzzConfig{
			Enabled:  true,
			BotName:  "buzzbot",
			RelayURL: "wss://relay.example.com",
		},
	}
	router := queue.NewRouter()

	mon := buildBuzzMonitor(context.Background(), cfg, store, router, t.TempDir(), t.TempDir(), nil, nil, nil)

	if mon == nil {
		t.Fatal("expected a non-nil Monitor for a valid, enabled BuzzConfig")
	}
	// router.QueueFor panics if the bot was never registered -- calling it
	// here proves buildBuzzMonitor registered the queue.
	if router.QueueFor("buzzbot") == nil {
		t.Error("expected buzzbot's queue to be registered")
	}
}

// TestBuildBuzzMonitor_QueueAlreadyRegistered_DoesNotDoubleRegister
// verifies that when Slack already registered the same bot name,
// buildBuzzMonitor does not call router.Register again -- Router.Register
// panics on a duplicate name, so a double-registration would crash the
// whole process rather than just skipping Buzz.
func TestBuildBuzzMonitor_QueueAlreadyRegistered_DoesNotDoubleRegister(t *testing.T) {
	_, nsec := genTestKeypair(t)
	store := &buzzFakeStore{values: map[domain.SecretRef]string{
		{Name: "buzz_private_key", Bot: "sharedbot"}: nsec,
	}}
	cfg := config.Config{
		Buzz: config.BuzzConfig{Enabled: true, BotName: "sharedbot", RelayURL: "wss://relay.example.com"},
	}
	router := queue.NewRouter()
	router.Register("sharedbot", 0) // simulate the Slack block having already registered it

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("buildBuzzMonitor must not double-register an already-registered queue, got panic: %v", r)
		}
	}()

	mon := buildBuzzMonitor(context.Background(), cfg, store, router, "", t.TempDir(), nil, nil, nil)
	if mon == nil {
		t.Fatal("expected a non-nil Monitor")
	}
}

// --- FR-001 (WS-A): NIP-OA auth-tag wiring ---------------------------------

// containsSecretRef reports whether calls includes ref -- used to prove
// buildBuzzMonitor actually asked the SecretStore for a given secret,
// which is the concrete evidence that FR-001's wiring gap existed (nothing
// in main.go ever requested buzz_auth_tag) and, once fixed, that it now
// does.
func containsSecretRef(calls []domain.SecretRef, ref domain.SecretRef) bool {
	for _, c := range calls {
		if c == ref {
			return true
		}
	}
	return false
}

// TestBuildBuzzMonitor_AuthTagSecretIsResolved is WS-A1's Red test for
// FR-001: "the feature's headline capability -- a NIP-OA owner-attested
// agent joining a channel without being explicitly enrolled -- cannot work
// in the shipped binary" because buildBuzzMonitor never asks the
// SecretStore for an auth-tag secret at all. This test configures a
// resolvable, well-formed auth-tag secret (a real owner-signed NIP-OA tag,
// pipe-delimited per research.md's OQ-R1 resolution) and asserts
// buildBuzzMonitor's SecretStore lookups include a request for it -- using
// the literal secret name "buzz_auth_tag" rather than a
// buzzinfra.AuthTagSecretName constant, since that constant does not exist
// yet on current HEAD (it is added in WS-A2's Green step). This fails
// today: buildBuzzMonitor's opts slice only ever appends WithLogger,
// WithProfile, and conditionally WithAPIToken (per the review PRD's FR-001
// finding) -- nothing requests buzz_auth_tag.
func TestBuildBuzzMonitor_AuthTagSecretIsResolved(t *testing.T) {
	agentSK, agentNsec := genTestKeypair(t)
	ownerSK := nostr.Generate()
	agentPubHex := agentSK.Public().Hex()

	tag, err := buzzinfra.SignAuthTag(ownerSK, agentPubHex, "kind=9")
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}
	// owner_pubkey_hex|conditions|sig_hex, per research.md's OQ-R1
	// resolution and data-dictionary.md.
	secretValue := strings.Join(tag[1:], "|")

	store := &buzzFakeStore{values: map[domain.SecretRef]string{
		{Name: "buzz_private_key", Bot: "buzzbot"}: agentNsec,
		{Name: "buzz_auth_tag", Bot: "buzzbot"}:    secretValue,
	}}
	cfg := config.Config{
		Buzz: config.BuzzConfig{Enabled: true, BotName: "buzzbot", RelayURL: "wss://relay.example.com"},
	}

	mon := buildBuzzMonitor(context.Background(), cfg, store, queue.NewRouter(), "", t.TempDir(), nil, nil, nil)

	if mon == nil {
		t.Fatal("expected a non-nil Monitor for a valid, enabled BuzzConfig with a resolvable auth-tag secret")
	}
	wantRef := domain.SecretRef{Name: "buzz_auth_tag", Bot: "buzzbot"}
	if !containsSecretRef(store.calls, wantRef) {
		t.Errorf("expected buildBuzzMonitor to resolve %+v through the SecretStore, got calls: %+v", wantRef, store.calls)
	}
}

// TestBuildBuzzMonitor_NoAuthTagSecret_StillActivates is the negative
// control at the buildBuzzMonitor level, matching FR-001's own acceptance
// criterion: "A boabot process with no such secret configured behaves
// exactly as today (no tag, no error)." internal/infrastructure/buzz's
// phase_e_wiring_test.go TestE3_NoAuthTagFuncOmitsAuthTag proves the
// RelayClient/AUTH-event half of this mechanism (unmodified by this
// workstream); this test proves the buildBuzzMonitor wiring half does not
// regress activation when no tag is configured -- "log and continue ...
// not fail closed" per FR-001's Green guidance.
func TestBuildBuzzMonitor_NoAuthTagSecret_StillActivates(t *testing.T) {
	_, nsec := genTestKeypair(t)
	store := &buzzFakeStore{values: map[domain.SecretRef]string{
		{Name: "buzz_private_key", Bot: "buzzbot"}: nsec,
	}}
	cfg := config.Config{
		Buzz: config.BuzzConfig{Enabled: true, BotName: "buzzbot", RelayURL: "wss://relay.example.com"},
	}

	mon := buildBuzzMonitor(context.Background(), cfg, store, queue.NewRouter(), "", t.TempDir(), nil, nil, nil)

	if mon == nil {
		t.Fatal("expected a non-nil Monitor when no auth-tag secret is configured")
	}
}

// TestLoadAuthTag_ResolvesAndValidatesWellFormedTag is WS-A2's Green test:
// buzzinfra.LoadAuthTag, given a resolvable, well-formed
// owner_pubkey_hex|conditions|sig_hex secret, returns found=true, a nil
// error, and an AuthTagFunc whose returned tag round-trips exactly through
// SignAuthTag's own output and independently passes
// buzzinfra.ValidateAuthTag -- the same tag TestE3_NIPOAAuthTagIncludedOnAuthEvent
// (internal/infrastructure/buzz/phase_e_wiring_test.go, unmodified by this
// workstream) proves ends up on the signed AUTH event once wired via
// WithAuthTagFunc.
func TestLoadAuthTag_ResolvesAndValidatesWellFormedTag(t *testing.T) {
	agentSK := nostr.Generate()
	ownerSK := nostr.Generate()
	agentPubHex := agentSK.Public().Hex()

	wantTag, err := buzzinfra.SignAuthTag(ownerSK, agentPubHex, "kind=9&created_at<2000000000")
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}
	secretValue := strings.Join(wantTag[1:], "|")

	store := &buzzFakeStore{values: map[domain.SecretRef]string{
		{Name: buzzinfra.AuthTagSecretName, Bot: "buzzbot"}: secretValue,
	}}

	fn, found, err := buzzinfra.LoadAuthTag(context.Background(), store, "buzzbot", agentPubHex)
	if err != nil {
		t.Fatalf("LoadAuthTag: unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true for a resolvable auth-tag secret")
	}

	gotTag, err := fn(context.Background())
	if err != nil {
		t.Fatalf("AuthTagFunc: unexpected error: %v", err)
	}
	if len(gotTag) != 4 {
		t.Fatalf("got %d-element tag, want 4: %v", len(gotTag), gotTag)
	}
	for i, v := range wantTag {
		if gotTag[i] != v {
			t.Errorf("tag[%d] = %q, want %q", i, gotTag[i], v)
		}
	}
	if err := buzzinfra.ValidateAuthTag(gotTag, agentPubHex); err != nil {
		t.Fatalf("ValidateAuthTag on the resolved tag: %v", err)
	}
}

// TestLoadAuthTag_NoSecretConfigured_CleanMiss verifies the "optional
// secret" contract: a SecretStore miss is found=false, err=nil -- not a
// failure -- matching LoadAPIToken's existing behaviour.
func TestLoadAuthTag_NoSecretConfigured_CleanMiss(t *testing.T) {
	store := &buzzFakeStore{values: map[domain.SecretRef]string{}}

	fn, found, err := buzzinfra.LoadAuthTag(context.Background(), store, "buzzbot", nostr.Generate().Public().Hex())
	if err != nil {
		t.Fatalf("expected a clean miss (nil error), got %v", err)
	}
	if found {
		t.Fatal("expected found=false when no auth-tag secret is configured")
	}
	if fn != nil {
		t.Fatal("expected a nil AuthTagFunc on a clean miss")
	}
}

// TestLoadAuthTag_MalformedFieldCount_ReturnsError verifies a secret value
// that does not split into exactly three pipe-delimited fields is a
// genuine error (not a clean miss), and never causes a panic.
func TestLoadAuthTag_MalformedFieldCount_ReturnsError(t *testing.T) {
	cases := []string{
		"only_two|fields",
		"way|too|many|fields|here",
	}
	for _, v := range cases {
		store := &buzzFakeStore{values: map[domain.SecretRef]string{
			{Name: buzzinfra.AuthTagSecretName, Bot: "buzzbot"}: v,
		}}
		_, found, err := buzzinfra.LoadAuthTag(context.Background(), store, "buzzbot", nostr.Generate().Public().Hex())
		if err == nil {
			t.Errorf("value %q: expected an error for a malformed field count", v)
		}
		if found {
			t.Errorf("value %q: expected found=false on error", v)
		}
	}
}

// TestLoadAuthTag_InvalidSignature_ReturnsError verifies a tampered
// signature -- syntactically well-formed (three fields) but failing
// StaticAuthTagFunc's own validation -- is reported as an error, not
// silently accepted.
func TestLoadAuthTag_InvalidSignature_ReturnsError(t *testing.T) {
	agentSK := nostr.Generate()
	ownerSK := nostr.Generate()
	agentPubHex := agentSK.Public().Hex()

	tag, err := buzzinfra.SignAuthTag(ownerSK, agentPubHex, "kind=9")
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}
	// Tamper the signature hex so it no longer verifies.
	tampered := []string{tag[1], tag[2], strings.Repeat("a", len(tag[3]))}
	store := &buzzFakeStore{values: map[domain.SecretRef]string{
		{Name: buzzinfra.AuthTagSecretName, Bot: "buzzbot"}: strings.Join(tampered, "|"),
	}}

	_, found, err := buzzinfra.LoadAuthTag(context.Background(), store, "buzzbot", agentPubHex)
	if err == nil {
		t.Fatal("expected an error for a tampered/non-verifying signature")
	}
	if found {
		t.Fatal("expected found=false on validation failure")
	}
}

// TestLoadAuthTag_EmptyConditionsIsLegal verifies the empty-conditions
// case (ValidateConditions("") is valid per nipoa.go) round-trips through
// the pipe-delimited format without a spurious rejection.
func TestLoadAuthTag_EmptyConditionsIsLegal(t *testing.T) {
	agentSK := nostr.Generate()
	ownerSK := nostr.Generate()
	agentPubHex := agentSK.Public().Hex()

	tag, err := buzzinfra.SignAuthTag(ownerSK, agentPubHex, "")
	if err != nil {
		t.Fatalf("SignAuthTag with empty conditions: %v", err)
	}
	secretValue := strings.Join(tag[1:], "|") // owner||sig
	store := &buzzFakeStore{values: map[domain.SecretRef]string{
		{Name: buzzinfra.AuthTagSecretName, Bot: "buzzbot"}: secretValue,
	}}

	_, found, err := buzzinfra.LoadAuthTag(context.Background(), store, "buzzbot", agentPubHex)
	if err != nil {
		t.Fatalf("LoadAuthTag with empty conditions: unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true for an empty-but-valid conditions field")
	}
}

// TestBuildBuzzMonitor_InvalidAuthTagSecret_LogsAndContinues verifies
// buildBuzzMonitor's log-and-continue (not fail-closed) handling of a
// malformed/invalid auth-tag secret: Buzz still activates, matching
// LoadAPIToken's existing "optional secret" treatment of any resolution
// failure.
func TestBuildBuzzMonitor_InvalidAuthTagSecret_LogsAndContinues(t *testing.T) {
	_, nsec := genTestKeypair(t)
	store := &buzzFakeStore{values: map[domain.SecretRef]string{
		{Name: "buzz_private_key", Bot: "buzzbot"}: nsec,
		{Name: "buzz_auth_tag", Bot: "buzzbot"}:    "not|enough", // wrong field count
	}}
	cfg := config.Config{
		Buzz: config.BuzzConfig{Enabled: true, BotName: "buzzbot", RelayURL: "wss://relay.example.com"},
	}

	mon := buildBuzzMonitor(context.Background(), cfg, store, queue.NewRouter(), "", t.TempDir(), nil, nil, nil)

	if mon == nil {
		t.Fatal("expected a non-nil Monitor even when the configured auth-tag secret is malformed (log-and-continue, not fail-closed)")
	}
}

// TestReadBotDescription_ExtractsWhatIDoParagraph verifies FR-011's
// "description from its AGENTS.md" is read from the "## What I do" section.
func TestReadBotDescription_ExtractsWhatIDoParagraph(t *testing.T) {
	dir := t.TempDir()
	botDir := filepath.Join(dir, "tech-lead")
	if err := os.MkdirAll(botDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "# Tech Lead — AGENTS.md\n\n## What I do\n\nI manage development work end-to-end.\n\n## How to reach me\n\nSend me a message.\n"
	if err := os.WriteFile(filepath.Join(botDir, "AGENTS.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	got := readBotDescription(dir, "tech-lead")
	want := "I manage development work end-to-end."
	if got != want {
		t.Errorf("readBotDescription: got %q, want %q", got, want)
	}
}

// TestReadBotDescription_MissingFileReturnsEmpty verifies a missing or
// unreadable AGENTS.md is never fatal -- just an empty description.
func TestReadBotDescription_MissingFileReturnsEmpty(t *testing.T) {
	if got := readBotDescription(t.TempDir(), "nonexistent-type"); got != "" {
		t.Errorf("readBotDescription: got %q, want empty string", got)
	}
	if got := readBotDescription("", "tech-lead"); got != "" {
		t.Errorf("readBotDescription with empty botsDir: got %q, want empty string", got)
	}
}
