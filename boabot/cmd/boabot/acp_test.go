package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeACPTestPersona(t *testing.T, configYAML string) (configPath string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("You are a test persona."), 0o600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}
	configPath = filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return configPath
}

func TestBuildACPAgent_ValidPersonaSucceeds(t *testing.T) {
	configPath := writeACPTestPersona(t, `
bot:
  name: test-bot
  type: test-bot
models:
  default: test-provider
  providers:
    - name: test-provider
      type: anthropic
      model_id: claude-x
`)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	agent, err := buildACPAgent(configPath)
	if err != nil {
		t.Fatalf("buildACPAgent returned error: %v", err)
	}
	if agent == nil {
		t.Fatal("buildACPAgent returned a nil Agent with no error")
	}
}

func TestBuildACPAgent_MissingSOULFails(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
bot:
  name: test-bot
  type: test-bot
models:
  default: test-provider
  providers:
    - name: test-provider
      type: anthropic
      model_id: claude-x
`), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	if _, err := buildACPAgent(configPath); err == nil {
		t.Fatal("expected an error when SOUL.md is missing, got nil")
	}
}

func TestBuildACPAgent_InvalidKeepAliveIntervalFails(t *testing.T) {
	configPath := writeACPTestPersona(t, `
bot:
  name: test-bot
  type: test-bot
models:
  default: test-provider
  providers:
    - name: test-provider
      type: anthropic
      model_id: claude-x
`)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("BOABOT_ACP_KEEPALIVE_INTERVAL", "not-a-duration")

	if _, err := buildACPAgent(configPath); err == nil {
		t.Fatal("expected an error for an invalid BOABOT_ACP_KEEPALIVE_INTERVAL, got nil")
	}
}

func TestBuildACPAgent_UnknownProviderFails(t *testing.T) {
	configPath := writeACPTestPersona(t, `
bot:
  name: test-bot
  type: test-bot
models:
  default: does-not-exist
  providers: []
`)

	if _, err := buildACPAgent(configPath); err == nil {
		t.Fatal("expected an error for an unresolvable default provider, got nil")
	}
}
