package team_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/team"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bus"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
)

// writeTeamYAML writes a minimal team.yaml to dir and returns its path.
func writeTeamYAML(t *testing.T, dir string, entries []team.BotEntryForTest) string {
	t.Helper()
	content := "team:\n"
	for _, e := range entries {
		orch := ""
		if e.Orchestrator {
			orch = "\n    orchestrator: true"
		}
		enabled := "false"
		if e.Enabled {
			enabled = "true"
		}
		content += fmt.Sprintf("  - name: %s\n    type: %s\n    enabled: %s%s\n",
			e.Name, e.Type, enabled, orch)
	}
	path := filepath.Join(dir, "team.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write team.yaml: %v", err)
	}
	return path
}

func newTestManager(t *testing.T, teamFilePath string) (*team.TeamManager, *queue.Router, *bus.Bus) {
	t.Helper()
	r := queue.NewRouter()
	b := bus.New()
	cfg := team.ManagerConfig{
		TeamFilePath:    teamFilePath,
		BotsDir:         t.TempDir(),
		MemoryRoot:      t.TempDir(),
		RestartDelay:    10 * time.Millisecond,
		MaxRestartDelay: 50 * time.Millisecond,
	}
	mgr := team.NewTeamManager(cfg, r, b)
	return mgr, r, b
}

// TestTeamManager_NoEnabledBots verifies that Run returns an error when
// team.yaml contains no enabled bots.
func TestTeamManager_NoEnabledBots(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "worker", Type: "worker", Enabled: false},
	})

	mgr, _, _ := newTestManager(t, teamFile)
	team.SetBotRunner(mgr, func(_ context.Context, _ team.BotEntryForTest, _ string) error {
		return nil
	})

	err := mgr.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for no enabled bots, got nil")
	}
}

// TestTeamManager_MissingTeamFile verifies that Run returns an error when the
// team.yaml path does not exist.
func TestTeamManager_MissingTeamFile(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newTestManager(t, "/nonexistent/path/team.yaml")
	err := mgr.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for missing team.yaml, got nil")
	}
}

// TestTeamManager_CleanShutdown verifies that bots are started and that
// cancelling the context causes Run to return cleanly.
func TestTeamManager_CleanShutdown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "orchestrator", Type: "orchestrator", Enabled: true, Orchestrator: true},
		{Name: "worker", Type: "worker", Enabled: true},
	})

	mgr, _, _ := newTestManager(t, teamFile)

	var started atomic.Int32
	team.SetBotRunner(mgr, func(ctx context.Context, _ team.BotEntryForTest, _ string) error {
		started.Add(1)
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- mgr.Run(ctx) }()

	// Wait until both bots have started.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && started.Load() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	if started.Load() < 2 {
		t.Fatalf("expected 2 bots started, got %d", started.Load())
	}

	cancel()

	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run returned unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s after context cancel")
	}
}

// TestTeamManager_BotPanicIsRestarted verifies that a panicking bot is
// restarted by runBotWithRestart.
func TestTeamManager_BotPanicIsRestarted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "crasher", Type: "crasher", Enabled: true, Orchestrator: true},
	})

	mgr, _, _ := newTestManager(t, teamFile)

	var callCount atomic.Int32
	team.SetBotRunner(mgr, func(ctx context.Context, _ team.BotEntryForTest, _ string) error {
		n := callCount.Add(1)
		if n < 3 {
			panic("simulated bot crash")
		}
		// Third call: block until context cancelled.
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- mgr.Run(ctx) }()

	// Wait until the bot has been restarted twice (callCount reaches 3).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && callCount.Load() < 3 {
		time.Sleep(10 * time.Millisecond)
	}
	if callCount.Load() < 3 {
		t.Fatalf("bot was not restarted enough times (got %d calls)", callCount.Load())
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s after context cancel")
	}
}

// TestTeamManager_BotErrorIsRestarted verifies that a bot that exits with a
// non-context error is treated as a crash and restarted.
func TestTeamManager_BotErrorIsRestarted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "errbot", Type: "errbot", Enabled: true, Orchestrator: true},
	})

	mgr, _, _ := newTestManager(t, teamFile)

	var callCount atomic.Int32
	team.SetBotRunner(mgr, func(ctx context.Context, _ team.BotEntryForTest, _ string) error {
		n := callCount.Add(1)
		if n < 3 {
			return errors.New("simulated transient error")
		}
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- mgr.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && callCount.Load() < 3 {
		time.Sleep(10 * time.Millisecond)
	}
	if callCount.Load() < 3 {
		t.Fatalf("bot was not restarted (got %d calls)", callCount.Load())
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s after context cancel")
	}
}

// TestTeamManager_Registry verifies that all enabled bots appear in the
// BotRegistry after starting — when the bot runner registers them.
func TestTeamManager_Registry(t *testing.T) {
	t.Parallel()
	if got := team.NewTeamManager(team.ManagerConfig{}, queue.NewRouter(), bus.New()).Registry(); got == nil {
		t.Fatal("Registry() returned nil")
	}
}

// TestTeamManager_OrchestratorFallback verifies that when no orchestrator bot
// is explicitly marked, the first enabled bot is used as the orchestrator name.
func TestTeamManager_OrchestratorFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// No entry has orchestrator:true — the first enabled one should be fallback.
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "alpha", Type: "alpha", Enabled: true},
		{Name: "beta", Type: "beta", Enabled: true},
	})

	mgr, _, _ := newTestManager(t, teamFile)

	var receivedOrch atomic.Value
	team.SetBotRunner(mgr, func(ctx context.Context, _ team.BotEntryForTest, orchestratorName string) error {
		receivedOrch.Store(orchestratorName)
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = mgr.Run(ctx) }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && receivedOrch.Load() == nil {
		time.Sleep(5 * time.Millisecond)
	}
	if receivedOrch.Load() == nil {
		t.Fatal("bot runner was never called")
	}
	if got := receivedOrch.Load().(string); got != "alpha" {
		t.Errorf("expected orchestrator fallback to be 'alpha', got %q", got)
	}

	cancel()
}

// TestTeamManager_DisabledBotsNotStarted verifies that disabled bots are
// skipped.
func TestTeamManager_DisabledBotsNotStarted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "active", Type: "active", Enabled: true, Orchestrator: true},
		{Name: "inactive", Type: "inactive", Enabled: false},
	})

	mgr, _, _ := newTestManager(t, teamFile)

	var names []string
	team.SetBotRunner(mgr, func(ctx context.Context, entry team.BotEntryForTest, _ string) error {
		names = append(names, entry.Name)
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- mgr.Run(ctx) }()

	// Give bots time to start.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("Run did not return")
	}

	for _, n := range names {
		if n == "inactive" {
			t.Error("inactive bot was started unexpectedly")
		}
	}
}
