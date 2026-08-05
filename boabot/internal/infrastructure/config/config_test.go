package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestLoad_CLIToolsConfig verifies that cli_tools fields round-trip through YAML.
func TestLoad_CLIToolsConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeConfig(t, dir, `orchestrator:
  enabled: true
  cli_tools:
    claude_code:
      enabled: true
      binary_path: /usr/local/bin/claude
    codex:
      enabled: false
      binary_path: codex
    openai_codex:
      enabled: true
      binary_path: openai-codex
    opencode:
      enabled: false
      binary_path: opencode
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ct := cfg.Orchestrator.CLITools
	if !ct.ClaudeCode.Enabled {
		t.Error("expected ClaudeCode.Enabled=true")
	}
	if ct.ClaudeCode.BinaryPath != "/usr/local/bin/claude" {
		t.Errorf("ClaudeCode.BinaryPath: got %q", ct.ClaudeCode.BinaryPath)
	}
	if ct.Codex.Enabled {
		t.Error("expected Codex.Enabled=false")
	}
	if ct.Codex.BinaryPath != "codex" {
		t.Errorf("Codex.BinaryPath: got %q", ct.Codex.BinaryPath)
	}
	if !ct.OpenAICodex.Enabled {
		t.Error("expected OpenAICodex.Enabled=true")
	}
	if ct.OpenAICodex.BinaryPath != "openai-codex" {
		t.Errorf("OpenAICodex.BinaryPath: got %q", ct.OpenAICodex.BinaryPath)
	}
	if ct.OpenCode.Enabled {
		t.Error("expected OpenCode.Enabled=false")
	}
}

// TestLoad_CLIToolsConfig_MissingBlock verifies that missing cli_tools block
// uses safe defaults (all disabled).
func TestLoad_CLIToolsConfig_MissingBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeConfig(t, dir, `orchestrator:
  enabled: true
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ct := cfg.Orchestrator.CLITools
	if ct.ClaudeCode.Enabled {
		t.Error("expected ClaudeCode.Enabled=false by default")
	}
	if ct.Codex.Enabled {
		t.Error("expected Codex.Enabled=false by default")
	}
	if ct.OpenAICodex.Enabled {
		t.Error("expected OpenAICodex.Enabled=false by default")
	}
	if ct.OpenCode.Enabled {
		t.Error("expected OpenCode.Enabled=false by default")
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

// TestLoad_BuzzConfig verifies that BuzzConfig's fields (FR-035) round-trip
// through YAML.
func TestLoad_BuzzConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeConfig(t, dir, `buzz:
  enabled: true
  relay_url: wss://relay.example.com
  bot_name: tech-lead
  owner_pubkey: abc123
  respond_to: def456
  respond_to_allowlist:
    - def456
    - ghi789
  presence_interval: 45s
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b := cfg.Buzz
	if !b.Enabled {
		t.Error("expected Buzz.Enabled=true")
	}
	if b.RelayURL != "wss://relay.example.com" {
		t.Errorf("RelayURL: got %q", b.RelayURL)
	}
	if b.BotName != "tech-lead" {
		t.Errorf("BotName: got %q", b.BotName)
	}
	if b.OwnerPubkey != "abc123" {
		t.Errorf("OwnerPubkey: got %q", b.OwnerPubkey)
	}
	if b.RespondTo != "def456" {
		t.Errorf("RespondTo: got %q", b.RespondTo)
	}
	if len(b.RespondToAllowlist) != 2 || b.RespondToAllowlist[0] != "def456" || b.RespondToAllowlist[1] != "ghi789" {
		t.Errorf("RespondToAllowlist: got %v", b.RespondToAllowlist)
	}
	if time.Duration(b.PresenceInterval) != 45*time.Second {
		t.Errorf("PresenceInterval: got %v, want 45s", time.Duration(b.PresenceInterval))
	}
}

// TestLoad_BuzzConfig_MissingBlock verifies that an absent buzz: block
// leaves BuzzConfig at its zero value (Enabled=false), matching Slack's
// "activation requires everything present" pattern.
func TestLoad_BuzzConfig_MissingBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeConfig(t, dir, `bot:
  name: mybot
  type: worker
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Buzz.Enabled {
		t.Error("expected Buzz.Enabled=false by default")
	}
	if cfg.Buzz.PresenceInterval != 0 {
		t.Errorf("expected zero PresenceInterval by default, got %v", cfg.Buzz.PresenceInterval)
	}
}

// TestLoad_BuzzSecretLikeKeyRejected verifies FR-035's "reject any
// secret-looking key under the buzz: block" requirement. Load's
// yaml.Decoder.KnownFields(true) already rejects any key that is not a
// literal BuzzConfig field, so a plausible-looking secret key such as
// buzz.nsec or buzz.private_key produces a clear "field ... not found"
// decode error rather than being silently ignored or accepted.
func TestLoad_BuzzSecretLikeKeyRejected(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"nsec", "private_key", "api_token", "auth_tag", "buzz_private_key", "secret"} {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			p := writeConfig(t, dir, "buzz:\n  enabled: true\n  "+key+": should-be-rejected\n")
			_, err := config.Load(p)
			if err == nil {
				t.Fatalf("expected an error for buzz.%s, got nil", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("expected error to name the offending key %q, got: %v", key, err)
			}
		})
	}
}

// TestLoad_BuzzChannelsKeyRejected verifies the H1 judgment call documented
// on BuzzConfig: since Phase F's channel discovery is fully dynamic
// (kind:39000/39002 + membership events), no static "channels" field
// exists on BuzzConfig, so a buzz.channels: key is a config-load error
// (KnownFields(true)), not silently-ignored dead config.
func TestLoad_BuzzChannelsKeyRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeConfig(t, dir, `buzz:
  enabled: true
  channels:
    - some-channel-uuid
`)
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("expected an error for buzz.channels (not a BuzzConfig field), got nil")
	}
	if !strings.Contains(err.Error(), "channels") {
		t.Errorf("expected error to name 'channels', got: %v", err)
	}
}

// TestLoad_BuzzPresenceIntervalTooHighRejected verifies FR-023's 180s
// staleness bound is enforced at config-load time, per Config's own doc
// comment in internal/infrastructure/buzz/monitor.go assigning that
// validation to config-loading rather than Monitor.
func TestLoad_BuzzPresenceIntervalTooHighRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeConfig(t, dir, `buzz:
  enabled: true
  presence_interval: 180s
`)
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("expected an error for presence_interval >= 180s, got nil")
	}
	if !strings.Contains(err.Error(), "180") {
		t.Errorf("expected error to mention the 180s bound, got: %v", err)
	}
}

// TestLoad_BuzzPresenceIntervalUnderBoundAccepted verifies a value safely
// under the 180s bound loads without error.
func TestLoad_BuzzPresenceIntervalUnderBoundAccepted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writeConfig(t, dir, `buzz:
  presence_interval: 90s
`)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if time.Duration(cfg.Buzz.PresenceInterval) != 90*time.Second {
		t.Errorf("PresenceInterval: got %v, want 90s", time.Duration(cfg.Buzz.PresenceInterval))
	}
}
