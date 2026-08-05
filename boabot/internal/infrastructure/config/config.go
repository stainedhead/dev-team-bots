package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// presenceStalenessBound is FR-023's 180s presence-staleness limit; a
// configured buzz.presence_interval MUST stay strictly under it (Monitor's
// own Config doc comment assigns this validation to config-loading, since
// Monitor itself never validates operator-supplied durations).
const presenceStalenessBound = 180 * time.Second

type Config struct {
	Bot          BotConfig          `yaml:"bot"`
	Orchestrator OrchestratorConfig `yaml:"orchestrator"`
	Models       ModelsConfig       `yaml:"models"`
	Team         TeamFileConfig     `yaml:"team"`
	Memory       MemoryConfig       `yaml:"memory"`
	Backup       BackupConfig       `yaml:"backup"`
	Slack        SlackConfig        `yaml:"slack"`
	Buzz         BuzzConfig         `yaml:"buzz"`
}

// Duration wraps time.Duration so buzz.presence_interval (and any future
// duration field) can be written in config.yaml as a Go duration string
// (e.g. "60s", "3m") rather than raw nanoseconds.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler over a duration string.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// BuzzConfig holds the Buzz (Nostr relay) channel monitor's non-secret
// connection settings (FR-035). It mirrors SlackConfig's shape: all fields
// are optional, and cmd/boabot/main.go only activates the Buzz monitor when
// Enabled is true and the required settings (RelayURL, BotName) resolve —
// mirroring Slack's all-or-nothing activation pattern (FR-036).
//
// Secret material — the agent's private key/nsec, an optional NIP-OA auth
// tag, and BUZZ_API_TOKEN — MUST NOT appear under this block: it resolves
// only through the FR-002 domain.SecretStore credential path (env var,
// systemd credential, OS keystore, or ~/.boabot/credentials — see
// internal/infrastructure/buzz.PrivateKeySecretName/APITokenSecretName/
// AuthTagSecretName, all three resolved and wired by
// cmd/boabot/main.go's buildBuzzMonitor).
// Load's yaml.Decoder.KnownFields(true) already rejects any key under
// buzz: that is not one of the fields below, with a clear
// "field <name> not found in type config.BuzzConfig" error — proof (not an
// additional guard) is in config_test.go's
// TestLoad_BuzzSecretLikeKeyRejected* cases.
//
// Deliberately NOT present: a static "channels" list. FR-035's field list
// names one, but internal/infrastructure/buzz's Phase F channel
// participation (discovery.go) subscribes to every channel the bot is a
// relay-confirmed member of (kind:39000/39002 discovery + kind:44100/44101
// membership events) entirely dynamically — no code path in that package
// reads or would honour a static channel list. Adding the field here would
// be dead config: an operator who writes buzz.channels: would reasonably
// believe it scopes which channels the bot joins, and nothing would ever
// read it. Omitting the field means a buzz.channels: key is instead a hard
// "field not found" config-load error — the honest outcome — until (if
// ever) a future phase actually threads static channel scoping through
// Monitor/discovery.go.
type BuzzConfig struct {
	Enabled            bool     `yaml:"enabled"`
	RelayURL           string   `yaml:"relay_url"`
	BotName            string   `yaml:"bot_name"`
	OwnerPubkey        string   `yaml:"owner_pubkey"`
	RespondTo          string   `yaml:"respond_to"`
	RespondToAllowlist []string `yaml:"respond_to_allowlist"`
	// PresenceInterval is FR-023's kind:20001 publish interval. Zero (the
	// default) leaves internal/infrastructure/buzz.Monitor's own default
	// (well under the 180s bound) in effect. A non-zero value at or above
	// presenceStalenessBound is rejected by Load with a clear error.
	PresenceInterval Duration `yaml:"presence_interval"`
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

	if d := time.Duration(cfg.Buzz.PresenceInterval); d != 0 {
		if d < 0 {
			return Config{}, fmt.Errorf("config: buzz.presence_interval must be positive, got %s", d)
		}
		if d >= presenceStalenessBound {
			return Config{}, fmt.Errorf("config: buzz.presence_interval (%s) must be under the 180s FR-023 staleness bound", d)
		}
	}

	return cfg, nil
}
