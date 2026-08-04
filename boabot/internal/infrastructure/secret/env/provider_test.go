package env

import (
	"context"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func TestProvider_Name(t *testing.T) {
	p := New()
	if got := p.Name(); got != "env" {
		t.Errorf("Name() = %q, want %q", got, "env")
	}
}

func TestProvider_Lookup_Hit(t *testing.T) {
	t.Setenv("BUZZ_PRIVATE_KEY", "nsec1abc")

	p := New()
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "buzz_private_key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if got != "nsec1abc" {
		t.Errorf("value = %q, want %q", got, "nsec1abc")
	}
}

func TestProvider_Lookup_Miss_Unset(t *testing.T) {
	p := New()
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "totally_unset_var_xyz"})
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

func TestProvider_Lookup_Miss_EmptyValue(t *testing.T) {
	t.Setenv("BUZZ_EMPTY_VAR", "")

	p := New()
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "buzz_empty_var"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected miss for empty env var value, got hit")
	}
	if got != "" {
		t.Errorf("value = %q, want empty", got)
	}
}

// FR-044: env var name is derived from Name only — Bot MUST NOT affect
// resolution. Env vars are inherently process-global.
func TestProvider_Lookup_IgnoresBot(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-shared")

	p := New()
	got1, ok1, _ := p.Lookup(context.Background(), domain.SecretRef{Name: "anthropic_api_key", Bot: "buzzy"})
	got2, ok2, _ := p.Lookup(context.Background(), domain.SecretRef{Name: "anthropic_api_key", Bot: ""})
	got3, ok3, _ := p.Lookup(context.Background(), domain.SecretRef{Name: "anthropic_api_key", Bot: "other-bot"})

	if !ok1 || !ok2 || !ok3 {
		t.Fatal("expected hit regardless of Bot value")
	}
	if got1 != "sk-shared" || got2 != "sk-shared" || got3 != "sk-shared" {
		t.Errorf("values differ by Bot: %q %q %q, want identical", got1, got2, got3)
	}
}

func TestProvider_Lookup_NameIsUppercased(t *testing.T) {
	t.Setenv("MIXED_CASE_KEY", "value1")

	p := New()
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "mixed_case_key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || got != "value1" {
		t.Errorf("got (%q, %v), want (%q, true)", got, ok, "value1")
	}
}
