package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Bot          BotConfig          `yaml:"bot"`
	Orchestrator OrchestratorConfig `yaml:"orchestrator"`
	Models       ModelsConfig       `yaml:"models"`
	Tools        ToolsConfig        `yaml:"tools"`
	Budget       BudgetConfig       `yaml:"budget"`
	Context      ContextConfig      `yaml:"context"`
	Team         TeamFileConfig     `yaml:"team"`
	Memory       MemoryConfig       `yaml:"memory"`
	Backup       BackupConfig       `yaml:"backup"`
}

// TeamFileConfig holds paths used by TeamManager to locate team.yaml and the
// per-bot configuration directories.
type TeamFileConfig struct {
	FilePath string `yaml:"file_path"`
	BotsDir  string `yaml:"bots_dir"`
}

// MemoryConfig is the full memory configuration.
type MemoryConfig struct {
	Path        string `yaml:"path"`         // default: <binary-dir>/memory
	VectorIndex string `yaml:"vector_index"` // "cosine" (default) | "hnsw" (future)
	Embedder    string `yaml:"embedder"`     // "bm25" (default) | provider name
	HeapWarnMB  int    `yaml:"heap_warn_mb"` // 0 = disabled
	HeapHardMB  int    `yaml:"heap_hard_mb"` // 0 = disabled
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
	Enabled       bool   `yaml:"enabled"`
	APIPort       int    `yaml:"api_port"`
	WebPort       int    `yaml:"web_port"`
	JWTSecret     string `yaml:"jwt_secret"`     // generated randomly if empty
	AdminPassword string `yaml:"admin_password"` // defaults to "admin" if empty
}

type ModelsConfig struct {
	Default   string           `yaml:"default"`
	Providers []ProviderConfig `yaml:"providers"`
}

type ProviderConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	ModelID  string `yaml:"model_id"`
	Region   string `yaml:"region"`
	Endpoint string `yaml:"endpoint"`
}

type ToolsConfig struct {
	AllowedTools     []string `yaml:"allowed_tools"`
	HTTPAllowedHosts []string `yaml:"http_allowed_hosts"`
	ReceiveFrom      []string `yaml:"receive_from"`
}

type BudgetConfig struct {
	TokenSpendDaily int64 `yaml:"token_spend_daily"`
	ToolCallsHourly int   `yaml:"tool_calls_hourly"`
}

type ContextConfig struct {
	ThresholdTokens int `yaml:"threshold_tokens"`
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
