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
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/sharedstate"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/vector"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/watchdog"

	apporchestrator "github.com/stainedhead/dev-team-bots/boabot/internal/application/orchestrator"
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
	agent, cfg, err := buildACPAgent(configPath)
	if err != nil {
		return fmt.Errorf("build acp agent: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// FR-505 (spec.md): mirrors team_manager.go's identical gate and
	// shutdown-via-cancel wiring -- ACP mode gets the same graceful
	// heap-limit shutdown native-mode bots already have. cancel here stops
	// this function's own select below, which returns nil (no
	// heap-hard-limit-specific error path) -- matching native mode's own
	// watchdog, which triggers ordinary shutdown, not a distinguished error.
	if cfg.Memory.HeapWarnMB > 0 || cfg.Memory.HeapHardMB > 0 {
		wd := watchdog.New(watchdog.Config{WarnMB: cfg.Memory.HeapWarnMB, HardMB: cfg.Memory.HeapHardMB}, cancel)
		go wd.Run(runCtx)
		slog.Info("acp mode: heap watchdog activated", "bot", cfg.Bot.Name,
			"warn_mb", cfg.Memory.HeapWarnMB, "hard_mb", cfg.Memory.HeapHardMB)
	} else {
		slog.Info("acp mode: heap watchdog not activated (heap_warn_mb/heap_hard_mb not set)", "bot", cfg.Bot.Name)
	}

	conn := sdk.NewAgentSideConnection(agent, os.Stdout, os.Stdin)
	conn.SetLogger(slog.Default())
	agent.SetConnection(conn)

	select {
	case <-conn.Done():
	case <-runCtx.Done():
	}
	return nil
}

// buildACPAgent constructs the single-persona *acpinfra.Agent for
// configPath, reusing the exact construction primitives native daemon mode
// uses (TeamManager.startBot) -- provider factory, BM25 embedder, local
// filesystem memory/vector stores, and the local filesystem MCP client --
// without going through team.yaml or TeamManager at all, since ACP mode
// has no multi-bot orchestration to coordinate (architecture.md).
func buildACPAgent(configPath string) (*acpinfra.Agent, config.Config, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, config.Config{}, fmt.Errorf("load config %q: %w", configPath, err)
	}

	// SOUL.md lives alongside this persona's own config.yaml -- e.g.
	// boabot-team/bots/tech-lead/{config.yaml,SOUL.md} -- so no botsDir/
	// team.yaml lookup is needed, unlike native mode's per-team-member path.
	soulPath := filepath.Join(filepath.Dir(configPath), "SOUL.md")
	soulBytes, err := os.ReadFile(soulPath)
	if err != nil {
		return nil, config.Config{}, fmt.Errorf("read SOUL.md for %q: %w", cfg.Bot.Name, err)
	}

	memRoot := cfg.Memory.Path
	if memRoot == "" {
		exe, _ := os.Executable()
		memRoot = filepath.Join(filepath.Dir(exe), "memory")
	}
	memPath := filepath.Join(memRoot, cfg.Bot.Name)

	// FR-501 (spec.md): memPath is the directory native mode's TeamManager
	// may also be writing board.json/chat.json/tasks.json to, if this
	// persona's name matches the team's orchestrator entry and cfg.Memory.Path
	// was set to native mode's shared memory root. There is no channel for
	// this process to compare configuration with native mode directly (it
	// may not even be running) -- EnsureOwner instead validates what is
	// checkable purely from the directory itself: whether it was already
	// claimed by a different identity (e.g. a renamed persona reusing an old
	// directory). A mismatch degrades gracefully (NFR-Reliability): logged,
	// does not block startup.
	if matched, ownerErr := sharedstate.EnsureOwner(memPath, cfg.Bot.Name); ownerErr != nil {
		slog.Warn("acp mode: shared-state owner check failed", "bot", cfg.Bot.Name, "path", memPath, "err", ownerErr)
	} else if !matched {
		slog.Warn("acp mode: shared-state directory already claimed by a different identity; state may not be shared as expected",
			"bot", cfg.Bot.Name, "path", memPath)
	}

	memStore, err := fs.New(memPath)
	if err != nil {
		return nil, config.Config{}, fmt.Errorf("create memory store for %q: %w", cfg.Bot.Name, err)
	}
	vecStore, err := vector.New(memPath)
	if err != nil {
		return nil, config.Config{}, fmt.Errorf("create vector store for %q: %w", cfg.Bot.Name, err)
	}

	pf := team.NewLocalProviderFactory(cfg.Models.Providers)
	provider, err := pf.Get(cfg.Models.Default)
	if err != nil {
		return nil, config.Config{}, fmt.Errorf("get provider %q for %q: %w", cfg.Models.Default, cfg.Bot.Name, err)
	}

	embedder := domain.Embedder(bm25.DefaultEmbedder())

	// FR-503 (chat), FR-504/504a (tasks/scheduling): same tech-lead gate
	// buildACPMCPOptions uses for the board store (its own doc comment) --
	// board/chat/task/scheduling activate together, not independently,
	// since FR-504a's per-turn recording needs all three plus the
	// ChatTaskManager. The board *instance* itself is constructed inside
	// buildACPWorker/buildACPMCPOptions (unchanged from the prior ACP-parity
	// feature, preserving buildACPMCPOptions' own direct unit tests) and
	// returned here so the Agent's own WithBoardStore option (below) shares
	// the identical instance the MCP tool surface uses -- two separate
	// instances over the same board.json wouldn't corrupt the file
	// (persist() merges by ID), but each would carry a stale in-memory view
	// of the other's writes, so a board read via the MCP tool surface could
	// miss turn.go's own automatic DirectTask/board-item recording.
	var chatStore domain.ChatStore
	var taskStore domain.DirectTaskStore
	var chatTaskManager *apporchestrator.ChatTaskManager
	if cfg.Bot.BotType != "tech-lead" {
		chatPath := filepath.Join(memPath, "chat.json")
		chatStore = orchestratorlocal.NewInMemoryChatStore(chatPath)

		tasksPath := filepath.Join(memPath, "tasks.json")
		ts := orchestratorlocal.NewInMemoryDirectTaskStore(tasksPath)
		taskStore = ts

		dispatcher := orchestratorlocal.NewLocalTaskDispatcher(ts, acpinfra.NoImmediateDispatchQueue{}, cfg.Bot.Name)
		chatTaskManager = apporchestrator.NewChatTaskManager(dispatcher)

		slog.Info("acp mode: chat store activated", "bot", cfg.Bot.Name, "path", chatPath)
		slog.Info("acp mode: direct task store activated", "bot", cfg.Bot.Name, "path", tasksPath)
		slog.Info("acp mode: scheduling detection activated", "bot", cfg.Bot.Name)
	} else {
		slog.Info("acp mode: chat/task store and scheduling detection not activated (persona type is tech-lead)", "bot", cfg.Bot.Name)
	}

	worker, board := buildACPWorker(cfg, memPath, string(soulBytes), pf, provider, memStore, embedder, vecStore)

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

	opts := []acpinfra.Option{
		acpinfra.WithBotName(cfg.Bot.Name),
	}
	if chatStore != nil {
		opts = append(opts, acpinfra.WithChatStore(chatStore))
	}
	if taskStore != nil && board != nil {
		opts = append(opts, acpinfra.WithDirectTaskStore(taskStore), acpinfra.WithBoardStore(board))
	}
	if chatTaskManager != nil {
		opts = append(opts, acpinfra.WithChatTaskManager(chatTaskManager))
	}
	if raw := os.Getenv("BOABOT_ACP_KEEPALIVE_INTERVAL"); raw != "" {
		d, parseErr := time.ParseDuration(raw)
		if parseErr != nil {
			return nil, config.Config{}, fmt.Errorf("invalid BOABOT_ACP_KEEPALIVE_INTERVAL %q: %w", raw, parseErr)
		}
		opts = append(opts, acpinfra.WithKeepAliveInterval(d))
	}

	return acpinfra.New(singleWorkerFactory{w: worker}, "", opts...), cfg, nil
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
) (*application.ExecuteTaskUseCase, domain.BoardStore) {
	mcpOpts, board := buildACPMCPOptions(cfg, memPath)
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

	return worker, board
}

// buildACPMCPOptions constructs the functional options for ACP mode's local
// filesystem MCP client -- board store (FR-402/403), plugin store (FR-404),
// and CLI runner/tools (FR-405) -- gated on the same granular config fields
// team_manager.go:1020-1036 gates on, not an umbrella enabled flag (see
// architecture.md's "do not reuse orchestrator.enabled" decision). This is
// scope-equivalent, not condition-identical: see each gate's own comment
// below for the two specific ways ACP mode's gating differs from native
// mode's (the board gate compares a different struct field, equivalent by
// convention; the plugin/CLI-tool gates read a different config scope).
// Every store construction here degrades gracefully on failure (NFR-Reliability):
// logged, ACP mode still starts and executes tasks without that tool
// surface. Startup logs state clearly whether each tool surface activated
// for this persona (NFR-Observability), mirroring how Buzz monitor
// activation is already logged in main.go.
// buildACPMCPOptions also returns the constructed domain.BoardStore (nil if
// the tech-lead gate skipped it), so buildACPAgent can share the identical
// instance for the Agent's own automatic per-turn recording (FR-504a) --
// see buildACPWorker's doc comment for why one shared instance matters.
func buildACPMCPOptions(cfg config.Config, memPath string) ([]func(*localmcp.Client), domain.BoardStore) {
	var opts []func(*localmcp.Client)
	var board domain.BoardStore

	// Board store (FR-402/403): equivalent to team_manager.go:1023's
	// persona-type gate by convention, not by construction -- team_manager.go
	// compares entry.Type (the team.yaml entry's own field, also the
	// <bots-dir>/<type>/ directory name that bot is loaded from);
	// cfg.Bot.BotType here is the loaded persona's own bot.type field from
	// its config.yaml, a different piece of data with no team.yaml entry to
	// read in ACP mode. The two coincide today only because every real
	// persona's own bot.type matches the directory name it's loaded from --
	// the same convention resolveACPConfigPath already relies on. Not an
	// enabled flag either way (tm.sharedBoard is always non-nil in native
	// mode). NewInMemoryBoardStore has no failure path of its own (a
	// missing/corrupt persist file is silently treated as an empty board),
	// so there is nothing to log-and-skip here beyond activation.
	if cfg.Bot.BotType != "tech-lead" {
		boardPath := filepath.Join(memPath, "board.json")
		b := orchestratorlocal.NewInMemoryBoardStore(boardPath)
		board = b
		opts = append(opts, localmcp.WithBoardStore(b))
		slog.Info("acp mode: board store activated", "bot", cfg.Bot.Name, "path", boardPath)
	} else {
		slog.Info("acp mode: board store not activated (persona type is tech-lead)", "bot", cfg.Bot.Name)
	}

	// Plugin store (FR-404): uses the same install-dir-presence gate and
	// relative-path resolution against the persona's own memPath as
	// team_manager.go:501-519, but at a different config scope: native
	// mode resolves Orchestrator.Plugins.InstallDir ONCE from the team's
	// orchestrator-entry persona's config.yaml and shares that single
	// result team-wide (tm.resolvedPluginStore/resolvedInstallDir); ACP
	// mode has no team.yaml/orchestrator-entry concept, so cfg here is
	// always the config of whichever persona this process is running as.
	// A non-orchestrator persona's own config.yaml must set
	// orchestrator.plugins.install_dir itself for this to activate --
	// see user-docs/ACP-Harness-Adoption-Config.md.
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
	// resolveBinary -- WithCLITools just passes the config through). Same
	// team-wide-vs-running-persona-own-config scope difference as the
	// plugin store above applies here too (cfg.Orchestrator.CLITools is
	// this persona's own field; native mode resolves it once from the
	// team's orchestrator entry and shares it team-wide).
	opts = append(opts, localmcp.WithCLIRunner(cliagent.New()), localmcp.WithCLITools(cfg.Orchestrator.CLITools))
	slog.Info("acp mode: cli runner activated", "bot", cfg.Bot.Name,
		"claude_code_enabled", cfg.Orchestrator.CLITools.ClaudeCode.Enabled,
		"codex_enabled", cfg.Orchestrator.CLITools.Codex.Enabled,
		"openai_codex_enabled", cfg.Orchestrator.CLITools.OpenAICodex.Enabled,
		"opencode_enabled", cfg.Orchestrator.CLITools.OpenCode.Enabled)

	return opts, board
}
