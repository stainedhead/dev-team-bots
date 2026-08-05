package keystore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// mockInitWithError installs a failing keyring backend for the duration of
// t and restores a clean MockInit() backend afterward. keyring.provider is
// process-global state (set by the zalando/go-keyring package's own
// MockInit/MockInitWithError), so leaving a failing backend installed after
// a test would leak into whatever test runs next in this binary.
func mockInitWithError(t *testing.T, err error) {
	t.Helper()
	keyring.MockInitWithError(err)
	t.Cleanup(keyring.MockInit)
}

func TestProvider_Name(t *testing.T) {
	p := New()
	if got := p.Name(); got != "keystore" {
		t.Errorf("Name() = %q, want %q", got, "keystore")
	}
}

// The library's own supported test seam (keyring.MockInit) swaps the
// package-level backend for an in-memory map — no subprocess, no D-Bus, no
// real OS keystore is ever touched by these tests.
func TestProvider_SetThenLookup_Hit_GlobalSecret(t *testing.T) {
	keyring.MockInit()

	p := New()
	ref := domain.SecretRef{Name: "anthropic_api_key"}
	if err := p.Set(context.Background(), ref, "sk-abc123"); err != nil {
		t.Fatalf("Set: unexpected error: %v", err)
	}

	got, ok, err := p.Lookup(context.Background(), ref)
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if got != "sk-abc123" {
		t.Errorf("value = %q, want %q", got, "sk-abc123")
	}
}

// FR-045/B8: per-bot namespacing. The same logical Name under two different
// Bot values must not collide.
func TestProvider_SetThenLookup_BotNamespacingDoesNotCollide(t *testing.T) {
	keyring.MockInit()

	p := New()
	global := domain.SecretRef{Name: "buzz_private_key"}
	botA := domain.SecretRef{Name: "buzz_private_key", Bot: "buzzy"}
	botB := domain.SecretRef{Name: "buzz_private_key", Bot: "other-bot"}

	if err := p.Set(context.Background(), global, "nsec-global"); err != nil {
		t.Fatalf("Set global: %v", err)
	}
	if err := p.Set(context.Background(), botA, "nsec-a"); err != nil {
		t.Fatalf("Set botA: %v", err)
	}
	if err := p.Set(context.Background(), botB, "nsec-b"); err != nil {
		t.Fatalf("Set botB: %v", err)
	}

	gotGlobal, _, _ := p.Lookup(context.Background(), global)
	gotA, _, _ := p.Lookup(context.Background(), botA)
	gotB, _, _ := p.Lookup(context.Background(), botB)

	if gotGlobal != "nsec-global" || gotA != "nsec-a" || gotB != "nsec-b" {
		t.Errorf("bot namespacing collided: global=%q a=%q b=%q", gotGlobal, gotA, gotB)
	}
}

func TestProvider_Lookup_Miss_NotFound(t *testing.T) {
	keyring.MockInit()

	p := New()
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "never_set"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected miss, got hit")
	}
	if got != "" {
		t.Errorf("value = %q, want empty", got)
	}
}

// A backend error that is not ErrNotFound (simulating, e.g., a D-Bus
// refusal or a locked keychain) must be reported as an error, not swallowed
// into a silent miss, and must never contain the secret value.
func TestProvider_Lookup_BackendError_ReportedNotSwallowed(t *testing.T) {
	backendErr := errors.New("keystore backend unavailable")
	mockInitWithError(t, backendErr)

	p := New()
	_, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "buzz_private_key"})
	if ok {
		t.Fatal("expected no hit on backend error")
	}
	if err == nil {
		t.Fatal("expected a non-nil error")
	}
	if !errors.Is(err, backendErr) {
		t.Errorf("error = %v, want it to wrap %v", err, backendErr)
	}
}

func TestProvider_Lookup_BackendError_NeverContainsSecretValue(t *testing.T) {
	mockInitWithError(t, errors.New("boom"))

	p := New()
	_, _, err := p.Lookup(context.Background(), domain.SecretRef{Name: "buzz_private_key"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "sk-super-secret-sentinel") {
		t.Errorf("error unexpectedly references a secret sentinel: %q", err.Error())
	}
}

func TestProvider_Set_BackendError_NeverContainsSecretValue(t *testing.T) {
	mockInitWithError(t, errors.New("boom"))

	p := New()
	err := p.Set(context.Background(), domain.SecretRef{Name: "buzz_private_key"}, "sk-super-secret-sentinel")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "sk-super-secret-sentinel") {
		t.Errorf("error leaked secret value: %q", err.Error())
	}
}

func TestProvider_Delete_BackendError_ReportedNotSwallowed(t *testing.T) {
	backendErr := errors.New("keystore backend unavailable")
	mockInitWithError(t, backendErr)

	p := New()
	err := p.Delete(context.Background(), domain.SecretRef{Name: "buzz_private_key"})
	if err == nil {
		t.Fatal("expected a non-nil error")
	}
	if !errors.Is(err, backendErr) {
		t.Errorf("error = %v, want it to wrap %v", err, backendErr)
	}
}

func TestProvider_Delete_NotFound_IsNotAnError(t *testing.T) {
	keyring.MockInit()

	p := New()
	if err := p.Delete(context.Background(), domain.SecretRef{Name: "never_set"}); err != nil {
		t.Errorf("Delete of a not-found entry should be a no-op, got: %v", err)
	}
}

func TestProvider_SetThenDelete_ThenLookupMisses(t *testing.T) {
	keyring.MockInit()

	p := New()
	ref := domain.SecretRef{Name: "buzz_private_key", Bot: "buzzy"}
	if err := p.Set(context.Background(), ref, "nsec1abc"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := p.Delete(context.Background(), ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, ok, err := p.Lookup(context.Background(), ref)
	if err != nil {
		t.Fatalf("Lookup after delete: unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected miss after delete")
	}
}

// recordingBackend is a backend fake that records the exact arguments of
// every call, so a test can inspect the constructed call rather than only
// asserting on its outcome.
type recordingBackend struct {
	setCalls []struct{ service, user, password string }
}

func (r *recordingBackend) Get(_, _ string) (string, error) { return "", keyring.ErrNotFound }

func (r *recordingBackend) Set(service, user, password string) error {
	r.setCalls = append(r.setCalls, struct{ service, user, password string }{service, user, password})
	return nil
}

func (r *recordingBackend) Delete(_, _ string) error { return keyring.ErrNotFound }

// FR-052: no provider may pass a secret as a subprocess command-line
// argument. On darwin, zalando/go-keyring's Set constructs the underlying
// `security -i ... add-generic-password -U -s <service> -a <user> -w
// <password>` command and sends it over stdin — -s and -a (service, user)
// are the only pieces that would ever land in a *visible* argv position if
// the library changed its transport; the secret itself must only ever be
// bound to the library's dedicated `password` parameter, never spliced
// into `service` or `user`. This test inspects the actual (service, user,
// password) tuple our provider constructs and hands to the backend,
// proving the value only ever reaches the password slot.
func TestProvider_Set_ConstructsCallWithSecretOnlyInPasswordSlot(t *testing.T) {
	rec := &recordingBackend{}
	p := &Provider{b: rec}

	ref := domain.SecretRef{Name: "buzz_private_key", Bot: "buzzy"}
	secretValue := "nsec1-should-only-be-the-password-arg"

	if err := p.Set(context.Background(), ref, secretValue); err != nil {
		t.Fatalf("Set: unexpected error: %v", err)
	}

	if len(rec.setCalls) != 1 {
		t.Fatalf("Set called the backend %d times, want 1", len(rec.setCalls))
	}
	call := rec.setCalls[0]

	if call.password != secretValue {
		t.Errorf("password slot = %q, want %q", call.password, secretValue)
	}
	if strings.Contains(call.service, secretValue) {
		t.Errorf("secret value leaked into the service argument: %q", call.service)
	}
	if strings.Contains(call.user, secretValue) {
		t.Errorf("secret value leaked into the user argument: %q", call.user)
	}
}

// FR-052 (source-level complement): this package's own code must never
// construct a subprocess itself — it delegates entirely to the library's
// Get/Set/Delete calls, which (per the call-inspection test above and the
// library-source reading recorded in implementation-notes.md) route any
// secret only through their designated password parameter. Guard that fact
// mechanically so a future edit cannot silently reintroduce a direct
// exec.Command call here.
func TestProvider_SourceNeverConstructsASubprocessDirectly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		src := string(data)
		if strings.Contains(src, "os/exec") {
			t.Errorf("%s imports os/exec — this package must delegate subprocess handling to zalando/go-keyring, never build its own argv", name)
		}
		if strings.Contains(src, "exec.Command") {
			t.Errorf("%s calls exec.Command directly — secrets must only ever reach a subprocess via the library's stdin path", name)
		}
	}
}
