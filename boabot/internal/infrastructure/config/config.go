package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

type Config struct {
	Bot          BotConfig          `yaml:"bot"`
	Orchestrator OrchestratorConfig `yaml:"orchestrator"`
	Models       ModelsConfig       `yaml:"models"`
	Team         TeamFileConfig     `yaml:"team"`
	Memory       MemoryConfig       `yaml:"memory"`
	Backup       BackupConfig       `yaml:"backup"`
	Slack        SlackConfig        `yaml:"slack"`
}

// SlackConfig holds the Slack Socket Mode connection settings.
// All fields are optional; the monitor is only activated when BotToken,
// AppToken, and BotName are all non-empty.
type SlackConfig struct {
	BotToken string `yaml:"bot_token"` // xoxb-...
	AppToken string `yaml:"app_token"` // xapp-... (Socket Mode)
	BotName  string `yaml:"bot_name"`  // which bot handles Slack messages
}

// ResolveSecrets fills in BotToken/AppToken from store when the
// corresponding inline config.yaml field is empty (FR-047), namespaced by
// BotName per the domain.SecretStore convention (SecretRef.Bot). The logical
// secret names are "slack_bot_token" and "slack_app_token".
//
// When an inline field is already set, it is used as configured and a
// deprecation warning naming the preferred alternative (a
// ~/.boabot/credentials entry or an OS keystore secret) is logged — this is
// FR-048's warn-only clause (the post-deprecation-period hard rejection is
// out of scope for this run; see tasks.md's Deferred Items).
//
// When BotName is empty, Slack can never activate (see cmd/boabot/main.go's
// activation gate, which requires BotToken, AppToken, and BotName all
// non-empty), so ResolveSecrets skips the store entirely rather than paying
// for a keystore/systemd round trip that can never matter.
func (s *SlackConfig) ResolveSecrets(ctx context.Context, store domain.SecretStore, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	if s.BotName == "" {
		return
	}
	s.BotToken = resolveSlackToken(ctx, store, logger, s.BotName, "bot_token", "slack_bot_token", s.BotToken)
	s.AppToken = resolveSlackToken(ctx, store, logger, s.BotName, "app_token", "slack_app_token", s.AppToken)
}

// resolveSlackToken implements the single-field resolution rule documented
// on SlackConfig.ResolveSecrets.
func resolveSlackToken(ctx context.Context, store domain.SecretStore, logger *slog.Logger, botName, fieldName, secretName, inline string) string {
	if inline != "" {
		logger.Warn(
			"deprecated: inline Slack token in config.yaml; prefer a ~/.boabot/credentials entry or an OS keystore secret instead",
			"field", "slack."+fieldName,
			"secret_name", secretName,
		)
		return inline
	}
	if store == nil {
		return ""
	}
	v, err := store.Get(ctx, domain.SecretRef{Name: secretName, Bot: botName})
	if err != nil {
		return ""
	}
	return v
}

// TeamFileConfig holds paths used by TeamManager to locate team.yaml and the
// per-bot configuration directories.
type TeamFileConfig struct {
	FilePath string `yaml:"file_path"`
	BotsDir  string `yaml:"bots_dir"`
}

// MemoryConfig is the full memory configuration.
type MemoryConfig struct {
	Path       string `yaml:"path"`         // default: <binary-dir>/memory
	Embedder   string `yaml:"embedder"`     // "bm25" (default) | provider name
	HeapWarnMB int    `yaml:"heap_warn_mb"` // 0 = disabled
	HeapHardMB int    `yaml:"heap_hard_mb"` // 0 = disabled
}

// BackupConfig controls the scheduled GitHub memory backup.
type BackupConfig struct {
	Enabled        bool             `yaml:"enabled"`
	Schedule       string           `yaml:"schedule"` // cron; default "*/30 * * * *"
	RestoreOnEmpty bool             `yaml:"restore_on_empty"`
	GitHub         GitHubBackupConf `yaml:"github"`
}

// GitHubBackupConf holds GitHub-specific backup settings.
// The token is read from BOABOT_BACKUP_TOKEN env var or credentials file —
// never from config.yaml.
type GitHubBackupConf struct {
	Repo        string `yaml:"repo"`
	Branch      string `yaml:"branch"` // default: "main"
	AuthorName  string `yaml:"author_name"`
	AuthorEmail string `yaml:"author_email"`
}

type BotConfig struct {
	Name    string `yaml:"name"`
	BotType string `yaml:"type"`
}

type OrchestratorConfig struct {
	Enabled       bool           `yaml:"enabled"`
	APIPort       int            `yaml:"api_port"`
	JWTSecret     string         `yaml:"jwt_secret"`     // generated randomly if empty
	AdminPassword string         `yaml:"admin_password"` // defaults to "admin" if empty
	WorkDirs      []string       `yaml:"work_dirs"`      // allowed base directories for board item workspaces
	RetentionDays int            `yaml:"retention_days"` // auto-delete done board items and tasks older than this; default 10
	MaxConcurrent int            `yaml:"max_concurrent"` // max items running in-progress simultaneously (1-7); default 3
	Plugins       PluginsConfig  `yaml:"plugins"`
	CLITools      CLIToolsConfig `yaml:"cli_tools"`
}

// CLIToolConfig configures a single CLI agent tool.
type CLIToolConfig struct {
	// Enabled controls whether the tool is available. Defaults to false.
	Enabled bool `yaml:"enabled"`
	// BinaryPath overrides the default binary name (e.g. "claude", "codex").
	// May be an absolute path or a name resolved via PATH.
	BinaryPath string `yaml:"binary_path"`
}

// CLIToolsConfig groups configuration for all supported CLI agent tools.
type CLIToolsConfig struct {
	ClaudeCode  CLIToolConfig `yaml:"claude_code"`
	Codex       CLIToolConfig `yaml:"codex"`
	OpenAICodex CLIToolConfig `yaml:"openai_codex"`
	OpenCode    CLIToolConfig `yaml:"opencode"`
}

// PluginsConfig configures the plugin registry and installer.
type PluginsConfig struct {
	InstallDir string                 `yaml:"install_dir"`
	Registries []PluginRegistryConfig `yaml:"registries"`
	AutoUpdate bool                   `yaml:"auto_update"`
}

// PluginRegistryConfig is a statically configured plugin registry.
type PluginRegistryConfig struct {
	Name    string `yaml:"name"`
	URL     string `yaml:"url"`
	Trusted bool   `yaml:"trusted"`
}

type ModelsConfig struct {
	Default      string           `yaml:"default"`
	ChatProvider string           `yaml:"chat_provider"` // provider name used for chat-source tasks; falls back to Default
	Providers    []ProviderConfig `yaml:"providers"`
}

type ProviderConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	ModelID  string `yaml:"model_id"`
	Endpoint string `yaml:"endpoint"`
	// WorkDir is the working directory for subprocess-based providers (claude_code, codex).
	WorkDir string `yaml:"work_dir"`
	// BinaryPath overrides the default CLI binary name/path for subprocess providers.
	// Defaults to "claude" for claude_code and "codex" for codex.
	BinaryPath string `yaml:"binary_path"`
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}
