package systemd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func TestProvider_Name(t *testing.T) {
	p := New()
	if got := p.Name(); got != "systemd" {
		t.Errorf("Name() = %q, want %q", got, "systemd")
	}
}

// FR-042: inert (miss, no error) when $CREDENTIALS_DIRECTORY is unset — the
// provider must cost nothing on non-Linux platforms and on Linux outside
// systemd.
func TestProvider_Lookup_Inert_WhenEnvVarUnset(t *testing.T) {
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	os.Unsetenv("CREDENTIALS_DIRECTORY") //nolint:errcheck

	p := New()
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "buzz_private_key"})
	if err != nil {
		t.Fatalf("unexpected error when unset (must be inert): %v", err)
	}
	if ok {
		t.Fatal("expected miss when CREDENTIALS_DIRECTORY is unset")
	}
	if got != "" {
		t.Errorf("value = %q, want empty", got)
	}
}

func TestProvider_Lookup_Hit_GlobalSecret(t *testing.T) {
	dir := t.TempDir()
	writeCred(t, dir, "buzz_private_key", "nsec1abc\n")
	t.Setenv("CREDENTIALS_DIRECTORY", dir)

	p := New()
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "buzz_private_key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if got != "nsec1abc" {
		t.Errorf("value = %q, want trailing newline stripped %q", got, "nsec1abc")
	}
}

// FR-045/B8: per-bot namespacing via filename convention "<bot>_<name>".
func TestProvider_Lookup_Hit_BotNamespacedSecret(t *testing.T) {
	dir := t.TempDir()
	writeCred(t, dir, "buzzy_buzz_private_key", "nsec1bot")
	writeCred(t, dir, "buzz_private_key", "nsec1global")
	t.Setenv("CREDENTIALS_DIRECTORY", dir)

	p := New()
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "buzz_private_key", Bot: "buzzy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if got != "nsec1bot" {
		t.Errorf("value = %q, want the bot-namespaced value %q", got, "nsec1bot")
	}
}

func TestProvider_Lookup_Miss_FileAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CREDENTIALS_DIRECTORY", dir)

	p := New()
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "not_present"})
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

// A read error that is not "file does not exist" (e.g. the credential name
// unexpectedly refers to a directory) must surface as an error, not be
// silently swallowed into a miss.
func TestProvider_Lookup_ReadError_IsNotSwallowed(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "buzz_private_key"), 0o700); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", dir)

	p := New()
	_, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "buzz_private_key"})
	if ok {
		t.Fatal("expected no hit on read error")
	}
	if err == nil {
		t.Fatal("expected a non-nil error for an unreadable credential path")
	}
}

func TestProvider_Lookup_Miss_EmptyFileContent(t *testing.T) {
	dir := t.TempDir()
	writeCred(t, dir, "buzz_private_key", "")
	t.Setenv("CREDENTIALS_DIRECTORY", dir)

	p := New()
	got, ok, err := p.Lookup(context.Background(), domain.SecretRef{Name: "buzz_private_key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected miss for empty credential file content, got hit")
	}
	if got != "" {
		t.Errorf("value = %q, want empty", got)
	}
}

func writeCred(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o400); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
