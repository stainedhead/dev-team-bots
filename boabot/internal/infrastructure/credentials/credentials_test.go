package credentials_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/credentials"
)

const sampleINI = `# BaoBot credentials
[default]
anthropic_api_key = sk-ant-default
openai_api_key    = sk-default
boabot_backup_token = ghp_default

[work]
anthropic_api_key = sk-ant-work
openai_api_key    = sk-work

; another comment
[staging]
anthropic_api_key = sk-ant-staging
`

func writeCreds(t *testing.T, dir, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, "credentials")
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	return p
}

// TestLoad_DefaultProfile verifies that the [default] section is returned when
// BOABOT_PROFILE is not set.
func TestLoad_DefaultProfile(t *testing.T) {
	t.Setenv("BOABOT_PROFILE", "")
	dir := t.TempDir()
	p := writeCreds(t, dir, sampleINI, 0600)

	creds, err := credentials.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds["anthropic_api_key"] != "sk-ant-default" {
		t.Errorf("anthropic_api_key: got %q, want sk-ant-default", creds["anthropic_api_key"])
	}
	if creds["openai_api_key"] != "sk-default" {
		t.Errorf("openai_api_key: got %q, want sk-default", creds["openai_api_key"])
	}
	if creds["boabot_backup_token"] != "ghp_default" {
		t.Errorf("boabot_backup_token: got %q, want ghp_default", creds["boabot_backup_token"])
	}
}

// TestLoad_WorkProfile verifies that the [work] section is returned when
// BOABOT_PROFILE=work.
func TestLoad_WorkProfile(t *testing.T) {
	t.Setenv("BOABOT_PROFILE", "work")
	dir := t.TempDir()
	p := writeCreds(t, dir, sampleINI, 0600)

	creds, err := credentials.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds["anthropic_api_key"] != "sk-ant-work" {
		t.Errorf("anthropic_api_key: got %q, want sk-ant-work", creds["anthropic_api_key"])
	}
	if creds["openai_api_key"] != "sk-work" {
		t.Errorf("openai_api_key: got %q, want sk-work", creds["openai_api_key"])
	}
	// boabot_backup_token is only in [default]
	if _, ok := creds["boabot_backup_token"]; ok {
		t.Error("boabot_backup_token should not be present in [work] profile")
	}
}

// TestLoad_UnknownProfile verifies that an unknown profile returns an empty
// map with nil error.
func TestLoad_UnknownProfile(t *testing.T) {
	t.Setenv("BOABOT_PROFILE", "doesnotexist")
	dir := t.TempDir()
	p := writeCreds(t, dir, sampleINI, 0600)

	creds, err := credentials.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(creds) != 0 {
		t.Errorf("expected empty map for unknown profile, got %v", creds)
	}
}

// TestLoad_WorldReadable verifies that a world-readable file returns an error.
func TestLoad_WorldReadable(t *testing.T) {
	dir := t.TempDir()
	p := writeCreds(t, dir, sampleINI, 0644) // world-readable

	_, err := credentials.Load(p)
	if err == nil {
		t.Fatal("expected error for world-readable credentials file, got nil")
	}
}

// TestLoad_NonExistentFile verifies that a missing file returns an empty map
// and nil error.
func TestLoad_NonExistentFile(t *testing.T) {
	creds, err := credentials.Load("/nonexistent/path/.boabot/credentials")
	if err != nil {
		t.Fatalf("expected nil error for non-existent file, got %v", err)
	}
	if len(creds) != 0 {
		t.Errorf("expected empty map for non-existent file, got %v", creds)
	}
}

// TestLoad_KeyEqualsWithoutSpaces verifies that key=value (no spaces) parses correctly.
func TestLoad_KeyEqualsWithoutSpaces(t *testing.T) {
	t.Setenv("BOABOT_PROFILE", "default")
	dir := t.TempDir()
	ini := "[default]\nmy_key=my_value\n"
	p := writeCreds(t, dir, ini, 0600)

	creds, err := credentials.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds["my_key"] != "my_value" {
		t.Errorf("my_key: got %q, want my_value", creds["my_key"])
	}
}

// TestLoad_CommentsAndBlankLines verifies that comments and blank lines are ignored.
func TestLoad_CommentsAndBlankLines(t *testing.T) {
	t.Setenv("BOABOT_PROFILE", "default")
	dir := t.TempDir()
	ini := `
# full line comment
; another comment

[default]
# comment inside section
key1 = val1
; semicolon comment
key2 = val2
`
	p := writeCreds(t, dir, ini, 0600)

	creds, err := credentials.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds["key1"] != "val1" {
		t.Errorf("key1: got %q", creds["key1"])
	}
	if creds["key2"] != "val2" {
		t.Errorf("key2: got %q", creds["key2"])
	}
}

// TestGet_PrefersCredsMap verifies that Get returns the credentials map value
// when it is present, ignoring the env var.
func TestGet_PrefersCredsMap(t *testing.T) {
	t.Setenv("MY_ENV", "from-env")
	creds := map[string]string{"my_key": "from-creds"}
	got := credentials.Get(creds, "my_key", "MY_ENV")
	if got != "from-creds" {
		t.Errorf("expected from-creds, got %q", got)
	}
}

// TestGet_FallsBackToEnv verifies that Get falls back to the env var when the
// key is absent from the credentials map.
func TestGet_FallsBackToEnv(t *testing.T) {
	t.Setenv("MY_ENV", "from-env")
	creds := map[string]string{}
	got := credentials.Get(creds, "my_key", "MY_ENV")
	if got != "from-env" {
		t.Errorf("expected from-env, got %q", got)
	}
}

// TestGet_EmptyValueFallsBack verifies that an empty string in the creds map
// causes Get to fall back to the env var.
func TestGet_EmptyValueFallsBack(t *testing.T) {
	t.Setenv("MY_ENV", "from-env")
	creds := map[string]string{"my_key": ""}
	got := credentials.Get(creds, "my_key", "MY_ENV")
	if got != "from-env" {
		t.Errorf("expected from-env when map value is empty, got %q", got)
	}
}

// TestGet_NoEnvVar verifies that Get returns empty string when both the creds
// map and envVar are absent.
func TestGet_NoEnvVar(t *testing.T) {
	t.Setenv("MY_ENV", "")
	creds := map[string]string{}
	got := credentials.Get(creds, "my_key", "MY_ENV")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestGet_EmptyEnvVarName verifies that Get returns empty string when envVar
// is empty and creds map has no entry.
func TestGet_EmptyEnvVarName(t *testing.T) {
	creds := map[string]string{}
	got := credentials.Get(creds, "my_key", "")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestDefaultPath verifies that DefaultPath returns a path under the user's
// home directory.
func TestDefaultPath(t *testing.T) {
	p, err := credentials.DefaultPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == "" {
		t.Error("DefaultPath returned empty string")
	}
	home, _ := os.UserHomeDir()
	// Should be <home>/.boabot/credentials
	expected := filepath.Join(home, ".boabot", "credentials")
	if p != expected {
		t.Errorf("DefaultPath: got %q, want %q", p, expected)
	}
}

// TestLoad_StagingProfile verifies the staging profile round-trips correctly.
func TestLoad_StagingProfile(t *testing.T) {
	t.Setenv("BOABOT_PROFILE", "staging")
	dir := t.TempDir()
	p := writeCreds(t, dir, sampleINI, 0600)

	creds, err := credentials.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds["anthropic_api_key"] != "sk-ant-staging" {
		t.Errorf("anthropic_api_key: got %q, want sk-ant-staging", creds["anthropic_api_key"])
	}
}
