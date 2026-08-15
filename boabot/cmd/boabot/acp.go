package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	sdk "github.com/coder/acp-go-sdk"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/team"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	acpinfra "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/acp"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/cliagent"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bm25"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/fs"
	localmcp "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/mcp"
	orchestratorlocal "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
	localplugin "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/plugin"
	localrules "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/rules"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/vector"
)

// singleWorkerFactory adapts one already-constructed domain.Worker to
// domain.WorkerFactory, mirroring team.simpleWorkerFactory's pattern.
// ACP mode services exactly one persona, so there is only ever one Worker
// to hand out (architecture.md AD-1/AD-2 -- no TeamManager involved).
type singleWorkerFactory struct{ w domain.Worker }

func (f singleWorkerFactory) New() domain.Worker { return f.w }

// defaultBotsDir returns <bin-dir>/bots, mirroring native daemon mode's own
// ManagerConfig.BotsDir default (main.go's run()) -- the same bots/<type>/
// layout, just resolved here for ACP mode's -agent flag.
func defaultBotsDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "bots"
	}
	return filepath.Join(filepath.Dir(exe), "bots")
}

// resolveACPConfigPath determines which persona config.yaml -acp mode
// loads. An explicitly-passed -config always wins (unchanged behavior from
// before -agent existed). Otherwise, agent (default "orchestrator") is
// resolved as <botsDir>/<agent>/config.yaml -- the same bots/<type>/
// config.yaml layout native daemon mode already uses, so an operator who
// already has a boabot-team/bots/ checkout can select a persona by name
// instead of typing out a full path.
func resolveACPConfigPath(configExplicit bool, configPath, botsDir, agent string) string {
	if configExplicit {
		return configPath
	}
	return filepath.Join(botsDir, agent, "config.yaml")
}

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

	worker := buildACPWorker(cfg, memPath, string(soulBytes), pf, provider, memStore, embedder, vecStore)

	// Mirrors team_manager.go's startBot exactly (RT3/FR-004, auto-review):
	// native mode wires a RulesTracker under this identical condition so the
	// persona loads AGENTS.md/CLAUDE.md hierarchically for tasks that carry
	// a WorkDir. ACP-mode tasks currently always have an empty WorkDir (see
	// implementation-notes.md), so this wiring is presently inert in
	// practice -- exactly as inert as it is for native mode's own
	// Slack/Buzz chat-triggered tasks, which also carry no WorkDir. Wiring
	// it anyway makes ACP mode's construction pattern identical to native
	// mode's, so neither mode silently diverges if WorkDir population is
	// ever added to either path.
	if len(cfg.Orchestrator.WorkDirs) > 0 {
		worker.WithRulesTracker(localrules.NewTracker(cfg.Orchestrator.WorkDirs))
	}

	var opts []acpinfra.Option
	if raw := os.Getenv("BOABOT_ACP_KEEPALIVE_INTERVAL"); raw != "" {
		d, parseErr := time.ParseDuration(raw)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid BOABOT_ACP_KEEPALIVE_INTERVAL %q: %w", raw, parseErr)
		}
		opts = append(opts, acpinfra.WithKeepAliveInterval(d))
	}

	return acpinfra.New(singleWorkerFactory{w: worker}, "", opts...), nil
}

// buildACPWorker constructs the *application.ExecuteTaskUseCase buildACPAgent
// wraps as the single persona's domain.Worker, given its already-loaded
// config, computed memPath, and SOUL.md system prompt. Extracted out of
// buildACPAgent -- which stays wiring-only per AGENTS.md's cmd/ convention --
// so FR-401's chat-provider selection and FR-402-405's board/plugin/CLI tool
// wiring are directly unit-testable, mirroring cmd/boabot/main.go's
// newBuzzMonitorBuilder extraction pattern (see its doc comment).
//
// pf and provider are passed in rather than rebuilt here so tests can supply
// a fake domain.ProviderFactory instead of constructing real
// anthropic/openai/bedrock clients (specs/260815-acp-harness-feature-parity).
func buildACPWorker(
	cfg config.Config,
	memPath, soulPrompt string,
	pf domain.ProviderFactory,
	provider domain.ModelProvider,
	memStore domain.MemoryStore,
	embedder domain.Embedder,
	vecStore domain.VectorStore,
) *application.ExecuteTaskUseCase {
	mcpOpts := buildACPMCPOptions(cfg, memPath)
	mcpClient := localmcp.NewClient(cfg.Orchestrator.WorkDirs, mcpOpts...)

	worker := application.NewExecuteTaskUseCase(provider, mcpClient, memStore, embedder, vecStore, soulPrompt)

	// FR-401 (scope addition beyond isConversationalSource's own extension --
	// see implementation-notes.md): mirrors team_manager.go:1047-1056's exact
	// gating condition. Without this, isConversationalSource recognizing
	// "acp" has no effect in production -- u.chatProvider stays nil for
	// every ACP-mode task, the same dead-code failure mode
	// docs/architectural-decision-record.md's ADR-B028 decision 4 already
	// documented for the pre-existing "chat" branch. A bad/unresolvable
	// chat_provider degrades gracefully (NFR-Reliability): logged, falls
	// back to the default provider, never blocks worker construction.
	if chatName := cfg.Models.ChatProvider; chatName != "" && chatName != cfg.Models.Default {
		if chatProvider, chatErr := pf.Get(chatName); chatErr != nil {
			slog.Warn("acp mode: chat provider unavailable; falling back to default for acp-sourced tasks",
				"bot", cfg.Bot.Name, "chat_provider", chatName, "err", chatErr)
		} else {
			worker.WithChatProvider(chatProvider)
			slog.Info("acp mode: chat provider activated", "bot", cfg.Bot.Name, "chat_provider", chatName)
		}
	}

	return worker
}

// buildACPMCPOptions constructs the functional options for ACP mode's local
// filesystem MCP client -- board store (FR-402/403), plugin store (FR-404),
// and CLI runner/tools (FR-405) -- mirroring team_manager.go:1020-1036's
// exact per-feature gating conditions (not an umbrella enabled flag; see
// architecture.md's "do not reuse orchestrator.enabled" decision). Every
// store construction here degrades gracefully on failure (NFR-Reliability):
// logged, ACP mode still starts and executes tasks without that tool
// surface. Startup logs state clearly whether each tool surface activated
// for this persona (NFR-Observability), mirroring how Buzz monitor
// activation is already logged in main.go.
func buildACPMCPOptions(cfg config.Config, memPath string) []func(*localmcp.Client) {
	var opts []func(*localmcp.Client)

	// Board store (FR-402/403): mirrors team_manager.go:1023-1024's persona-
	// type gate exactly (not an enabled flag -- tm.sharedBoard is always
	// non-nil there). NewInMemoryBoardStore has no failure path of its own
	// (a missing/corrupt persist file is silently treated as an empty
	// board), so there is nothing to log-and-skip here beyond activation.
	if cfg.Bot.BotType != "tech-lead" {
		boardPath := filepath.Join(memPath, "board.json")
		opts = append(opts, localmcp.WithBoardStore(orchestratorlocal.NewInMemoryBoardStore(boardPath)))
		slog.Info("acp mode: board store activated", "bot", cfg.Bot.Name, "path", boardPath)
	} else {
		slog.Info("acp mode: board store not activated (persona type is tech-lead)", "bot", cfg.Bot.Name)
	}

	// Plugin store (FR-404): mirrors team_manager.go:501-519's install-dir
	// gate and relative-path resolution against the persona's own memPath.
	if installDir := cfg.Orchestrator.Plugins.InstallDir; installDir != "" {
		if !filepath.IsAbs(installDir) {
			installDir = filepath.Join(memPath, installDir)
		}
		pluginStore, pluginErr := localplugin.NewLocalPluginStore(installDir)
		if pluginErr != nil {
			slog.Error("acp mode: plugin store construction failed; continuing without plugin tools",
				"bot", cfg.Bot.Name, "install_dir", installDir, "err", pluginErr)
		} else {
			opts = append(opts, localmcp.WithPluginStore(pluginStore), localmcp.WithInstallDir(installDir))
			slog.Info("acp mode: plugin store activated", "bot", cfg.Bot.Name, "install_dir", installDir)
		}
	} else {
		slog.Info("acp mode: plugin store not activated (orchestrator.plugins.install_dir not set)", "bot", cfg.Bot.Name)
	}

	// CLI tools (FR-405): runner is always wired, mirroring
	// team_manager.go:531's unconditional cliagent.New(); per-tool
	// availability is gated by each CLIToolConfig.Enabled bool inside
	// cfg.Orchestrator.CLITools (enforced by localmcp.Client itself via
	// resolveBinary -- WithCLITools just passes the config through).
	opts = append(opts, localmcp.WithCLIRunner(cliagent.New()), localmcp.WithCLITools(cfg.Orchestrator.CLITools))
	slog.Info("acp mode: cli runner activated", "bot", cfg.Bot.Name,
		"claude_code_enabled", cfg.Orchestrator.CLITools.ClaudeCode.Enabled,
		"codex_enabled", cfg.Orchestrator.CLITools.Codex.Enabled,
		"openai_codex_enabled", cfg.Orchestrator.CLITools.OpenAICodex.Enabled,
		"opencode_enabled", cfg.Orchestrator.CLITools.OpenCode.Enabled)

	return opts
}
