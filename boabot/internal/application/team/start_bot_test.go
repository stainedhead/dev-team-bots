package team_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/team"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bus"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
)

// writeMinimalBotFiles creates a minimal <botsDir>/<botType>/config.yaml and SOUL.md
// so startBot can reach the provider-creation step.
func writeMinimalBotFiles(t *testing.T, botsDir, botType string) {
	t.Helper()
	dir := filepath.Join(botsDir, botType)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir bots/%s: %v", botType, err)
	}
	// Minimal config.yaml — no aws section; anthropic provider with no key set.
	cfg := `bot:
  name: testbot
  type: ` + botType + `
orchestrator:
  enabled: false
models:
  default: claude
  providers:
    - name: claude
      type: anthropic
      model_id: claude-haiku-4-5-20251001
budget:
  token_spend_daily: 0
  tool_calls_hourly: 0
context:
  threshold_tokens: 4096
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("You are a test bot."), 0600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}
}

// TestStartBot_ProviderError verifies that startBot fails gracefully when the
// configured model provider cannot be constructed (no ANTHROPIC_API_KEY).
// The TeamManager treats this as a crash and restarts the bot.
func TestStartBot_ProviderError(t *testing.T) {
	// Cannot run parallel because we clear ANTHROPIC_API_KEY.
	t.Setenv("ANTHROPIC_API_KEY", "")

	dir := t.TempDir()
	botsDir := filepath.Join(dir, "bots")
	writeMinimalBotFiles(t, botsDir, "worker")

	teamFile := filepath.Join(dir, "team.yaml")
	if err := os.WriteFile(teamFile, []byte(`team:
  - name: worker
    type: worker
    enabled: true
    orchestrator: true
`), 0600); err != nil {
		t.Fatalf("write team.yaml: %v", err)
	}

	r := queue.NewRouter()
	b := bus.New()
	mgr := team.NewTeamManager(team.ManagerConfig{
		TeamFilePath:    teamFile,
		BotsDir:         botsDir,
		MemoryRoot:      filepath.Join(dir, "memory"),
		RestartDelay:    5 * time.Millisecond,
		MaxRestartDelay: 20 * time.Millisecond,
	}, r, b)

	// Run for a short time — the bot will repeatedly fail to get a provider and
	// be restarted, but the process must not crash.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := mgr.Run(ctx)
	// Run returns nil when ctx is cancelled cleanly; a non-context error would
	// indicate the manager itself failed unexpectedly.
	if err != nil && ctx.Err() == nil {
		t.Errorf("unexpected non-context error from Run: %v", err)
	}
}

// TestStartBot_SuccessPath exercises startBot through to RunAgentUseCase.Run
// by providing a fake (non-empty) ANTHROPIC_API_KEY.  The SDK accepts any
// non-empty key at construction time; actual API calls are never made because
// the context is cancelled before any task arrives.
func TestStartBot_SuccessPath(t *testing.T) {
	// t.Setenv requires no t.Parallel().
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-fake-key-for-unit-testing")

	dir := t.TempDir()
	botsDir := filepath.Join(dir, "bots")
	writeMinimalBotFiles(t, botsDir, "worker")

	teamFile := filepath.Join(dir, "team.yaml")
	if err := os.WriteFile(teamFile, []byte(`team:
  - name: worker
    type: worker
    enabled: true
    orchestrator: true
`), 0600); err != nil {
		t.Fatalf("write team.yaml: %v", err)
	}

	r := queue.NewRouter()
	b := bus.New()
	mgr := team.NewTeamManager(team.ManagerConfig{
		TeamFilePath:    teamFile,
		BotsDir:         botsDir,
		MemoryRoot:      filepath.Join(dir, "memory"),
		RestartDelay:    5 * time.Millisecond,
		MaxRestartDelay: 20 * time.Millisecond,
	}, r, b)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := mgr.Run(ctx)
	// Clean context cancellation: Run should return nil.
	if err != nil && ctx.Err() == nil {
		t.Errorf("unexpected non-context error: %v", err)
	}
}

// TestTeamManager_ShutdownAlreadyCancelledCtx verifies that Shutdown completes
// without hanging when called with an already-cancelled context.
func TestTeamManager_ShutdownAlreadyCancelledCtx(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "bot", Type: "bot", Enabled: true, Orchestrator: true},
	})

	mgr, _, _ := newTestManager(t, teamFile)
	team.SetBotRunner(mgr, func(ctx context.Context, _ team.BotEntryForTest, _ string) error {
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- mgr.Run(ctx) }()

	// Let the bot start.
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Shutdown with an already-cancelled context — should complete via the
	// 30-second fallback timeout (internally) and return promptly since the bot
	// exits on ctx cancellation.
	alreadyCancelled, cancelAlready := context.WithCancel(context.Background())
	cancelAlready()
	if err := mgr.Shutdown(alreadyCancelled); err != nil {
		// A timeout error is acceptable here if goroutines haven't exited yet;
		// the key check is that Shutdown returns at all (doesn't hang forever).
		t.Logf("Shutdown returned (possibly timeout): %v", err)
	}

	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s")
	}
}
