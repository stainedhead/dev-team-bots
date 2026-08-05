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

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
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

	mon := buildBuzzMonitor(context.Background(), cfg, store, queue.NewRouter(), false, "", t.TempDir(), nil)

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

		mon := buildBuzzMonitor(context.Background(), cfg, store, queue.NewRouter(), false, "", t.TempDir(), nil)

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

	mon := buildBuzzMonitor(context.Background(), cfg, store, queue.NewRouter(), false, "", t.TempDir(), nil)

	if mon != nil {
		t.Fatal("expected nil Monitor when the private key fails to resolve")
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

	mon := buildBuzzMonitor(context.Background(), cfg, store, router, false, t.TempDir(), t.TempDir(), nil)

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

	mon := buildBuzzMonitor(context.Background(), cfg, store, router, true, "", t.TempDir(), nil)
	if mon == nil {
		t.Fatal("expected a non-nil Monitor")
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
