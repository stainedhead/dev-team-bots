package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/team"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	acpinfra "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/acp"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bm25"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/fs"
	localmcp "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/mcp"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/vector"
)

// singleWorkerFactory adapts one already-constructed domain.Worker to
// domain.WorkerFactory, mirroring team.simpleWorkerFactory's pattern.
// ACP mode services exactly one persona, so there is only ever one Worker
// to hand out (architecture.md AD-1/AD-2 -- no TeamManager involved).
type singleWorkerFactory struct{ w domain.Worker }

func (f singleWorkerFactory) New() domain.Worker { return f.w }

// runACP loads configPath as a single persona's config (the same
// boabot-team/bots/<type>/config.yaml shape native daemon mode uses -- FR-004)
// and serves it as an ACP agent over stdio until the peer disconnects.
func runACP(ctx context.Context, configPath string) error {
	agent, err := buildACPAgent(configPath)
	if err != nil {
		return fmt.Errorf("build acp agent: %w", err)
	}

	conn := sdk.NewAgentSideConnection(agent, os.Stdout, os.Stdin)
	conn.SetLogger(slog.Default())
	agent.SetConnection(conn)

	select {
	case <-conn.Done():
	case <-ctx.Done():
	}
	return nil
}

// buildACPAgent constructs the single-persona *acpinfra.Agent for
// configPath, reusing the exact construction primitives native daemon mode
// uses (TeamManager.startBot) -- provider factory, BM25 embedder, local
// filesystem memory/vector stores, and the local filesystem MCP client --
// without going through team.yaml or TeamManager at all, since ACP mode
// has no multi-bot orchestration to coordinate (architecture.md).
func buildACPAgent(configPath string) (*acpinfra.Agent, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config %q: %w", configPath, err)
	}

	// SOUL.md lives alongside this persona's own config.yaml -- e.g.
	// boabot-team/bots/tech-lead/{config.yaml,SOUL.md} -- so no botsDir/
	// team.yaml lookup is needed, unlike native mode's per-team-member path.
	soulPath := filepath.Join(filepath.Dir(configPath), "SOUL.md")
	soulBytes, err := os.ReadFile(soulPath)
	if err != nil {
		return nil, fmt.Errorf("read SOUL.md for %q: %w", cfg.Bot.Name, err)
	}

	memRoot := cfg.Memory.Path
	if memRoot == "" {
		exe, _ := os.Executable()
		memRoot = filepath.Join(filepath.Dir(exe), "memory")
	}
	memPath := filepath.Join(memRoot, cfg.Bot.Name)

	memStore, err := fs.New(memPath)
	if err != nil {
		return nil, fmt.Errorf("create memory store for %q: %w", cfg.Bot.Name, err)
	}
	vecStore, err := vector.New(memPath)
	if err != nil {
		return nil, fmt.Errorf("create vector store for %q: %w", cfg.Bot.Name, err)
	}

	pf := team.NewLocalProviderFactory(cfg.Models.Providers)
	provider, err := pf.Get(cfg.Models.Default)
	if err != nil {
		return nil, fmt.Errorf("get provider %q for %q: %w", cfg.Models.Default, cfg.Bot.Name, err)
	}

	embedder := domain.Embedder(bm25.DefaultEmbedder())

	mcpClient := localmcp.NewClient(cfg.Orchestrator.WorkDirs)

	worker := application.NewExecuteTaskUseCase(provider, mcpClient, memStore, embedder, vecStore, string(soulBytes))

	return acpinfra.New(singleWorkerFactory{w: worker}, ""), nil
}
