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
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/credentials"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bus"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/watchdog"
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
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "err", err)
		os.Exit(1)
	}

	if *diagSecrets {
		if err := runDiagSecrets(cfg, os.Stdout); err != nil {
			slog.Error("secret diagnostics failed", "err", err)
			os.Exit(1)
		}
		return
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

	return mgr.Run(ctx)
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
