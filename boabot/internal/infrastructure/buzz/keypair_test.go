package buzz

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// fakeSecretStore is a minimal domain.SecretStore test double. It does not
// need Store's chain-resolution behaviour (that is Phase B's concern) --
// only a single canned response per call, matching what LoadKeypair
// actually consumes.
type fakeSecretStore struct {
	value string
	err   error

	gotRef domain.SecretRef
}

func (f *fakeSecretStore) Get(_ context.Context, ref domain.SecretRef) (string, error) {
	f.gotRef = ref
	if f.err != nil {
		return "", f.err
	}
	return f.value, nil
}

func TestLoadKeypair_NsecEnvValue(t *testing.T) {
	sk := nostr.Generate()
	nsec := nip19.EncodeNsec(sk)

	store := &fakeSecretStore{value: nsec}
	gotSK, gotPK, err := LoadKeypair(context.Background(), store, "buzzbot")
	if err != nil {
		t.Fatalf("LoadKeypair: %v", err)
	}
	if gotSK != sk {
		t.Errorf("secret key mismatch")
	}
	if gotPK != sk.Public() {
		t.Errorf("pubkey mismatch: got %s want %s", gotPK.Hex(), sk.Public().Hex())
	}
	if store.gotRef.Name != PrivateKeySecretName {
		t.Errorf("ref.Name = %q, want %q", store.gotRef.Name, PrivateKeySecretName)
	}
	if store.gotRef.Bot != "buzzbot" {
		t.Errorf("ref.Bot = %q, want %q", store.gotRef.Bot, "buzzbot")
	}
}

func TestLoadKeypair_HexValue(t *testing.T) {
	sk := nostr.Generate()
	store := &fakeSecretStore{value: sk.Hex()}

	gotSK, gotPK, err := LoadKeypair(context.Background(), store, "buzzbot")
	if err != nil {
		t.Fatalf("LoadKeypair: %v", err)
	}
	if gotSK != sk || gotPK != sk.Public() {
		t.Errorf("key mismatch")
	}
}

func TestLoadKeypair_WhitespacePadded(t *testing.T) {
	sk := nostr.Generate()
	store := &fakeSecretStore{value: "  " + sk.Hex() + "\n"}

	gotSK, _, err := LoadKeypair(context.Background(), store, "buzzbot")
	if err != nil {
		t.Fatalf("LoadKeypair: %v", err)
	}
	if gotSK != sk {
		t.Errorf("key mismatch after whitespace trim")
	}
}

func TestLoadKeypair_StoreError_FailsClosed(t *testing.T) {
	sentinel := errors.New("no provider resolved reference")
	store := &fakeSecretStore{err: sentinel}

	_, _, err := LoadKeypair(context.Background(), store, "buzzbot")
	if err == nil {
		t.Fatal("expected error when the store cannot resolve the secret")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel error, got %v", err)
	}
}

func TestLoadKeypair_MalformedValue_FailsClosed(t *testing.T) {
	cases := map[string]string{
		"empty string":        "",
		"garbage":             "not-a-key-at-all",
		"too-long hex":        strings.Repeat("ab", 40), // 80 hex chars, over the 64-char limit
		"invalid nsec bech32": "nsec1notvalidbech32",
		"npub instead of nsec": func() string {
			sk := nostr.Generate()
			return nip19.EncodeNpub(sk.Public())
		}(),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			store := &fakeSecretStore{value: raw}
			_, _, err := LoadKeypair(context.Background(), store, "buzzbot")
			if err == nil {
				t.Fatalf("expected error for malformed value %q", raw)
			}
			// The raw (malformed) secret value must never appear in the
			// returned error text.
			if raw != "" && strings.Contains(err.Error(), raw) {
				t.Errorf("error text leaks the raw secret value: %v", err)
			}
		})
	}
}

func TestLoadKeypair_ZeroKey_RejectedAsMalformed(t *testing.T) {
	var zero nostr.SecretKey
	store := &fakeSecretStore{value: zero.Hex()}
	_, _, err := LoadKeypair(context.Background(), store, "buzzbot")
	if err == nil {
		t.Fatal("expected the all-zero secret key to be rejected")
	}
}

// TestLoadKeypair_NeverLogsSecret is the D4-specific, stronger version of
// Phase B's provider-level no-value-logging test: it exercises LoadKeypair
// through a captured slog buffer with a sentinel nsec value and asserts
// neither the bech32-encoded secret NOR its decoded hex form ever appears
// in any log line, on both the success and failure paths.
func TestLoadKeypair_NeverLogsSecret(t *testing.T) {
	sk := nostr.Generate()
	nsec := nip19.EncodeNsec(sk)
	hexKey := sk.Hex()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	// Success path: a well-formed nsec resolves cleanly.
	store := &fakeSecretStore{value: nsec}
	if _, _, err := LoadKeypair(context.Background(), store, "buzzbot"); err != nil {
		t.Fatalf("LoadKeypair: %v", err)
	}
	// LoadKeypair itself takes no logger (it is a pure function returning
	// an error for the caller to log) -- but a realistic caller logs the
	// error/outcome using the resolved ref and outcome only, never the
	// secret. Simulate that caller-side logging here to prove the values
	// this package hands back are safe to log.
	logger.Info("buzz keypair loaded", "bot", "buzzbot", "pubkey", sk.Public().Hex())

	// Failure path: a malformed value must not leak either, including in
	// the returned error's text, which a realistic caller logs directly.
	badStore := &fakeSecretStore{value: nsec + "-corrupted"}
	_, _, err := LoadKeypair(context.Background(), badStore, "buzzbot")
	if err == nil {
		t.Fatal("expected corrupted nsec to fail")
	}
	logger.Error("buzz keypair load failed", "bot", "buzzbot", "err", err)

	out := buf.String()
	if strings.Contains(out, nsec) {
		t.Errorf("captured log output contains the bech32 nsec secret")
	}
	if strings.Contains(out, hexKey) {
		t.Errorf("captured log output contains the hex-encoded secret")
	}
}
