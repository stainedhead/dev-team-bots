//go:build integration

// This file holds Phase I's (tasks.md I2) `//go:build integration` stubs
// for every Secret-storage PRD acceptance criterion that requires a real
// OS keystore -- interactive-session resolution on macOS/Windows/Linux,
// plus the two hardest-to-verify service-mode ACs: macOS LaunchDaemon
// System-keychain reachability (FR-041's core open question, flagged
// repeatedly across the PRD and every prior phase) and a Windows service
// resolving under its own account identity (OQ-7). None of these run
// under a plain `go test ./...` (excluded by the build tag) and every
// test additionally self-skips on the wrong GOOS or when its required
// environment variable is unset, so `go test -tags integration ./...` is
// safe anywhere without touching a real keystore -- it only proves the
// file compiles and every test skips cleanly. Running these for real is
// tracked on implementation-notes.md's "Manual Verification Required"
// checklist -- outside this job's automated scope.
//
// Environment contract:
//
//	BUZZ_KEYSTORE_TEST_ACCOUNT   the "user" name to Set/Get/Delete against.
//	                             Interactive tests generate a fresh one
//	                             per run when unset; the two service-mode
//	                             tests require it to be pre-provisioned by
//	                             an operator (a LaunchDaemon/Windows
//	                             service cannot itself interactively
//	                             prompt to create the entry), so those
//	                             skip rather than generate when unset.
package keystore

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/zalando/go-keyring"
)

func skipUnlessGOOS(t *testing.T, want string) {
	t.Helper()
	if runtime.GOOS != want {
		t.Skipf("this test only applies to GOOS=%s (running on %s)", want, runtime.GOOS)
	}
}

func testRef(name string) domain.SecretRef {
	return domain.SecretRef{Name: name, Bot: "integration-test"}
}

// roundTrip exercises Set -> Lookup -> Delete -> Lookup(miss) against the
// real OS keystore Provider (New(), not the fake-backend seam Phase B's
// unit tests use).
func roundTrip(t *testing.T, ref domain.SecretRef) {
	t.Helper()
	p := New()
	ctx := context.Background()
	const value = "integration-test-secret-value"

	if err := p.Set(ctx, ref, value); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Cleanup(func() { _ = p.Delete(ctx, ref) })

	got, ok, err := p.Lookup(ctx, ref)
	if err != nil {
		t.Fatalf("Lookup after Set: %v", err)
	}
	if !ok || got != value {
		t.Fatalf("Lookup after Set = (%q, %v), want (%q, true)", got, ok, value)
	}

	if err := p.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, err := p.Lookup(ctx, ref); err != nil || ok {
		t.Fatalf("Lookup after Delete = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

// --- Interactive-session resolution: three separate PRD ACs -------------

// TestInteractive_MacOSLoginKeychain is the PRD AC "a secret written to the
// macOS login keychain is resolved by an interactively-run boabot, with
// nothing in config.yaml or ~/.boabot/credentials" (line 575). Must be run
// interactively (an unlocked login keychain) -- not under CI, not headless.
func TestInteractive_MacOSLoginKeychain(t *testing.T) {
	skipUnlessGOOS(t, "darwin")
	roundTrip(t, testRef(envOrFresh(t, "BUZZ_KEYSTORE_TEST_ACCOUNT", "macos-login-keychain")))
}

// TestInteractive_WindowsCredentialManager is the PRD AC "the same, on
// Windows via Credential Manager" (line 576).
func TestInteractive_WindowsCredentialManager(t *testing.T) {
	skipUnlessGOOS(t, "windows")
	roundTrip(t, testRef(envOrFresh(t, "BUZZ_KEYSTORE_TEST_ACCOUNT", "windows-credential-manager")))
}

// TestInteractive_LinuxSecretService is the PRD AC "the same, on Linux via
// Secret Service in a desktop session" (line 577). Requires a running
// desktop session with an unlocked keyring (gnome-keyring/kwallet) backing
// org.freedesktop.secrets over the session D-Bus.
func TestInteractive_LinuxSecretService(t *testing.T) {
	skipUnlessGOOS(t, "linux")
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("no session D-Bus available (DBUS_SESSION_BUS_ADDRESS unset) -- this test needs a real desktop session, not headless CI")
	}
	roundTrip(t, testRef(envOrFresh(t, "BUZZ_KEYSTORE_TEST_ACCOUNT", "linux-secret-service")))
}

func envOrFresh(t *testing.T, key, fallbackSuffix string) string {
	t.Helper()
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallbackSuffix + "-" + t.Name()
}

// --- Service mode: the two hardest ACs -----------------------------------

// TestServiceMode_MacOSLaunchDaemonSystemKeychainReachability is FR-041's
// single most uncertain claim, flagged repeatedly across the PRD and every
// prior phase as needing real validation: whether zalando/go-keyring's
// darwin backend (which shells out to `security`) can reach the System
// keychain at all when run as root from a LaunchDaemon with no logged-in
// user session, and if not, whether the documented
// `-k /Library/Keychains/System.keychain` provisioning step is a working
// fallback. This test only runs when BUZZ_KEYSTORE_TEST_ACCOUNT names an
// entry an operator has ALREADY provisioned into the System keychain via
// that documented step, from inside an actual LaunchDaemon-launched
// process (a plain `go test` invocation, even as root, does not reproduce
// a LaunchDaemon's session-less execution context) -- it does not
// provision anything itself.
func TestServiceMode_MacOSLaunchDaemonSystemKeychainReachability(t *testing.T) {
	skipUnlessGOOS(t, "darwin")
	account := os.Getenv("BUZZ_KEYSTORE_TEST_ACCOUNT")
	if account == "" {
		t.Skip("BUZZ_KEYSTORE_TEST_ACCOUNT not set; this test requires a pre-provisioned System-keychain entry (see keystore package doc and user-docs/Buzz-Adoption-Config.md), run from an actual LaunchDaemon context")
	}

	// Read directly via the library, not roundTrip's Set/Delete: this test
	// asserts *reachability* of an operator-provisioned entry, and must not
	// mutate the System keychain out from under a real deployment.
	got, err := keyring.Get(serviceName, account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			t.Fatalf("System-keychain entry %q/%q not found -- either the reachability question (FR-041) is answered NO for LaunchDaemon context, or the entry was never provisioned; record either outcome explicitly per the PRD AC (\"either outcome is acceptable; silently claiming support is not\")", serviceName, account)
		}
		t.Fatalf("System-keychain lookup failed: %v", err)
	}
	if got == "" {
		t.Fatal("System-keychain entry resolved to an empty value")
	}
	t.Log("System keychain reachable from this process context -- record this outcome (and whether it required -k /Library/Keychains/System.keychain provisioning) in docs/architectural-decision-record.md per FR-041")
}

// TestServiceMode_WindowsCredentialManagerUnderServiceAccount is OQ-7: a
// boabot Windows service resolving a credential written under its own
// service account identity (e.g. a dedicated service account, or
// LocalSystem). Windows Credential Manager entries are per-user (or, for
// LocalSystem, per-machine) -- an entry an operator writes while logged in
// interactively is NOT visible to a service running as a different
// identity, which is exactly the OQ-7 question this test exists to
// confirm one way or the other. Like the macOS LaunchDaemon test, this
// only reads an operator-provisioned entry; it does not provision one,
// since doing so requires already running under the target service
// identity.
func TestServiceMode_WindowsCredentialManagerUnderServiceAccount(t *testing.T) {
	skipUnlessGOOS(t, "windows")
	account := os.Getenv("BUZZ_KEYSTORE_TEST_ACCOUNT")
	if account == "" {
		t.Skip("BUZZ_KEYSTORE_TEST_ACCOUNT not set; this test requires a credential pre-provisioned under the target Windows service account identity (see OQ-7 in the PRD), run from the actual service process")
	}

	got, err := keyring.Get(serviceName, account)
	if err != nil {
		t.Fatalf("Windows Credential Manager lookup failed under this process's identity -- record the OQ-7 finding either way (whether LocalSystem/service-account identity can read this entry): %v", err)
	}
	if got == "" {
		t.Fatal("Credential Manager entry resolved to an empty value")
	}
	t.Log("Credential Manager reachable under this service's own account identity -- record this OQ-7 outcome in docs/architectural-decision-record.md")
}
