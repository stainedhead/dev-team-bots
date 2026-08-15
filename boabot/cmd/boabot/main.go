package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	apporchestrator "github.com/stainedhead/dev-team-bots/boabot/internal/application/orchestrator"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/screening"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/team"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	buzzinfra "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/buzz"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/credentials"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bus"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/watchdog"
	infraScreening "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/screening"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret"
	secretenv "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret/env"
	secretfile "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret/file"
	secretkeystore "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret/keystore"
	secretsystemd "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret/systemd"
	slackinfra "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/slack"
)

var version = "dev"

func main() {
	configPath := flag.String("config", defaultConfigPath(), "path to config file")
	diagSecrets := flag.Bool("diag-secrets", false, "report which provider resolves each configured secret (name only, never the value), then exit")
	acpMode := flag.Bool("acp", false, "serve a single persona (configPath) as an Agent Client Protocol agent over stdio, for registration as a buzz-acp custom harness, instead of running the normal multi-bot daemon")
	agentName := flag.String("agent", "orchestrator", "-acp mode only: bot persona name under -bots-dir to load, e.g. \"tech-lead\" resolves <bots-dir>/tech-lead/config.yaml (ignored if -config is passed explicitly)")
	botsDir := flag.String("bots-dir", defaultBotsDir(), "-acp mode only: directory containing <agent>/config.yaml persona subdirectories, used to resolve -agent")
	flag.Parse()

	configExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			configExplicit = true
		}
	})

	if *diagSecrets {
		cfg, err := config.Load(*configPath)
		if err != nil {
			slog.Error("failed to load config", "path", *configPath, "err", err)
			os.Exit(1)
		}
		if err := runDiagSecrets(cfg, os.Stdout); err != nil {
			slog.Error("secret diagnostics failed", "err", err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if *acpMode {
		// ACP mode loads a single persona's config (not team.yaml) and
		// never touches TeamManager/ChannelMonitor -- see cmd/boabot/acp.go
		// and architecture.md. resolveACPConfigPath lets an explicit
		// -config win outright, or resolves -agent under -bots-dir.
		resolvedConfigPath := resolveACPConfigPath(configExplicit, *configPath, *botsDir, *agentName)
		slog.Info("starting boabot acp mode", "config", resolvedConfigPath, "agent", *agentName, "version", version)
		if err := runACP(ctx, resolvedConfigPath); err != nil {
			slog.Error("acp agent exited with error", "err", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "err", err)
		os.Exit(1)
	}

	slog.Info("starting boabot", "name", cfg.Bot.Name, "type", cfg.Bot.BotType, "version", version)

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
	providers, err := buildSecretProviders()
	if err != nil {
		return err
	}
	store := secret.New(providers)

	// Resolve ANTHROPIC_API_KEY and BOABOT_BACKUP_TOKEN through the
	// SecretStore chain rather than the credentials file directly (FR-046).
	resolveEnvCredentials(ctx, store)

	// Resolve Slack bot_token/app_token from the store when not inlined in
	// config.yaml (FR-047); inline fields still work but log a deprecation
	// warning (FR-048, warn-only).
	cfg.Slack.ResolveSecrets(ctx, store, slog.Default())

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
		mgr.WithChannelMonitor(slackMon)
		slog.Info("slack socket mode monitor configured", "bot", cfg.Slack.BotName)
	}

	// Wire Buzz (Nostr relay) monitors: one per Buzz-enabled team.yaml
	// persona (FR-001/FR-003), not the single process-wide cfg.Buzz block
	// this used to be. The actual per-persona loop lives in
	// TeamManager.Run() (not here) because the Dispatcher/BoardStore the
	// Buzz -> Dispatcher/DirectTaskStore/BoardStore bridge needs (P2.2)
	// don't exist until Run() creates its shared stores -- this closure is
	// only the infrastructure-construction step (buildBuzzMonitor), called
	// back by Run() at the right time with the dependencies only it has.
	// See specs/260814-boabot-native-daemon-mode/implementation-notes.md.
	mgr.WithBuzzMonitorBuilder(newBuzzMonitorBuilder(store, managerCfg.BotsDir, managerCfg.MemoryRoot))

	return mgr.Run(ctx)
}

// newBuzzMonitorBuilder returns the team.BuzzMonitorBuilder closure
// TeamManager.Run() invokes once per Buzz-enabled persona (see the wiring
// comment at its call site in run()).
//
// The nil check here is load-bearing, not defensive boilerplate: this
// closure's declared return type is domain.ChannelMonitor (an interface),
// but buildBuzzMonitor returns *buzzinfra.Monitor (a concrete pointer). A
// bare `return buildBuzzMonitor(...)` converts a nil *buzzinfra.Monitor
// (buildBuzzMonitor's documented fail-closed result on a bad/missing key)
// into a *non-nil* domain.ChannelMonitor interface value -- Go's classic
// typed-nil-interface pitfall. TeamManager.Run()'s own `if mon == nil`
// check can never catch that, so the broken monitor was being registered
// anyway and every bot in the team (they share one monitor list) crashed
// with a nil-pointer panic the first time anything called .Start() on it.
// Comparing the concrete *buzzinfra.Monitor to nil first, and returning a
// literal nil for the interface, is the standard fix.
func newBuzzMonitorBuilder(store domain.SecretStore, botsDir, memoryRoot string) team.BuzzMonitorBuilder {
	return func(
		buildCtx context.Context,
		entry team.BotEntry,
		botCfg config.Config,
		r *queue.Router,
		dispatcher domain.ScheduledTaskDispatcher,
		board domain.BoardStore,
		chatStore domain.ChatStore,
		shutdownFn func(context.Context) error,
	) domain.ChannelMonitor {
		chatMgr := apporchestrator.NewChatTaskManager(dispatcher)
		// P1.5: wire chatStore into the bridge for FR-206's ChatStore
		// history-replay conversation continuation (thread replies and DMs
		// alike). Falls back to plain NewBuzzTaskBridge (no history replay)
		// when chatStore is nil -- e.g. an orchestrator-mode-disabled
		// process that never creates tm.sharedChatStore.
		var bridge *apporchestrator.BuzzTaskBridge
		if chatStore != nil {
			bridge = apporchestrator.NewBuzzTaskBridgeWithChatStore(dispatcher, board, chatMgr, chatStore)
		} else {
			bridge = apporchestrator.NewBuzzTaskBridge(dispatcher, board, chatMgr)
		}
		// A persona's own bot_name may collide with Slack's configured bot
		// name (an operator pointing both channels at the same bot);
		// buildBuzzMonitor's router.Lookup-before-Register handles that
		// (and the Run()-side team-entry pre-registration) uniformly now,
		// so no queueAlreadyRegistered flag is needed here.
		mon := buildBuzzMonitor(buildCtx, botCfg, store, r, botsDir, memoryRoot, shutdownFn, bridge, chatStore)
		if mon == nil {
			return nil
		}
		return mon
	}
}

// buildBuzzMonitor constructs the Buzz (Nostr) domain.ChannelMonitor from
// cfg.Buzz when buzz.enabled is true and every required setting resolves.
// Its first statement is the FR-036 activation guard: with Buzz disabled,
// nothing below it runs, so no SecretStore lookup, no RelayClient, and no
// relay connection is ever attempted. On any resolution failure (missing
// bot_name/relay_url, or LoadKeypair failing closed per FR-003) it logs and
// returns nil -- mirroring the Slack wiring block's "activate only if
// everything's present" pattern above -- so Slack, every other bot, and the
// rest of the process start completely unaffected.
//
// Registration is idempotent via router.Lookup: whether the bot's queue was
// already registered by the Slack wiring block above (an operator pointing
// both channels at one bot), by an earlier persona sharing this bot name, or
// by TeamManager.Run()'s own team-entry pre-registration (the conventional
// case once a persona's own team.yaml entry name matches its buzz.bot_name),
// buildBuzzMonitor reuses the existing queue instead of calling
// router.Register again -- Register panics on a duplicate name, which would
// otherwise crash the whole process rather than just skipping Buzz for one
// persona.
//
// td, when non-nil, is wired as the Monitor's BuzzTaskDispatcher bridge
// (internal/application/orchestrator.BuzzTaskBridge in production) so
// dispatched tasks flow through Dispatcher/DirectTaskStore/BoardStore
// instead of the direct queue.Send fallback (see monitor.go's
// WithTaskDispatcher option doc comment).
//
// chatStore, when non-nil, is wired as the Monitor's own ChatStore
// reference (WithChatStore) so outbound (bot-authored) replies get recorded
// for FR-206 history replay too -- see monitor.go's recordOutbound.
//
// DM support (P2.1-P2.4) is activated unconditionally whenever this
// persona's own channel key (sk, already resolved above) is available --
// spec.md's Goals: "any Buzz-enabled persona with a provisioned key is
// reachable via a direct 1:1 message ... using its existing channel
// identity -- no separate DM key," so there is no separate DM-enable
// config flag to check.
func buildBuzzMonitor(ctx context.Context, cfg config.Config, store domain.SecretStore, router *queue.Router, botsDir, memoryRoot string, shutdownFn buzzinfra.ShutdownFunc, td domain.BuzzTaskDispatcher, chatStore domain.ChatStore) *buzzinfra.Monitor {
	bc := cfg.Buzz
	if !bc.Enabled {
		return nil
	}
	if bc.BotName == "" || bc.RelayURL == "" {
		slog.Error("buzz monitor: refusing to activate -- buzz.enabled is true but bot_name/relay_url is missing")
		return nil
	}

	sk, pk, err := buzzinfra.LoadKeypair(ctx, store, bc.BotName)
	if err != nil {
		slog.Error("buzz monitor: failed to load private key; Buzz will not start (all other channels and bots continue normally)", "bot", bc.BotName, "err", err)
		return nil
	}

	profile := buzzinfra.Profile{Name: bc.BotName}
	// Only publish another bot's type/description when Buzz's target bot is
	// the same bot this config.yaml's [bot] section actually describes --
	// cfg.Bot.BotType/botsDir's AGENTS.md is not necessarily buzz.bot_name's
	// own identity.
	if cfg.Bot.Name == bc.BotName {
		profile.BotType = cfg.Bot.BotType
		profile.Description = readBotDescription(botsDir, cfg.Bot.BotType)
	}

	opts := []buzzinfra.Option{
		buzzinfra.WithLogger(slog.Default()),
		buzzinfra.WithProfile(profile),
	}

	if token, found, tokErr := buzzinfra.LoadAPIToken(ctx, store, bc.BotName); tokErr != nil {
		slog.Warn("buzz monitor: failed to resolve API token; continuing without one", "bot", bc.BotName, "err", tokErr)
	} else if found {
		// required=false: BUZZ_REQUIRE_AUTH_TOKEN is a relay-side setting,
		// not something boabot's own config controls -- see FR-010. The
		// token, when resolved, is still attached on every dial.
		opts = append(opts, buzzinfra.WithAPIToken(token, false))
	}

	// FR-001: resolve an optional NIP-OA owner-attestation "auth" tag and
	// attach it to every AUTH event via WithAuthTagFunc, so this bot can
	// gain NIP-AA virtual channel membership without being explicitly
	// enrolled. Absent or invalid is not fatal to Buzz activation -- a bot
	// that only needs to act as an explicitly-enrolled member legitimately
	// has no tag configured, so this logs and continues, matching the
	// LoadAPIToken pattern immediately above.
	if authTagFn, found, tagErr := buzzinfra.LoadAuthTag(ctx, store, bc.BotName, pk.Hex()); tagErr != nil {
		slog.Warn("buzz monitor: failed to resolve/validate NIP-OA auth tag; Buzz will connect without owner attestation (virtual membership limited to explicit relay enrollment)", "bot", bc.BotName, "err", tagErr)
	} else if found {
		opts = append(opts, buzzinfra.WithAuthTagFunc(authTagFn))
	}

	rc := buzzinfra.NewRelayClient(bc.RelayURL, sk, opts...)

	q, alreadyRegistered := router.Lookup(bc.BotName)
	if !alreadyRegistered {
		q = router.Register(bc.BotName, 0)
	}

	// AcquireLock's O_CREATE|O_EXCL (via Monitor.Start) needs memoryRoot to
	// already exist. TeamManager normally brings it into existence as a
	// side effect of creating a bot's own per-bot memory subdirectory, but
	// that must not be relied on to happen before a monitor's own Start
	// runs -- and this MkdirAll must live here, not unconditionally in
	// run(), so the buzz.enabled: false path is untouched (FR-036: no
	// observable behaviour change when Buzz is disabled).
	if err := os.MkdirAll(memoryRoot, 0o700); err != nil {
		slog.Error("buzz monitor: failed to create memory root for the FR-031 singleton lock; Buzz will not start", "memory_root", memoryRoot, "err", err)
		return nil
	}

	monCfg := buzzinfra.Config{
		RelayURL:           bc.RelayURL,
		BotName:            bc.BotName,
		AgentPubKeyHex:     pk.Hex(),
		OwnerPubkeyHex:     bc.OwnerPubkey,
		RespondTo:          bc.RespondTo,
		RespondToAllowlist: bc.RespondToAllowlist,
		PresenceInterval:   time.Duration(bc.PresenceInterval),
		// LockDir MUST be the shared memory root, not a per-bot directory --
		// see buzzinfra.Config.LockDir's doc comment for why.
		LockDir: memoryRoot,
	}

	screener := screening.NewScreenContentUseCase(infraScreening.NewRegexScreener())

	monOpts := []buzzinfra.MonitorOption{
		buzzinfra.WithShutdownFunc(shutdownFn),
		buzzinfra.WithMonitorLogger(slog.Default()),
		// P2.1: DM support over this persona's own channel key -- no
		// separate DM key/secret (see this function's doc comment).
		buzzinfra.WithDMKeyer(buzzinfra.NewDMKeyer(sk)),
	}
	if td != nil {
		monOpts = append(monOpts, buzzinfra.WithTaskDispatcher(td))
	}
	if chatStore != nil {
		monOpts = append(monOpts, buzzinfra.WithChatStore(chatStore))
	}

	return buzzinfra.NewMonitor(rc, monCfg, q, screener, monOpts...)
}

// readBotDescription best-effort reads botsDir/<botType>/AGENTS.md and
// extracts the paragraph following its "## What I do" heading, per FR-011's
// "description from its AGENTS.md". Any read/parse failure, or the heading
// not being found, returns "" -- a missing profile description is never
// fatal to Buzz activation.
func readBotDescription(botsDir, botType string) string {
	if botsDir == "" || botType == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(botsDir, botType, "AGENTS.md"))
	if err != nil {
		return ""
	}
	const heading = "## What I do"
	content := string(b)
	idx := strings.Index(content, heading)
	if idx == -1 {
		return ""
	}
	rest := content[idx+len(heading):]
	if end := strings.Index(rest, "\n## "); end != -1 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}

// buildSecretProviders assembles the default four-provider chain (FR-040:
// env → systemd → keystore → file). The world-readable-credentials-file
// check (FR-043) is preserved exactly as it was before the SecretStore
// migration: it runs unconditionally (independent of which individual
// secrets end up being needed) and is fatal on failure, matching the
// pre-migration behaviour of the credentials.Load call that used to sit at
// the top of run(). If the credentials path itself cannot be determined,
// the file provider is simply omitted (matching the old code's behaviour of
// skipping the whole credentials-file block in that case) — env vars still
// resolve normally either way.
func buildSecretProviders() ([]domain.SecretProvider, error) {
	providers := []domain.SecretProvider{secretenv.New(), secretsystemd.New(), secretkeystore.New()}

	credsPath, err := credentials.DefaultPath()
	if err != nil {
		slog.Warn("could not determine credentials file path", "err", err)
		return providers, nil
	}
	if _, err := credentials.Load(credsPath); err != nil {
		return nil, fmt.Errorf("credentials: %w", err) // world-readable file → fatal
	}
	return append(providers, secretfile.New(credsPath)), nil
}

// resolveEnvCredentials applies ANTHROPIC_API_KEY and BOABOT_BACKUP_TOKEN
// from store into the process environment (FR-046), migrated from the
// former two applyCredential calls against the credentials file directly.
func resolveEnvCredentials(ctx context.Context, store domain.SecretStore) {
	applyCredentialFromStore(ctx, store, "anthropic_api_key", "ANTHROPIC_API_KEY")
	applyCredentialFromStore(ctx, store, "boabot_backup_token", "BOABOT_BACKUP_TOKEN")
}

// applyCredentialFromStore sets envKey from store's resolution of the named
// secret. Precedence (an explicitly-set env var always wins) is enforced by
// the SecretStore chain itself — its first provider is always "env" — so on
// a hit this is a no-op when envKey was already set to the same value it
// already held, and only actually changes anything when envKey was unset.
// A miss (no provider resolved the secret) is not an error: envKey is left
// exactly as it was, matching the pre-migration behaviour.
func applyCredentialFromStore(ctx context.Context, store domain.SecretStore, secretName, envKey string) {
	v, err := store.Get(ctx, domain.SecretRef{Name: secretName})
	if err != nil {
		return
	}
	os.Setenv(envKey, v) //nolint:errcheck
}
