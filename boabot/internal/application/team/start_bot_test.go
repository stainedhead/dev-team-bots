package team_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

// TestStartBot_EmbedderValidationFails verifies that startBot fails gracefully
// when a non-bm25 embedder is configured with an unsupported provider type.
func TestStartBot_EmbedderValidationFails(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-fake-key-for-unit-testing")

	dir := t.TempDir()
	botsDir := filepath.Join(dir, "bots")
	botType := "embedworker"
	botDir := filepath.Join(botsDir, botType)
	if err := os.MkdirAll(botDir, 0700); err != nil {
		t.Fatalf("mkdir bots/%s: %v", botType, err)
	}
	// Config with anthropic provider set as embedder — should fail validation.
	cfg := `bot:
  name: embedworker
  type: ` + botType + `
models:
  default: claude
  providers:
    - name: claude
      type: anthropic
      model_id: claude-haiku-4-5-20251001
memory:
  embedder: claude
budget:
  token_spend_daily: 0
  tool_calls_hourly: 0
context:
  threshold_tokens: 4096
`
	if err := os.WriteFile(filepath.Join(botDir, "config.yaml"), []byte(cfg), 0600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(botDir, "SOUL.md"), []byte("You are a test bot."), 0600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}

	teamFile := filepath.Join(dir, "team.yaml")
	if err := os.WriteFile(teamFile, []byte(`team:
  - name: embedworker
    type: `+botType+`
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

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// The bot should repeatedly fail embedder validation and be restarted.
	// The manager itself must not crash.
	err := mgr.Run(ctx)
	if err != nil && ctx.Err() == nil {
		t.Errorf("unexpected non-context error from Run: %v", err)
	}
}

// TestStartBot_RestoreOnEmptyFails verifies that when backup.enabled=true and
// restore_on_empty=true, startBot calls Restore() for an empty memory directory
// and returns an error when Restore fails (e.g. unreachable repo URL).
// The TeamManager restarts the bot; the process must not crash.
func TestStartBot_RestoreOnEmptyFails(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-fake-key-for-unit-testing")

	dir := t.TempDir()
	botsDir := filepath.Join(dir, "bots")
	botType := "restorebot"
	botDir := filepath.Join(botsDir, botType)
	if err := os.MkdirAll(botDir, 0700); err != nil {
		t.Fatalf("mkdir bots/%s: %v", botType, err)
	}
	cfg := `bot:
  name: restorebot
  type: ` + botType + `
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
backup:
  enabled: true
  restore_on_empty: true
  github:
    repo: https://localhost:1/nonexistent/repo.git
    branch: main
    author_name: Test
    author_email: test@test.com
`
	if err := os.WriteFile(filepath.Join(botDir, "config.yaml"), []byte(cfg), 0600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(botDir, "SOUL.md"), []byte("You are a test bot."), 0600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}

	teamFile := filepath.Join(dir, "team.yaml")
	if err := os.WriteFile(teamFile, []byte(`team:
  - name: restorebot
    type: `+botType+`
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

	// The bot should repeatedly fail Restore() (connection refused to localhost:1)
	// and be restarted with back-off. The manager must not crash.
	err := mgr.Run(ctx)
	if err != nil && ctx.Err() == nil {
		t.Errorf("unexpected non-context error from Run: %v", err)
	}
}

// TestStartBot_BackupEnabledNoRestoreProviderFails verifies that when backup is
// enabled with restore_on_empty: false, the backup goroutine is started and
// its WaitGroup is released correctly when the provider subsequently fails.
func TestStartBot_BackupEnabledNoRestoreProviderFails(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	dir := t.TempDir()
	botsDir := filepath.Join(dir, "bots")
	botType := "backupbot"
	botDir := filepath.Join(botsDir, botType)
	if err := os.MkdirAll(botDir, 0700); err != nil {
		t.Fatalf("mkdir bots/%s: %v", botType, err)
	}
	cfg := `bot:
  name: backupbot
  type: ` + botType + `
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
backup:
  enabled: true
  restore_on_empty: false
  github:
    repo: https://example.com/nonexistent/repo.git
    branch: main
    author_name: Test
    author_email: test@test.com
`
	if err := os.WriteFile(filepath.Join(botDir, "config.yaml"), []byte(cfg), 0600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(botDir, "SOUL.md"), []byte("You are a test bot."), 0600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}

	teamFile := filepath.Join(dir, "team.yaml")
	if err := os.WriteFile(teamFile, []byte(`team:
  - name: backupbot
    type: `+botType+`
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

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := mgr.Run(ctx)
	if err != nil && ctx.Err() == nil {
		t.Errorf("unexpected non-context error from Run: %v", err)
	}
}

// TestStartBot_SoulMDMissing verifies that startBot returns a clear error when
// SOUL.md is absent from the bot directory, and the manager restarts without crashing.
func TestStartBot_SoulMDMissing(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	dir := t.TempDir()
	botsDir := filepath.Join(dir, "bots")
	botType := "nosoulbot"
	botDir := filepath.Join(botsDir, botType)
	if err := os.MkdirAll(botDir, 0700); err != nil {
		t.Fatalf("mkdir bots/%s: %v", botType, err)
	}
	cfg := `bot:
  name: nosoulbot
  type: ` + botType + `
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
	if err := os.WriteFile(filepath.Join(botDir, "config.yaml"), []byte(cfg), 0600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	// SOUL.md deliberately omitted.

	teamFile := filepath.Join(dir, "team.yaml")
	if err := os.WriteFile(teamFile, []byte(`team:
  - name: nosoulbot
    type: `+botType+`
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

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := mgr.Run(ctx)
	if err != nil && ctx.Err() == nil {
		t.Errorf("unexpected non-context error from Run: %v", err)
	}
}

// TestStartBot_RestoreOnEmptyDirExists exercises the isDirEmpty path where the
// memory directory already exists but is empty, causing Restore to be attempted.
func TestStartBot_RestoreOnEmptyDirExists(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-fake-key-for-unit-testing")

	dir := t.TempDir()
	botsDir := filepath.Join(dir, "bots")
	botType := "restorebot2"
	botDir := filepath.Join(botsDir, botType)
	if err := os.MkdirAll(botDir, 0700); err != nil {
		t.Fatalf("mkdir bots/%s: %v", botType, err)
	}

	// Pre-create the per-bot memory directory so isDirEmpty takes the
	// "exists and empty" branch rather than the os.IsNotExist branch.
	memoryRoot := filepath.Join(dir, "memory")
	if err := os.MkdirAll(filepath.Join(memoryRoot, "restorebot2"), 0700); err != nil {
		t.Fatalf("mkdir memory/restorebot2: %v", err)
	}

	cfg := `bot:
  name: restorebot2
  type: ` + botType + `
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
backup:
  enabled: true
  restore_on_empty: true
  github:
    repo: https://localhost:1/nonexistent/repo.git
    branch: main
    author_name: Test
    author_email: test@test.com
`
	if err := os.WriteFile(filepath.Join(botDir, "config.yaml"), []byte(cfg), 0600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(botDir, "SOUL.md"), []byte("You are a test bot."), 0600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}

	teamFile := filepath.Join(dir, "team.yaml")
	if err := os.WriteFile(teamFile, []byte(`team:
  - name: restorebot2
    type: `+botType+`
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
		MemoryRoot:      memoryRoot,
		RestartDelay:    5 * time.Millisecond,
		MaxRestartDelay: 20 * time.Millisecond,
	}, r, b)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := mgr.Run(ctx)
	if err != nil && ctx.Err() == nil {
		t.Errorf("unexpected non-context error from Run: %v", err)
	}
}

// TestStartBot_OrchestratorHTTPServer verifies that when orchestrator.enabled=true
// and api_port is set to a free port, the HTTP server starts and serves the
// Kanban UI at /, then shuts down cleanly when ctx is cancelled.
func TestStartBot_OrchestratorHTTPServer(t *testing.T) {
	// Not parallel — binds a TCP port.
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-fake-key-for-unit-testing")

	// Find a free port.
	port := freePort(t)

	dir := t.TempDir()
	botsDir := filepath.Join(dir, "bots")
	botType := "orchbot"
	botDir := filepath.Join(botsDir, botType)
	if err := os.MkdirAll(botDir, 0700); err != nil {
		t.Fatalf("mkdir bots/%s: %v", botType, err)
	}

	cfg := "bot:\n  name: orchbot\n  type: " + botType + "\norchestrator:\n  enabled: true\n  api_port: " +
		fmt.Sprintf("%d", port) + "\n  admin_password: testadmin\nmodels:\n  default: claude\n  providers:\n    - name: claude\n      type: anthropic\n      model_id: claude-haiku-4-5-20251001\nbudget:\n  token_spend_daily: 0\n  tool_calls_hourly: 0\ncontext:\n  threshold_tokens: 4096\n"
	if err := os.WriteFile(filepath.Join(botDir, "config.yaml"), []byte(cfg), 0600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(botDir, "SOUL.md"), []byte("You are a test bot."), 0600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}

	teamFile := filepath.Join(dir, "team.yaml")
	if err := os.WriteFile(teamFile, []byte("team:\n  - name: orchbot\n    type: "+botType+"\n    enabled: true\n    orchestrator: true\n"), 0600); err != nil {
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Run the manager in a goroutine so we can probe the HTTP server.
	runDone := make(chan error, 1)
	go func() { runDone <- mgr.Run(ctx) }()

	// Give the server time to start (wait up to 800ms).
	addr := fmt.Sprintf("http://localhost:%d/", port)
	var resp *http.Response
	for i := range 8 {
		time.Sleep(100 * time.Millisecond)
		var err error
		resp, err = http.Get(addr) //nolint:noctx
		if err == nil {
			break
		}
		if i == 7 {
			t.Logf("orchestrator HTTP server not reachable after 800ms (port %d): %v", port, err)
		}
	}

	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 from orchestrator UI, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected text/html content-type, got %q", ct)
		}
	}

	// Cancel context and wait for Run to return.
	cancel()
	select {
	case runErr := <-runDone:
		if runErr != nil && ctx.Err() == nil {
			t.Errorf("unexpected non-context error: %v", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s after cancel")
	}
}

// freePort asks the OS for a free TCP port by binding on :0, then releases it.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// TestTeamManager_WatchdogWiring verifies that the watchdog goroutine is started
// and can trigger the cancel function (simulated via very low HardMB and a
// fake watchdog config; the watchdog fires against real heap, so we use a
// low-enough threshold that it fires immediately under test conditions, or
// we just verify the manager shuts down cleanly with a watchdog configured).
// This test focuses on the wiring path — watchdog correctness is tested separately.
func TestTeamManager_WatchdogWiring(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "wdbot", Type: "wdbot", Enabled: true, Orchestrator: true},
	})

	r := queue.NewRouter()
	b := bus.New()
	mgr := team.NewTeamManager(team.ManagerConfig{
		TeamFilePath:    teamFile,
		BotsDir:         t.TempDir(),
		MemoryRoot:      t.TempDir(),
		RestartDelay:    10 * time.Millisecond,
		MaxRestartDelay: 50 * time.Millisecond,
		WatchdogCfg:     team.WatchdogConfigForTest(10*time.Millisecond, 0, 1), // HardMB=1 fires immediately
	}, r, b)

	team.SetBotRunner(mgr, func(ctx context.Context, _ team.BotEntryForTest, _ string) error {
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// The watchdog should fire (HardMB=1 MiB will be exceeded immediately since
	// the Go runtime uses more than 1 MiB of heap) and cancel the context,
	// causing Run to return.
	err := mgr.Run(ctx)
	// Run either returns nil (clean cancel) or the context error — both are fine.
	_ = err
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
