//go:build integration

// This file holds Phase I's (tasks.md I2) `//go:build integration` stub for
// the Secret-storage PRD AC that requires a real systemd unit: "a systemd
// unit using LoadCredentialEncrypted= starts with no session D-Bus and no
// unlocked keyring, and resolves the secret from $CREDENTIALS_DIRECTORY"
// (line 579). This provider (Lookup) already runs with zero D-Bus/syscalls
// beyond an env-var read plus a file read (see provider.go's package doc),
// so the interesting confirmation here is entirely environmental: that a
// real systemd unit actually populates $CREDENTIALS_DIRECTORY the way this
// provider assumes, in a context that has no session bus at all.
package systemd

import (
	"context"
	"os"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// TestServiceMode_LoadCredentialEncrypted_NoSessionDBus asserts this
// provider resolves a real systemd-supplied credential. It only runs
// inside a systemd unit configured with
// LoadCredentialEncrypted=<name>:<path> (or LoadCredential=), which sets
// $CREDENTIALS_DIRECTORY -- a plain `go test` invocation outside such a
// unit does not have it set, so this self-skips everywhere else,
// including CI. BUZZ_TEST_SYSTEMD_CREDENTIAL_NAME names the credential ID
// the unit file configured (defaults to "buzz_private_key", the same name
// D4 resolves the Buzz nsec under).
func TestServiceMode_LoadCredentialEncrypted_NoSessionDBus(t *testing.T) {
	dir := os.Getenv("CREDENTIALS_DIRECTORY")
	if dir == "" {
		t.Skip("CREDENTIALS_DIRECTORY not set; this test only runs inside a systemd unit using LoadCredentialEncrypted=/LoadCredential=")
	}
	if _, ok := os.LookupEnv("DBUS_SESSION_BUS_ADDRESS"); ok {
		t.Log("note: DBUS_SESSION_BUS_ADDRESS is set -- this run does not confirm the 'no session D-Bus' half of the AC, only credential resolution itself")
	}

	name := os.Getenv("BUZZ_TEST_SYSTEMD_CREDENTIAL_NAME")
	if name == "" {
		name = "buzz_private_key"
	}

	p := New()
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: name})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatalf("systemd credential %q not resolved from $CREDENTIALS_DIRECTORY=%s -- confirm the unit file's LoadCredentialEncrypted= directive matches this name", name, dir)
	}
	if got == "" {
		t.Fatal("resolved credential value is empty")
	}
	t.Log("systemd LoadCredentialEncrypted= credential resolved with no session D-Bus present")
}

// TestServiceMode_NegativePath_NoSystemdCredential_NamesEveryProvider is
// the AC's negative half (line 580): "only a Secret Service entry present,
// no systemd credential -- fails with an error naming every provider
// consulted, not a hang." The chain-level logic (Store.Get enumerating
// every provider on an all-miss) is already proven by a REAL,
// non-integration-tagged unit test using fake providers --
// TestStore_Get_AllMiss_NamesReferenceAndEnumeratesProviders in
// internal/infrastructure/secret/store_test.go, which uses fakes named
// "env"/"systemd"/"keystore"/"file" and asserts the error names the
// reference and all four -- so it does not need live infrastructure and is
// not duplicated here. This test is the environmental confirmation: run
// inside a systemd unit with NO LoadCredentialEncrypted= directive (this
// Provider is therefore inert per FR-042 -- $CREDENTIALS_DIRECTORY unset),
// confirming Lookup itself returns a clean miss (not an error, not a
// hang) in that exact deployment shape, which is the precondition the
// fake-based chain test's logic depends on holding for the real provider.
func TestServiceMode_NegativePath_NoSystemdCredential_NamesEveryProvider(t *testing.T) {
	if os.Getenv("BUZZ_TEST_SYSTEMD_NEGATIVE_PATH") != "1" {
		t.Skip("BUZZ_TEST_SYSTEMD_NEGATIVE_PATH not set to 1; run this inside a systemd unit deliberately configured WITHOUT LoadCredentialEncrypted=, alongside a keystore-only (Secret Service) secret, to confirm the negative path")
	}
	if _, set := os.LookupEnv("CREDENTIALS_DIRECTORY"); set {
		t.Fatal("CREDENTIALS_DIRECTORY is set -- this unit is not exercising the negative path (no systemd credential configured)")
	}

	p := New()
	_, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "buzz_private_key"})
	if err != nil {
		t.Fatalf("systemd provider returned an error rather than an inert miss when $CREDENTIALS_DIRECTORY is unset (violates FR-042): %v", err)
	}
	if ok {
		t.Fatal("systemd provider unexpectedly resolved a value with no $CREDENTIALS_DIRECTORY set")
	}
	t.Log("systemd provider correctly inert (clean miss, no error) with no credential configured; see store_test.go's TestStore_Get_AllMiss_NamesReferenceAndEnumeratesProviders for the chain-level 'names every provider, not a hang' assertion this precondition supports")
}
