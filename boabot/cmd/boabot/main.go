package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/team"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bus"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
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
	router := queue.NewRouter()
	b := bus.New()

	managerCfg := team.ManagerConfig{
		TeamFilePath:    cfg.Team.FilePath,
		BotsDir:         cfg.Team.BotsDir,
		MemoryRoot:      cfg.Memory.Path,
		RestartDelay:    time.Second,
		MaxRestartDelay: 5 * time.Minute,
	}

	// Apply sensible binary-relative defaults for fields not yet in the config
	// file.  M6 will wire these from config.yaml fully.
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
	return mgr.Run(ctx)
}
