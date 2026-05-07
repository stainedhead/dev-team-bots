package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
)

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return p
}

// TestLoad_MinimalConfig verifies that a minimal config file loads without error
// and default zero values are set for unspecified fields.
func TestLoad_MinimalConfig(t *testing.T) {
	dir := t.TempDir()
	p := writeConfig(t, dir, `bot:
  name: mybot
  type: worker
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Bot.Name != "mybot" {
		t.Errorf("expected bot.name=mybot, got %q", cfg.Bot.Name)
	}
	if cfg.Bot.BotType != "worker" {
		t.Errorf("expected bot.type=worker, got %q", cfg.Bot.BotType)
	}
}

// TestLoad_MemoryConfig verifies that all MemoryConfig fields round-trip through YAML.
func TestLoad_MemoryConfig(t *testing.T) {
	dir := t.TempDir()
	p := writeConfig(t, dir, `memory:
  path: /data/memory
  embedder: openai
  heap_warn_mb: 512
  heap_hard_mb: 1024
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := cfg.Memory
	if m.Path != "/data/memory" {
		t.Errorf("Path: got %q, want /data/memory", m.Path)
	}
	if m.Embedder != "openai" {
		t.Errorf("Embedder: got %q, want openai", m.Embedder)
	}
	if m.HeapWarnMB != 512 {
		t.Errorf("HeapWarnMB: got %d, want 512", m.HeapWarnMB)
	}
	if m.HeapHardMB != 1024 {
		t.Errorf("HeapHardMB: got %d, want 1024", m.HeapHardMB)
	}
}

// TestLoad_BackupConfig verifies that BackupConfig fields round-trip through YAML.
func TestLoad_BackupConfig(t *testing.T) {
	dir := t.TempDir()
	p := writeConfig(t, dir, `backup:
  enabled: true
  schedule: "*/15 * * * *"
  restore_on_empty: true
  github:
    repo: org/repo
    branch: backup
    author_name: BaoBot
    author_email: baobot@example.com
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b := cfg.Backup
	if !b.Enabled {
		t.Error("expected Backup.Enabled=true")
	}
	if b.Schedule != "*/15 * * * *" {
		t.Errorf("Schedule: got %q, want '*/15 * * * *'", b.Schedule)
	}
	if !b.RestoreOnEmpty {
		t.Error("expected Backup.RestoreOnEmpty=true")
	}
	if b.GitHub.Repo != "org/repo" {
		t.Errorf("GitHub.Repo: got %q, want org/repo", b.GitHub.Repo)
	}
	if b.GitHub.Branch != "backup" {
		t.Errorf("GitHub.Branch: got %q, want backup", b.GitHub.Branch)
	}
	if b.GitHub.AuthorName != "BaoBot" {
		t.Errorf("GitHub.AuthorName: got %q, want BaoBot", b.GitHub.AuthorName)
	}
	if b.GitHub.AuthorEmail != "baobot@example.com" {
		t.Errorf("GitHub.AuthorEmail: got %q, want baobot@example.com", b.GitHub.AuthorEmail)
	}
}

// TestLoad_TeamFileConfig verifies that TeamFileConfig fields round-trip through YAML.
func TestLoad_TeamFileConfig(t *testing.T) {
	dir := t.TempDir()
	p := writeConfig(t, dir, `team:
  file_path: /etc/boabot/team.yaml
  bots_dir: /etc/boabot/bots
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Team.FilePath != "/etc/boabot/team.yaml" {
		t.Errorf("Team.FilePath: got %q", cfg.Team.FilePath)
	}
	if cfg.Team.BotsDir != "/etc/boabot/bots" {
		t.Errorf("Team.BotsDir: got %q", cfg.Team.BotsDir)
	}
}

// TestLoad_FullConfig verifies a comprehensive config with all sections.
func TestLoad_FullConfig(t *testing.T) {
	dir := t.TempDir()
	p := writeConfig(t, dir, `bot:
  name: fullbot
  type: orchestrator
orchestrator:
  enabled: true
  api_port: 8080
models:
  default: claude
  providers:
    - name: claude
      type: anthropic
      model_id: claude-opus-4-5
team:
  file_path: ./team.yaml
  bots_dir: ./bots
memory:
  path: ./memory
  embedder: bm25
  heap_warn_mb: 256
  heap_hard_mb: 512
backup:
  enabled: false
  schedule: "*/30 * * * *"
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Bot.Name != "fullbot" {
		t.Errorf("Bot.Name: got %q", cfg.Bot.Name)
	}
	if !cfg.Orchestrator.Enabled {
		t.Error("expected Orchestrator.Enabled=true")
	}
	if cfg.Orchestrator.APIPort != 8080 {
		t.Errorf("APIPort: got %d", cfg.Orchestrator.APIPort)
	}
	if cfg.Memory.HeapWarnMB != 256 {
		t.Errorf("Memory.HeapWarnMB: got %d", cfg.Memory.HeapWarnMB)
	}
	if cfg.Backup.Schedule != "*/30 * * * *" {
		t.Errorf("Backup.Schedule: got %q", cfg.Backup.Schedule)
	}
}

// TestLoad_MissingFile verifies that loading a non-existent file returns an error.
func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestLoad_InvalidYAML verifies that malformed YAML returns an error.
func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	p := writeConfig(t, dir, "bot: [\ninvalid yaml{{")
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

// TestLoad_ProviderConfig verifies that multiple provider configs parse correctly.
func TestLoad_ProviderConfig(t *testing.T) {
	dir := t.TempDir()
	p := writeConfig(t, dir, `models:
  default: openai-gpt4
  providers:
    - name: openai-gpt4
      type: openai
      model_id: gpt-4o
      endpoint: https://api.openai.com/v1
    - name: local
      type: ollama
      model_id: llama3
      endpoint: http://localhost:11434
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Models.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(cfg.Models.Providers))
	}
	if cfg.Models.Providers[0].Type != "openai" {
		t.Errorf("provider[0].type: got %q", cfg.Models.Providers[0].Type)
	}
	if cfg.Models.Providers[1].ModelID != "llama3" {
		t.Errorf("provider[1].model_id: got %q", cfg.Models.Providers[1].ModelID)
	}
}

// TestLoad_AWSBlockRejected verifies that a config file containing an aws: block
// is rejected at parse time with a clear error (AC-15).
func TestLoad_AWSBlockRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeConfig(t, dir, `aws:
  region: us-east-1
  sqs_queue_url: https://sqs.us-east-1.amazonaws.com/123/queue
`)
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("expected error for config with aws: block, got nil")
	}
	if !strings.Contains(err.Error(), "aws") {
		t.Errorf("expected error message to mention 'aws', got: %v", err)
	}
}

// TestLoad_UnknownFieldRejected verifies that any unknown top-level field is
// rejected (not silently ignored), ensuring strict schema enforcement.
func TestLoad_UnknownFieldRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeConfig(t, dir, `unknown_field: should-fail`)
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

// TestLoad_OrchestratorJWTAndAdminPassword verifies that the new jwt_secret and
// admin_password fields in OrchestratorConfig round-trip through YAML.
func TestLoad_OrchestratorJWTAndAdminPassword(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeConfig(t, dir, `orchestrator:
  enabled: true
  api_port: 9090
  jwt_secret: mysecret
  admin_password: mypassword
`)

	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Orchestrator.JWTSecret != "mysecret" {
		t.Errorf("JWTSecret: got %q, want mysecret", cfg.Orchestrator.JWTSecret)
	}
	if cfg.Orchestrator.AdminPassword != "mypassword" {
		t.Errorf("AdminPassword: got %q, want mypassword", cfg.Orchestrator.AdminPassword)
	}
}
