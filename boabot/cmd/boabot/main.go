package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/team"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/credentials"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bus"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/watchdog"
	slackinfra "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/slack"
)

var version = "dev"

func main() {
	configPath := flag.String("config", defaultConfigPath(), "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "err", err)
		os.Exit(1)
	}

	slog.Info("starting boabot", "name", cfg.Bot.Name, "type", cfg.Bot.BotType, "version", version)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := run(ctx, cfg); err != nil {
		slog.Error("agent exited with error", "err", err)
		os.Exit(1)
	}
}

// defaultConfigPath returns the path to config.yaml next to the running binary.
func defaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(filepath.Dir(exe), "config.yaml")
}

func run(ctx context.Context, cfg config.Config) error {
	// Load credentials file and apply to environment.
	credsPath, err := credentials.DefaultPath()
	if err != nil {
		slog.Warn("could not determine credentials file path", "err", err)
	} else {
		creds, err := credentials.Load(credsPath)
		if err != nil {
			return fmt.Errorf("credentials: %w", err) // world-readable file → fatal
		}
		// Override ANTHROPIC_API_KEY and BOABOT_BACKUP_TOKEN from credentials file
		// only if the env var is not already set.
		applyCredential(creds, "anthropic_api_key", "ANTHROPIC_API_KEY")
		applyCredential(creds, "boabot_backup_token", "BOABOT_BACKUP_TOKEN")
	}

	router := queue.NewRouter()
	b := bus.New()

	managerCfg := team.ManagerConfig{
		TeamFilePath:    cfg.Team.FilePath,
		BotsDir:         cfg.Team.BotsDir,
		MemoryRoot:      cfg.Memory.Path,
		AllowedWorkDirs: cfg.Orchestrator.WorkDirs,
		RestartDelay:    time.Second,
		MaxRestartDelay: 5 * time.Minute,
		WatchdogCfg: watchdog.Config{
			SampleInterval: 30 * time.Second,
			WarnMB:         cfg.Memory.HeapWarnMB,
			HardMB:         cfg.Memory.HeapHardMB,
		},
	}

	// Apply sensible binary-relative defaults for path fields.
	exe, _ := os.Executable()
	binDir := filepath.Dir(exe)

	if managerCfg.TeamFilePath == "" {
		managerCfg.TeamFilePath = filepath.Join(binDir, "team.yaml")
	}
	if managerCfg.BotsDir == "" {
		managerCfg.BotsDir = filepath.Join(binDir, "bots")
	}
	if managerCfg.MemoryRoot == "" {
		managerCfg.MemoryRoot = filepath.Join(binDir, "memory")
	}

	mgr := team.NewTeamManager(managerCfg, router, b)

	// Wire the Slack Socket Mode monitor when all three credentials are present.
	if cfg.Slack.BotToken != "" && cfg.Slack.AppToken != "" && cfg.Slack.BotName != "" {
		// Ensure the target bot's queue is registered before we try to obtain it.
		// (All enabled bots are registered inside mgr.Run, but the monitor needs
		// a queue reference at construction time — we register it here so the
		// router has it before Run is called.)
		router.Register(cfg.Slack.BotName, 0)
		slackMon := slackinfra.New(slackinfra.Config{
			BotToken: cfg.Slack.BotToken,
			AppToken: cfg.Slack.AppToken,
			BotName:  cfg.Slack.BotName,
		}, router.QueueFor(cfg.Slack.BotName))
		mgr.WithSlackMonitor(slackMon)
		slog.Info("slack socket mode monitor configured", "bot", cfg.Slack.BotName)
	}

	return mgr.Run(ctx)
}

// applyCredential sets envKey from the credentials map if the env var is not
// already set.
func applyCredential(creds map[string]string, credKey, envKey string) {
	if os.Getenv(envKey) == "" {
		if v := credentials.Get(creds, credKey, ""); v != "" {
			os.Setenv(envKey, v) //nolint:errcheck
		}
	}
}
