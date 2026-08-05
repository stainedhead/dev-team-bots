package file

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func writeCredsFile(t *testing.T, contents string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod fixture: %v", err)
	}
	return path
}

func TestProvider_Name(t *testing.T) {
	p := New("/nonexistent")
	if got := p.Name(); got != "file" {
		t.Errorf("Name() = %q, want %q", got, "file")
	}
}

func TestProvider_Lookup_Hit_GlobalSecret(t *testing.T) {
	path := writeCredsFile(t, "[default]\nanthropic_api_key = sk-abc123\n", 0o600)

	p := New(path)
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "anthropic_api_key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if got != "sk-abc123" {
		t.Errorf("value = %q, want %q", got, "sk-abc123")
	}
}

// FR-045/B8: per-bot namespacing. A bot-scoped key ("<bot>_<name>") is tried
// when Bot is set; it does not collide with the global key of the same Name.
func TestProvider_Lookup_Hit_BotNamespacedSecret(t *testing.T) {
	path := writeCredsFile(t, "[default]\nbuzzy_buzz_private_key = nsec1bot\nbuzz_private_key = nsec1global\n", 0o600)

	p := New(path)
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "buzz_private_key", Bot: "buzzy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if got != "nsec1bot" {
		t.Errorf("value = %q, want the bot-namespaced value %q, not the global one", got, "nsec1bot")
	}
}

// Consistency with the systemd and keystore providers (neither of which
// falls back): when Bot is set, only the bot-scoped key is consulted. A
// bot-scoped ref MUST NOT silently resolve to a differently-scoped global
// entry — for a secret like a per-bot Buzz nsec, that would mean two
// different bots silently sharing one private key.
func TestProvider_Lookup_BotSet_DoesNotFallBackToGlobal(t *testing.T) {
	path := writeCredsFile(t, "[default]\nbuzz_private_key = nsec1global\n", 0o600)

	p := New(path)
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "buzz_private_key", Bot: "buzzy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected miss (no fallback to the global key), got hit with value %q", got)
	}
	if got != "" {
		t.Errorf("value = %q, want empty", got)
	}
}

func TestProvider_Lookup_Miss_KeyAbsent(t *testing.T) {
	path := writeCredsFile(t, "[default]\nsome_other_key = val\n", 0o600)

	p := New(path)
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "anthropic_api_key"})
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

func TestProvider_Lookup_Miss_FileAbsent(t *testing.T) {
	p := New(filepath.Join(t.TempDir(), "does-not-exist"))
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "anthropic_api_key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected miss when file does not exist, got hit")
	}
	if got != "" {
		t.Errorf("value = %q, want empty", got)
	}
}

// FR-043: world-readable file remains fatal — the provider surfaces the
// existing credentials.Load error, unchanged, rather than downgrading to a
// miss or a warning.
func TestProvider_Lookup_WorldReadable_ReturnsExistingFatalError(t *testing.T) {
	path := writeCredsFile(t, "[default]\nanthropic_api_key = sk-abc123\n", 0o644)

	p := New(path)
	_, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "anthropic_api_key"})
	if ok {
		t.Fatal("expected no hit when the underlying file load fails")
	}
	if err == nil {
		t.Fatal("expected an error for a world-readable credentials file")
	}
	if !strings.Contains(err.Error(), "world-readable") {
		t.Errorf("error = %q, want it to preserve the existing world-readable message", err.Error())
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("error = %q, want the existing remediation hint preserved", err.Error())
	}
}

func TestProvider_Lookup_WorldReadable_ErrorNeverContainsSecretValue(t *testing.T) {
	path := writeCredsFile(t, "[default]\nanthropic_api_key = sk-super-secret-sentinel\n", 0o644)

	p := New(path)
	_, _, err := p.Lookup(context.Background(), domain.SecretRef{Name: "anthropic_api_key"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "sk-super-secret-sentinel") {
		t.Errorf("error leaked secret value: %q", err.Error())
	}
}
