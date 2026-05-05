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
	//   1.  AWS session (aws.LoadDefaultConfig)
	//   2.  Infrastructure adapters: SQS, SNS, S3, S3 Vectors, DynamoDB, Secrets Manager
	//   3.  MCP client: load mcp.json from shared S3 bucket, then private S3 bucket; merge;
	//         resolve credentials from Secrets Manager
	//   4.  Provider factory (Bedrock + OpenAI adapters)
	//   5.  BudgetTracker: seed counters from DynamoDB; start 30s flush goroutine
	//   6.  ToolScorer (BM25) and ToolGater
	//   7.  Worker factory (pre-wired with provider, MCP client, ToolGater, BudgetTracker)
	//   8.  Channel monitors (Slack, Teams)
	//   9.  CardRegistry (in-memory)
	//   10. Publish own Agent Card to private S3 bucket
	//   11. Request team_snapshot from orchestrator; populate CardRegistry
	//   12. Register with orchestrator (send register message)
	//   13. If orchestrator.enabled: start orchestrator services + HTTP servers
	//   14. RunAgentUseCase.Run(ctx) — blocks until ctx cancelled (SIGTERM/SIGINT)
	//   15. Shutdown: checkpoint active workers, broadcast shutdown, flush budget to DynamoDB
	_ = cfg
	<-ctx.Done()
	return nil
}
