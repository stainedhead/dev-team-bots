package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Bot          BotConfig          `yaml:"bot"`
	Orchestrator OrchestratorConfig `yaml:"orchestrator"`
	AWS          AWSConfig          `yaml:"aws"`
	Models       ModelsConfig       `yaml:"models"`
	Tools        ToolsConfig        `yaml:"tools"`
	Budget       BudgetConfig       `yaml:"budget"`
	Context      ContextConfig      `yaml:"context"`
	Team         TeamFileConfig     `yaml:"team"`
	Memory       MemoryConfig       `yaml:"memory"`
}

// TeamFileConfig holds paths used by TeamManager to locate team.yaml and the
// per-bot configuration directories.  Fields are additive — M6 will populate
// them fully; for now they default to paths relative to the binary.
type TeamFileConfig struct {
	FilePath string `yaml:"file_path"`
	BotsDir  string `yaml:"bots_dir"`
}

// MemoryConfig holds the base path for per-bot memory and vector files.
// M6 will expand this with retention and quota settings.
type MemoryConfig struct {
	Path string `yaml:"path"`
}

type BotConfig struct {
	Name    string `yaml:"name"`
	BotType string `yaml:"type"`
}

type OrchestratorConfig struct {
	Enabled bool `yaml:"enabled"`
	APIPort int  `yaml:"api_port"`
	WebPort int  `yaml:"web_port"`
}

type AWSConfig struct {
	Region               string `yaml:"region"`
	SQSQueueURL          string `yaml:"sqs_queue_url"`
	SNSTopicARN          string `yaml:"sns_topic_arn"`
	PrivateBucket        string `yaml:"private_bucket"`
	TeamBucket           string `yaml:"team_bucket"`
	DynamoDBBudgetTable  string `yaml:"dynamodb_budget_table"`
	OrchestratorQueueURL string `yaml:"orchestrator_queue_url"`
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

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}
