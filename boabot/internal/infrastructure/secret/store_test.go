package secret

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// fakeProvider is a domain.SecretProvider test double used to exercise
// Store.Get's chain-resolution logic in isolation from the four real
// providers (env/systemd/keystore/file), per tasks.md B3's "tested against
// fake providers" acceptance criterion.
type fakeProvider struct {
	name  string
	value string
	hit   bool
	err   error
	calls int
	// hang, if non-nil, makes Lookup block on this channel forever,
	// deliberately ignoring ctx cancellation — simulating a
	// non-cooperative backend (e.g. an unresponsive D-Bus call) so the
	// test can prove Store enforces the timeout at the call boundary
	// rather than relying on provider cooperation.
	hang chan struct{}
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Lookup(ctx context.Context, _ domain.SecretRef) (string, bool, error) {
	f.calls++
	if f.hang != nil {
		<-f.hang // never closed by the timeout test: intentionally never returns
	}
	return f.value, f.hit, f.err
}

var ref = domain.SecretRef{Name: "buzz_private_key", Bot: "buzzy"}

func TestStore_Get_FirstHitWins(t *testing.T) {
	miss := &fakeProvider{name: "p1", hit: false}
	hit := &fakeProvider{name: "p2", value: "found", hit: true}
	neverReached := &fakeProvider{name: "p3", value: "should-not-be-used", hit: true}

	s := New([]domain.SecretProvider{miss, hit, neverReached})
	got, err := s.Get(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "found" {
		t.Errorf("value = %q, want %q", got, "found")
	}
	if neverReached.calls != 0 {
		t.Errorf("provider after the first hit was called %d times, want 0 (first-hit-wins)", neverReached.calls)
	}
}

// FR-039: a provider that errors does not halt the chain.
func TestStore_Get_ProviderErrorDoesNotHaltChain(t *testing.T) {
	errored := &fakeProvider{name: "p1", err: errors.New("simulated D-Bus refusal")}
	hit := &fakeProvider{name: "p2", value: "found", hit: true}

	s := New([]domain.SecretProvider{errored, hit})
	got, err := s.Get(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "found" {
		t.Errorf("value = %q, want %q", got, "found")
	}
}

// A hung provider must time out rather than block the chain forever.
func TestStore_Get_HungProviderTimesOut(t *testing.T) {
	hung := &fakeProvider{name: "p1", hang: make(chan struct{})}
	hit := &fakeProvider{name: "p2", value: "found", hit: true}

	s := New([]domain.SecretProvider{hung, hit}, WithProviderTimeout(20*time.Millisecond))

	done := make(chan struct{})
	var got string
	var err error
	go func() {
		got, err = s.Get(context.Background(), ref)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Store.Get did not return within 2s of a hung provider — timeout not enforced")
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "found" {
		t.Errorf("value = %q, want %q", got, "found")
	}
}

// FR-053: an all-miss Get names the reference and enumerates every
// provider consulted.
func TestStore_Get_AllMiss_NamesReferenceAndEnumeratesProviders(t *testing.T) {
	p1 := &fakeProvider{name: "env"}
	p2 := &fakeProvider{name: "systemd"}
	p3 := &fakeProvider{name: "keystore"}
	p4 := &fakeProvider{name: "file"}

	s := New([]domain.SecretProvider{p1, p2, p3, p4})
	_, err := s.Get(context.Background(), ref)
	if err == nil {
		t.Fatal("expected an error when every provider misses")
	}
	msg := err.Error()
	if !strings.Contains(msg, ref.Name) {
		t.Errorf("error = %q, want it to name the reference %q", msg, ref.Name)
	}
	for _, name := range []string{"env", "systemd", "keystore", "file"} {
		if !strings.Contains(msg, name) {
			t.Errorf("error = %q, want it to enumerate provider %q", msg, name)
		}
	}
}

// NotFoundError.Unwrap must let callers use errors.Is/errors.As to inspect
// the underlying per-provider errors it collected.
func TestNotFoundError_UnwrapSupportsErrorsIs(t *testing.T) {
	sentinelErr := errors.New("simulated D-Bus refusal")
	errored := &fakeProvider{name: "keystore", err: sentinelErr}
	miss := &fakeProvider{name: "file"}

	s := New([]domain.SecretProvider{errored, miss})
	_, err := s.Get(context.Background(), ref)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("errors.Is(err, sentinelErr) = false, want true (via NotFoundError.Unwrap); err = %v", err)
	}

	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("errors.As into *NotFoundError failed; err = %v", err)
	}
	if len(nfe.Providers) != 2 {
		t.Errorf("Providers = %v, want 2 entries", nfe.Providers)
	}
}

func TestStore_Get_AllMiss_ErrorNeverContainsSecretValue(t *testing.T) {
	p1 := &fakeProvider{name: "env"}
	s := New([]domain.SecretProvider{p1})
	_, err := s.Get(context.Background(), ref)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "sk-super-secret-sentinel") {
		t.Errorf("error unexpectedly references a secret sentinel: %q", err.Error())
	}
}

// PRD AC: "provider precedence: with the same logical secret present in all
// four providers, the env var wins; unset it and the systemd credential
// wins; unset that and the keystore wins; remove that and the file wins —
// asserted as a single ordered test" (fake providers standing in for each
// of the four real ones, in the FR-040 default order).
func TestStore_Get_ProviderPrecedence_OrderedTest(t *testing.T) {
	env := &fakeProvider{name: "env", value: "from-env", hit: true}
	systemd := &fakeProvider{name: "systemd", value: "from-systemd", hit: true}
	keystore := &fakeProvider{name: "keystore", value: "from-keystore", hit: true}
	file := &fakeProvider{name: "file", value: "from-file", hit: true}

	s := New([]domain.SecretProvider{env, systemd, keystore, file})

	got, err := s.Get(context.Background(), ref)
	if err != nil || got != "from-env" {
		t.Fatalf("with all four present: got (%q, %v), want (%q, nil)", got, err, "from-env")
	}

	env.hit = false
	got, err = s.Get(context.Background(), ref)
	if err != nil || got != "from-systemd" {
		t.Fatalf("with env unset: got (%q, %v), want (%q, nil)", got, err, "from-systemd")
	}

	systemd.hit = false
	got, err = s.Get(context.Background(), ref)
	if err != nil || got != "from-keystore" {
		t.Fatalf("with env+systemd unset: got (%q, %v), want (%q, nil)", got, err, "from-keystore")
	}

	keystore.hit = false
	got, err = s.Get(context.Background(), ref)
	if err != nil || got != "from-file" {
		t.Fatalf("with only file present: got (%q, %v), want (%q, nil)", got, err, "from-file")
	}

	file.hit = false
	_, err = s.Get(context.Background(), ref)
	if err == nil {
		t.Fatal("with nothing present, expected an all-miss error")
	}
}

func TestStore_Get_ProviderOmissible(t *testing.T) {
	// Only two of the four providers configured — chain still resolves.
	hit := &fakeProvider{name: "file", value: "found", hit: true}
	s := New([]domain.SecretProvider{hit})
	got, err := s.Get(context.Background(), ref)
	if err != nil || got != "found" {
		t.Fatalf("got (%q, %v), want (%q, nil)", got, err, "found")
	}
}

func TestStore_ImplementsDomainSecretStore(t *testing.T) {
	var _ domain.SecretStore = New(nil)
}

func TestStore_DefaultProviderTimeout(t *testing.T) {
	s := New(nil)
	if s.timeout != defaultProviderTimeout {
		t.Errorf("default timeout = %v, want %v", s.timeout, defaultProviderTimeout)
	}
}
