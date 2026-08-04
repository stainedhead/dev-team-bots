package domain

import (
	"context"
	"testing"
)

// fakeProvider is a minimal SecretProvider used only to prove the interface
// shape is usable from outside the package and to exercise SecretRef.
type fakeProvider struct {
	name  string
	value string
	hit   bool
	err   error
}

func (f fakeProvider) Name() string { return f.name }

func (f fakeProvider) Lookup(_ context.Context, _ SecretRef) (string, bool, error) {
	return f.value, f.hit, f.err
}

var _ SecretProvider = fakeProvider{}

// fakeStore is a minimal SecretStore used only to prove the interface shape
// compiles and is usable from outside the package.
type fakeStore struct {
	value string
	err   error
}

func (f fakeStore) Get(_ context.Context, _ SecretRef) (string, error) {
	return f.value, f.err
}

var _ SecretStore = fakeStore{}

func TestSecretRef_Fields(t *testing.T) {
	ref := SecretRef{Name: "buzz_private_key", Bot: "buzzy"}

	if ref.Name != "buzz_private_key" {
		t.Errorf("Name = %q, want %q", ref.Name, "buzz_private_key")
	}
	if ref.Bot != "buzzy" {
		t.Errorf("Bot = %q, want %q", ref.Bot, "buzzy")
	}
}

func TestSecretRef_ZeroValue_BotIsGlobal(t *testing.T) {
	ref := SecretRef{Name: "anthropic_api_key"}

	if ref.Bot != "" {
		t.Errorf("zero-value Bot = %q, want empty string (global/shared secret)", ref.Bot)
	}
}

func TestSecretProvider_Lookup_Hit(t *testing.T) {
	p := fakeProvider{name: "fake", value: "s3cr3t", hit: true}

	got, ok, err := p.Lookup(context.Background(), SecretRef{Name: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if got != "s3cr3t" {
		t.Errorf("value = %q, want %q", got, "s3cr3t")
	}
	if p.Name() != "fake" {
		t.Errorf("Name() = %q, want %q", p.Name(), "fake")
	}
}

func TestSecretProvider_Lookup_Miss(t *testing.T) {
	p := fakeProvider{name: "fake", hit: false}

	got, ok, err := p.Lookup(context.Background(), SecretRef{Name: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected miss, got hit")
	}
	if got != "" {
		t.Errorf("value = %q, want empty string on miss", got)
	}
}

func TestSecretStore_Get(t *testing.T) {
	s := fakeStore{value: "resolved"}

	got, err := s.Get(context.Background(), SecretRef{Name: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "resolved" {
		t.Errorf("value = %q, want %q", got, "resolved")
	}
}
