package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
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
	// TODO: wire infrastructure adapters and use cases, then start agent.
	// Wiring order:
	//   1. AWS config (aws.LoadDefaultConfig)
	//   2. Infrastructure adapters (SQS, SNS, S3, Bedrock, Secrets)
	//   3. MCP client (load mcp.json from S3)
	//   4. Provider factory
	//   5. Worker factory
	//   6. Channel monitors (Slack, Teams)
	//   7. RunAgentUseCase
	//   8. If orchestrator.enabled: orchestrator services + HTTP server
	//   9. agent.Run(ctx) — blocks until ctx cancelled
	//  10. agent.Shutdown(ctx) — broadcast shutdown, drain workers
	_ = cfg
	<-ctx.Done()
	return nil
}
