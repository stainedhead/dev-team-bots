package team

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application"
	appbackup "github.com/stainedhead/dev-team-bots/boabot/internal/application/backup"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	githubbackup "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/github/backup"
	httpserver "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/http"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bm25"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bus"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/fs"
	orchestratorlocal "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/vector"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/watchdog"
	openaiembedder "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/openai"
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
	// WatchdogCfg configures the heap memory watchdog. Zero means disabled.
	WatchdogCfg watchdog.Config
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

	// teamEntries holds all enabled bot entries, set by Run before any bots start.
	// Used by startBot to pre-register the full team in the orchestrator control plane.
	teamEntries []BotEntry

	// sharedChatStore, sharedTaskStore and sharedBoard are created once in Run
	// and used by all bots so that any bot's reply surfaces in the orchestrator
	// chat interface and board state is kept consistent.
	sharedChatStore domain.ChatStore
	sharedTaskStore domain.DirectTaskStore
	sharedBoard     domain.BoardStore

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
	// any bot tries to send to another. Also snapshot the list so startBot can
	// seed the orchestrator control plane with every team member.
	for _, e := range teamCfg.Team {
		if !e.Enabled {
			continue
		}
		tm.router.Register(e.Name, 0)
		tm.teamEntries = append(tm.teamEntries, e)
	}

	// Create shared stores. The orchestrator HTTP server uses these; all bots
	// register a result handler against them so any bot's reply appears in chat.
	// Persist the chat store under the orchestrator's memory directory.
	orchestratorMemPath := filepath.Join(tm.cfg.MemoryRoot, orchestratorName)
	_ = os.MkdirAll(orchestratorMemPath, 0o755)
	tm.sharedChatStore = orchestratorlocal.NewInMemoryChatStore(filepath.Join(orchestratorMemPath, "chat.json"))
	tm.sharedTaskStore = orchestratorlocal.NewInMemoryDirectTaskStore(filepath.Join(orchestratorMemPath, "tasks.json"))
	tm.sharedBoard = orchestratorlocal.NewInMemoryBoardStore(filepath.Join(orchestratorMemPath, "board.json"))

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

	// Start the heap watchdog if either threshold is configured.
	if tm.cfg.WatchdogCfg.WarnMB > 0 || tm.cfg.WatchdogCfg.HardMB > 0 {
		wd := watchdog.New(tm.cfg.WatchdogCfg, cancel)
		tm.wg.Add(1)
		go func() {
			defer tm.wg.Done()
			wd.Run(runCtx)
		}()
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

// validateEmbedderProvider checks that the provider named in botCfg.Memory.Embedder
// exists in botCfg.Models.Providers and that its type supports embeddings.
// Currently only "openai" supports the /v1/embeddings endpoint.
func validateEmbedderProvider(botCfg config.Config) error {
	embedderName := botCfg.Memory.Embedder
	for _, pc := range botCfg.Models.Providers {
		if pc.Name == embedderName {
			if pc.Type != "openai" {
				return fmt.Errorf(
					"provider %q (type %q) does not support embeddings; only openai does",
					pc.Name, pc.Type,
				)
			}
			return nil
		}
	}
	return fmt.Errorf("embedder provider %q not found in providers list", embedderName)
}

// startBot wires adapters and runs the RunAgentUseCase for a single bot.
func (tm *TeamManager) startBot(ctx context.Context, entry BotEntry, orchestratorName string) error {
	// Load per-bot config from <botsDir>/<type>/config.yaml.
	botCfgPath := filepath.Join(tm.cfg.BotsDir, entry.Type, "config.yaml")
	botCfg, err := config.Load(botCfgPath)
	if err != nil {
		return fmt.Errorf("load bot config for %q: %w", entry.Name, err)
	}

	// Validate embedder provider if a non-bm25 embedder is configured.
	if botCfg.Memory.Embedder != "" && botCfg.Memory.Embedder != "bm25" {
		if err := validateEmbedderProvider(botCfg); err != nil {
			return fmt.Errorf("embedder validation for %q: %w", entry.Name, err)
		}
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

	// Wire optional GitHub memory backup. Restore runs before adapters initialise
	// so that restored files (including budget.json) are visible on first load.
	var backupUC *appbackup.ScheduledBackupUseCase
	if botCfg.Backup.Enabled {
		token := os.Getenv("BOABOT_BACKUP_TOKEN")
		gb, err := githubbackup.New(githubbackup.Config{
			RepoURL:     botCfg.Backup.GitHub.Repo,
			Branch:      botCfg.Backup.GitHub.Branch,
			AuthorName:  botCfg.Backup.GitHub.AuthorName,
			AuthorEmail: botCfg.Backup.GitHub.AuthorEmail,
			MemoryPath:  memPath,
			Token:       token,
		})
		if err != nil {
			return fmt.Errorf("create github backup for %q: %w", entry.Name, err)
		}
		if botCfg.Backup.RestoreOnEmpty {
			empty, err := isDirEmpty(memPath)
			if err != nil {
				return fmt.Errorf("check memory dir for %q: %w", entry.Name, err)
			}
			if empty {
				if err := gb.Restore(ctx); err != nil {
					return fmt.Errorf("restore on empty for %q: %w", entry.Name, err)
				}
			}
		}
		backupUC = appbackup.New(gb, botCfg.Backup.Schedule)
	}

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

	// Run the scheduled backup loop if configured.
	if backupUC != nil {
		backupCtx, backupCancel := context.WithCancel(ctx)
		var backupDone sync.WaitGroup
		backupDone.Add(1)
		defer func() {
			backupCancel()
			backupDone.Wait()
		}()
		go func() {
			defer backupDone.Done()
			_ = backupUC.Run(backupCtx)
		}()
	}

	// Orchestrator-specific stores, populated only when orchestrator mode is active.
	var orchTaskStore domain.DirectTaskStore
	var orchChatStore domain.ChatStore

	// Wire orchestrator HTTP server before provider validation so the dashboard
	// is reachable even when ANTHROPIC_API_KEY is not yet configured.
	if botCfg.Orchestrator.Enabled && botCfg.Orchestrator.APIPort > 0 {
		adminPassword := botCfg.Orchestrator.AdminPassword
		if adminPassword == "" {
			adminPassword = "admin"
		}
		oAuth, oAuthErr := orchestratorlocal.NewInMemoryAuthProvider(adminPassword, botCfg.Orchestrator.JWTSecret, filepath.Join(memPath, "users.json"))
		if oAuthErr != nil {
			return fmt.Errorf("create orchestrator auth for %q: %w", entry.Name, oAuthErr)
		}
		board := tm.sharedBoard
		cp := orchestratorlocal.NewInMemoryControlPlane()

		// Pre-register every enabled team member so the dashboard shows the
		// full roster immediately, even before individual bots send heartbeats.
		for _, te := range tm.teamEntries {
			if regErr := cp.Register(ctx, domain.BotEntry{
				Name:    te.Name,
				BotType: te.Type,
			}); regErr != nil {
				return fmt.Errorf("register bot with control plane for %q: %w", te.Name, regErr)
			}
		}

		// Wire direct-task store and dispatcher. The orchestrator's own queue
		// can route to any bot because queueURL == bot name in local mode.
		orchTaskStore = tm.sharedTaskStore
		routerQueue := tm.router.QueueFor(entry.Name)
		dispatcher := orchestratorlocal.NewLocalTaskDispatcher(tm.sharedTaskStore, routerQueue, entry.Name)

		orchChatStore = tm.sharedChatStore

		// Wire LocalSkillRegistry; fall back to noop if the directory cannot be created.
		var skillReg domain.SkillRegistry = orchestratorlocal.NoopSkillRegistry{}
		if sr, srErr := orchestratorlocal.NewLocalSkillRegistry(filepath.Join(memPath, "skills")); srErr == nil {
			skillReg = sr
		} else {
			slog.Warn("skill registry unavailable; using noop", "bot", entry.Name, "err", srErr)
		}

		taskLogBase := filepath.Join(memPath, "task-logs")

		srv := httpserver.New(httpserver.Config{
			Auth:            oAuth,
			Board:           board,
			Team:            cp,
			Users:           oAuth,
			Skills:          skillReg,
			DLQ:             orchestratorlocal.NoopDLQStore{},
			Tasks:           orchTaskStore,
			Dispatcher:      dispatcher,
			Chat:            orchChatStore,
			AllowedWorkDirs: botCfg.Orchestrator.WorkDirs,
			TaskLogBase:     taskLogBase,
		})

		httpSrv := &http.Server{
			Addr:    fmt.Sprintf(":%d", botCfg.Orchestrator.APIPort),
			Handler: srv.Handler(),
		}

		httpCtx, httpCancel := context.WithCancel(ctx)
		var httpDone sync.WaitGroup
		httpDone.Add(1)
		defer func() {
			httpCancel()
			httpDone.Wait()
		}()
		go func() {
			defer httpDone.Done()
			if listenErr := httpSrv.ListenAndServe(); listenErr != nil && listenErr != http.ErrServerClosed {
				slog.Error("orchestrator HTTP server error", "bot", entry.Name, "err", listenErr)
			}
		}()
		go func() {
			<-httpCtx.Done()
			if shutdownErr := httpSrv.Shutdown(context.Background()); shutdownErr != nil {
				slog.Warn("orchestrator HTTP server shutdown error", "bot", entry.Name, "err", shutdownErr)
			}
		}()

		slog.Info("orchestrator dashboard started", "url", fmt.Sprintf("http://localhost:%d/", botCfg.Orchestrator.APIPort))
	}

	// Wire domain.ModelProvider.
	pf := newLocalProviderFactory(botCfg.Models.Providers)
	providerName := botCfg.Models.Default
	provider, err := pf.Get(providerName)
	if err != nil {
		if botCfg.Orchestrator.Enabled {
			// Dashboard is up; bot loop inactive until a provider is configured.
			slog.Warn("model provider unavailable; dashboard running but bot loop is inactive",
				"bot", entry.Name, "provider", providerName, "err", err)
			<-ctx.Done()
			return nil
		}
		return fmt.Errorf("get provider %q for %q: %w", providerName, entry.Name, err)
	}

	// Wire domain.Embedder — defaults to BM25; switches to the named provider
	// if memory.embedder is set and the provider exposes an OpenAI-compatible
	// embeddings endpoint.
	embedder := domain.Embedder(bm25.DefaultEmbedder())
	if name := botCfg.Memory.Embedder; name != "" && name != "bm25" {
		var found bool
		for _, pc := range botCfg.Models.Providers {
			if pc.Name != name {
				continue
			}
			found = true
			e, embedErr := openaiembedder.NewEmbedder(pc.Endpoint, pc.ModelID)
			if embedErr != nil {
				slog.Warn("could not build OpenAI embedder; falling back to BM25",
					"bot", entry.Name, "embedder", name, "err", embedErr)
			} else {
				embedder = e
				slog.Info("using OpenAI-compatible embedder", "bot", entry.Name,
					"embedder", name, "endpoint", pc.Endpoint, "model", pc.ModelID)
			}
			break
		}
		if !found {
			slog.Warn("embedder provider not found in providers list; falling back to BM25",
				"bot", entry.Name, "embedder", name)
		}
	}

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
	// Wire chat provider if configured and different from the default.
	if chatName := botCfg.Models.ChatProvider; chatName != "" && chatName != providerName {
		if chatProvider, chatErr := pf.Get(chatName); chatErr != nil {
			slog.Warn("chat provider unavailable; falling back to default for chat tasks",
				"bot", entry.Name, "chat_provider", chatName, "err", chatErr)
		} else {
			worker.WithChatProvider(chatProvider)
			slog.Info("chat provider wired", "bot", entry.Name, "chat_provider", chatName)
		}
	}

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

	// Register result handler on every bot so any bot's reply surfaces in chat.
	sharedChat := tm.sharedChatStore
	sharedTasks := tm.sharedTaskStore
	if sharedChat != nil && sharedTasks != nil {
		uc.WithTaskResultHandler(func(handlerCtx context.Context, p domain.TaskResultPayload) {
			if _, err := sharedTasks.Get(handlerCtx, p.TaskID); err != nil {
				return // not a tracked chat task
			}
			msg := domain.ChatMessage{
				Direction: domain.ChatDirectionInbound,
				Content:   p.Output,
				TaskID:    p.TaskID,
			}
			if task, getErr := sharedTasks.Get(handlerCtx, p.TaskID); getErr == nil {
				msg.BotName = task.BotName
				msg.ThreadID = task.ThreadID
			}
			if appendErr := sharedChat.Append(handlerCtx, msg); appendErr != nil {
				slog.Warn("failed to append inbound chat message", "task_id", p.TaskID, "err", appendErr)
			}

			// Mark the task as completed.
			if task, getErr := sharedTasks.Get(handlerCtx, p.TaskID); getErr == nil {
				now := time.Now().UTC()
				task.Status = domain.DirectTaskStatusCompleted
				task.CompletedAt = &now
				task.Output = p.Output
				_, _ = sharedTasks.Update(handlerCtx, task)

				// If this was a board-triggered task, update the board item status.
				if task.Source == domain.DirectTaskSourceBoard && tm.sharedBoard != nil {
					items, listErr := tm.sharedBoard.List(handlerCtx, domain.WorkItemFilter{ActiveTaskID: p.TaskID})
					if listErr == nil && len(items) > 0 {
						item := items[0]
						item.LastResult = p.Output
						item.LastResultAt = &now
						item.ActiveTaskID = ""
						if p.Success {
							item.Status = domain.WorkItemStatusDone
						} else {
							item.Status = domain.WorkItemStatusBlocked
						}
						_, _ = tm.sharedBoard.Update(handlerCtx, item)
					}
				}
			}
		})
	}

	return uc.Run(ctx)
}

// --- helpers -----------------------------------------------------------------

// isDirEmpty returns true if path does not exist or is an empty directory.
func isDirEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return len(entries) == 0, nil
}

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
