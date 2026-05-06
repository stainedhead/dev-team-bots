package team

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bm25"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/budget"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bus"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/fs"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/vector"
)

// TeamConfig is the parsed team.yaml structure.
type TeamConfig struct {
	Team []BotEntry `yaml:"team"`
}

// BotEntry describes a single bot in the team roster.
type BotEntry struct {
	Name         string `yaml:"name"`
	Type         string `yaml:"type"`
	Enabled      bool   `yaml:"enabled"`
	Orchestrator bool   `yaml:"orchestrator"`
}

// ManagerConfig holds the configuration for a TeamManager.
type ManagerConfig struct {
	// TeamFilePath is the path to team.yaml.
	TeamFilePath string
	// BotsDir is the directory that contains bots/<type>/ subdirectories.
	BotsDir string
	// MemoryRoot is the base path for per-bot memory files. Defaults to ./memory.
	MemoryRoot string
	// RestartDelay is the initial back-off on panic restart. Defaults to 1s.
	RestartDelay time.Duration
	// MaxRestartDelay is the maximum back-off. Defaults to 5 minutes.
	MaxRestartDelay time.Duration
}

func (c *ManagerConfig) applyDefaults() {
	if c.RestartDelay <= 0 {
		c.RestartDelay = time.Second
	}
	if c.MaxRestartDelay <= 0 {
		c.MaxRestartDelay = 5 * time.Minute
	}
	if c.MemoryRoot == "" {
		c.MemoryRoot = "memory"
	}
}

// simpleWorkerFactory wraps a single pre-wired Worker so that WorkerFactory.New
// always returns the same instance.  This is fine for the local single-binary
// model because each bot has exactly one goroutine calling New at a time.
type simpleWorkerFactory struct {
	worker domain.Worker
}

func (f *simpleWorkerFactory) New() domain.Worker { return f.worker }

// TeamManager starts and manages all enabled bots in-process.
// Create one with NewTeamManager and call Run to start the team.
type TeamManager struct {
	cfg      ManagerConfig
	router   *queue.Router
	bus      *bus.Bus
	registry *BotRegistry

	// botRunner is called once per enabled bot. Defaults to (*TeamManager).startBot.
	// Replaced in tests via export_test.go to avoid real file I/O and network calls.
	botRunner func(ctx context.Context, entry BotEntry, orchestratorName string) error

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewTeamManager constructs a TeamManager.  Call Run to start the team.
func NewTeamManager(cfg ManagerConfig, router *queue.Router, bus *bus.Bus) *TeamManager {
	cfg.applyDefaults()
	tm := &TeamManager{
		cfg:      cfg,
		router:   router,
		bus:      bus,
		registry: NewBotRegistry(),
	}
	tm.botRunner = tm.startBot
	return tm
}

// Registry returns the BotRegistry so callers can inspect running bots.
func (tm *TeamManager) Registry() *BotRegistry { return tm.registry }

// Run reads team.yaml, starts all enabled bots, blocks until ctx is cancelled,
// then calls Shutdown.  It returns an error if the team file cannot be parsed
// or if no bots could be started.
func (tm *TeamManager) Run(ctx context.Context) error {
	teamCfg, err := loadTeamConfig(tm.cfg.TeamFilePath)
	if err != nil {
		return fmt.Errorf("team: load team config: %w", err)
	}

	// Identify the orchestrator bot — its name is used as the target for
	// registration messages in local mode.
	orchestratorName := ""
	for _, e := range teamCfg.Team {
		if e.Orchestrator && e.Enabled {
			orchestratorName = e.Name
			break
		}
	}
	// If there is no enabled orchestrator, fall back to the first enabled bot so
	// that bots can still attempt to register (even if nothing handles it).
	if orchestratorName == "" {
		for _, e := range teamCfg.Team {
			if e.Enabled {
				orchestratorName = e.Name
				break
			}
		}
	}

	// Pre-register all enabled bots with the Router so channels exist before
	// any bot tries to send to another.
	for _, e := range teamCfg.Team {
		if !e.Enabled {
			continue
		}
		tm.router.Register(e.Name, 0)
	}

	// Start each enabled bot in its own goroutine.
	started := 0
	runCtx, cancel := context.WithCancel(ctx)
	tm.cancel = cancel

	for _, e := range teamCfg.Team {
		if !e.Enabled {
			continue
		}
		entry := e // capture loop variable
		tm.wg.Add(1)
		go func() {
			defer tm.wg.Done()
			tm.runBotWithRestart(runCtx, entry, orchestratorName)
		}()
		started++
	}

	if started == 0 {
		cancel()
		return fmt.Errorf("team: no enabled bots found in %s", tm.cfg.TeamFilePath)
	}

	slog.Info("team started", "bots", started, "orchestrator", orchestratorName)

	<-runCtx.Done()
	return tm.Shutdown(context.Background())
}

// Shutdown sends a ShutdownMessage to all registered bots then waits for all
// goroutines to exit.  If the provided context is already cancelled it uses a
// 30-second timeout.
func (tm *TeamManager) Shutdown(ctx context.Context) error {
	if tm.cancel != nil {
		tm.cancel()
	}

	// Broadcast shutdown to all bots so their poll loops unblock.
	shutdownCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		shutdownCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}

	for _, id := range tm.registry.List() {
		msg := domain.Message{
			Type: domain.MessageTypeShutdown,
			From: "team-manager",
			To:   id.Name,
		}
		if err := tm.router.SendTo(shutdownCtx, id.Name, msg); err != nil {
			slog.Warn("shutdown message delivery failed", "bot", id.Name, "err", err)
		}
	}

	done := make(chan struct{})
	go func() {
		tm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-shutdownCtx.Done():
		return fmt.Errorf("team: shutdown timed out waiting for goroutines: %w", shutdownCtx.Err())
	}
}

// runBotWithRestart runs a bot goroutine, restarting it on panic with
// exponential back-off capped at MaxRestartDelay.  It exits cleanly when ctx
// is cancelled.
func (tm *TeamManager) runBotWithRestart(ctx context.Context, entry BotEntry, orchestratorName string) {
	delay := tm.cfg.RestartDelay
	for {
		if ctx.Err() != nil {
			return
		}
		crashed := tm.runBot(ctx, entry, orchestratorName, tm.botRunner)
		if !crashed {
			// Clean exit (ctx cancelled).
			return
		}
		// Exponential back-off.
		slog.Warn("bot crashed, restarting", "bot", entry.Name, "delay", delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
		delay = time.Duration(math.Min(float64(delay*2), float64(tm.cfg.MaxRestartDelay)))
	}
}

// runBot starts a single bot and returns true if it panicked, false if it
// exited normally (ctx cancelled or clean error).
func (tm *TeamManager) runBot(
	ctx context.Context,
	entry BotEntry,
	orchestratorName string,
	runner func(context.Context, BotEntry, string) error,
) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("bot panic recovered", "bot", entry.Name, "panic", r)
			panicked = true
		}
	}()

	if err := runner(ctx, entry, orchestratorName); err != nil {
		if ctx.Err() != nil {
			return false // context cancelled — not a crash
		}
		slog.Error("bot exited with error", "bot", entry.Name, "err", err)
		return true // treat non-context errors as crashes so the bot is restarted
	}
	return false
}

// startBot wires adapters and runs the RunAgentUseCase for a single bot.
func (tm *TeamManager) startBot(ctx context.Context, entry BotEntry, orchestratorName string) error {
	// Load per-bot config from <botsDir>/<type>/config.yaml.
	botCfgPath := filepath.Join(tm.cfg.BotsDir, entry.Type, "config.yaml")
	botCfg, err := config.Load(botCfgPath)
	if err != nil {
		return fmt.Errorf("load bot config for %q: %w", entry.Name, err)
	}

	// Read SOUL.md from <botsDir>/<type>/SOUL.md.
	soulPath := filepath.Join(tm.cfg.BotsDir, entry.Type, "SOUL.md")
	soulBytes, err := os.ReadFile(soulPath)
	if err != nil {
		return fmt.Errorf("read SOUL.md for %q: %w", entry.Name, err)
	}
	soulPrompt := string(soulBytes)

	// Per-bot memory directory.
	memPath := filepath.Join(tm.cfg.MemoryRoot, entry.Name)

	// Wire domain.MemoryStore.
	memStore, err := fs.New(memPath)
	if err != nil {
		return fmt.Errorf("create FS for %q: %w", entry.Name, err)
	}

	// Wire domain.VectorStore.
	vecStore, err := vector.New(memPath)
	if err != nil {
		return fmt.Errorf("create VectorStore for %q: %w", entry.Name, err)
	}

	// Wire domain.BudgetTracker.
	bt, err := budget.New(botCfg.Budget, memPath)
	if err != nil {
		return fmt.Errorf("create BudgetTracker for %q: %w", entry.Name, err)
	}
	// Run budget flusher in a goroutine for the lifetime of this bot.
	go func() { _ = bt.Run(ctx) }()

	// Wire domain.ModelProvider.
	pf := newLocalProviderFactory(botCfg.Models.Providers)
	providerName := botCfg.Models.Default
	provider, err := pf.Get(providerName)
	if err != nil {
		return fmt.Errorf("get provider %q for %q: %w", providerName, entry.Name, err)
	}

	// Wire domain.Embedder.
	embedder := bm25.DefaultEmbedder()

	// Wire no-op MCP client.
	mcpClient := &noopMCPClient{}

	// Construct the worker (ExecuteTaskUseCase).
	worker := application.NewExecuteTaskUseCase(
		provider,
		mcpClient,
		memStore,
		embedder,
		vecStore,
		soulPrompt,
	)
	worker.WithBudgetTracker(bt)

	workerFactory := &simpleWorkerFactory{worker: worker}

	// Build the BotIdentity.
	identity := domain.BotIdentity{
		Name:     entry.Name,
		BotType:  entry.Type,
		QueueURL: entry.Name, // in local mode, queue URL == bot name
	}
	tm.registry.Register(identity)

	// Get the bot's own queue (already registered by Run).
	q := tm.router.QueueFor(entry.Name)

	// Run the agent loop.
	uc := application.NewRunAgentUseCase(
		identity,
		q,
		tm.bus,
		workerFactory,
		nil, // no channel monitors for now
		orchestratorName,
	)
	return uc.Run(ctx)
}

// --- helpers -----------------------------------------------------------------

func loadTeamConfig(path string) (TeamConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TeamConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var tc TeamConfig
	if err := yaml.Unmarshal(data, &tc); err != nil {
		return TeamConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return tc, nil
}
