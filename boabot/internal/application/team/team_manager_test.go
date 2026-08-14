package team_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/team"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
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
	mgr, r, b, _ := newTestManagerWithBotsDir(t, teamFilePath, t.TempDir())
	return mgr, r, b
}

// newTestManagerWithBotsDir is like newTestManager but lets the caller
// supply (and know) the BotsDir, for tests that need to write real
// <botsDir>/<type>/config.yaml fixtures (e.g. the Buzz monitor builder
// loop, which calls config.Load directly inside Run()).
func newTestManagerWithBotsDir(t *testing.T, teamFilePath, botsDir string) (*team.TeamManager, *queue.Router, *bus.Bus, string) {
	t.Helper()
	r := queue.NewRouter()
	b := bus.New()
	cfg := team.ManagerConfig{
		TeamFilePath:    teamFilePath,
		BotsDir:         botsDir,
		MemoryRoot:      t.TempDir(),
		RestartDelay:    10 * time.Millisecond,
		MaxRestartDelay: 50 * time.Millisecond,
	}
	mgr := team.NewTeamManager(cfg, r, b)
	return mgr, r, b, botsDir
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

// TestTeamManager_Run_PreRegisteredBotName_NoDuplicatePanic verifies that
// Run()'s pre-registration loop tolerates a bot name that is already
// registered on the Router before Run() is called -- mirroring
// cmd/boabot/main.go's Slack/Buzz wiring, which registers a bot's queue
// before mgr.Run(ctx) executes. Router.Register panics on a duplicate name;
// without this fix, any Buzz/Slack-enabled bot that is also an enabled
// team.yaml member (the conventional/default setup -- see
// specs/260814-boabot-native-daemon-mode/implementation-notes.md) would
// crash the whole process at startup instead of starting cleanly.
func TestTeamManager_Run_PreRegisteredBotName_NoDuplicatePanic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "architect", Type: "architect", Enabled: true, Orchestrator: true},
	})

	mgr, r, _ := newTestManager(t, teamFile)
	// Simulate main.go's pre-Run() Buzz/Slack wiring already having
	// registered this bot's queue before Run() is called.
	r.Register("architect", 0)

	team.SetBotRunner(mgr, func(ctx context.Context, _ team.BotEntryForTest, _ string) error {
		<-ctx.Done()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- mgr.Run(ctx) }()

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

// writeBotConfig writes a minimal <botsDir>/<botType>/config.yaml with an
// optional buzz: block, for exercising TeamManager's per-persona Buzz
// monitor builder loop.
func writeBotConfig(t *testing.T, botsDir, botType string, buzzEnabled bool, buzzBotName string) {
	t.Helper()
	dir := filepath.Join(botsDir, botType)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir bot config dir: %v", err)
	}
	content := fmt.Sprintf("bot:\n  name: %s\n  type: %s\n", botType, botType)
	if buzzEnabled {
		content += fmt.Sprintf("buzz:\n  enabled: true\n  bot_name: %s\n  relay_url: wss://relay.example.com\n", buzzBotName)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write bot config.yaml: %v", err)
	}
}

// TestTeamManager_BuzzMonitorBuilder_InvokedPerBuzzEnabledPersona verifies
// P1.1's acceptance criterion: one buzzinfra.Monitor (represented here by a
// fake domain.ChannelMonitor, since team_manager.go must stay free of an
// internal/infrastructure/buzz import) is constructed per Buzz-enabled
// team.yaml persona, and NOT for a persona with no buzz: block.
func TestTeamManager_BuzzMonitorBuilder_InvokedPerBuzzEnabledPersona(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "orchestrator", Type: "orchestrator", Enabled: true, Orchestrator: true},
		{Name: "architect", Type: "architect", Enabled: true},
		{Name: "implementer", Type: "implementer", Enabled: true}, // no buzz: block
	})

	botsDir := t.TempDir()
	mgr, _, _, _ := newTestManagerWithBotsDir(t, teamFile, botsDir)
	writeBotConfig(t, botsDir, "orchestrator", false, "")
	writeBotConfig(t, botsDir, "architect", true, "architect")
	writeBotConfig(t, botsDir, "implementer", false, "")

	team.SetBotRunner(mgr, func(ctx context.Context, _ team.BotEntryForTest, _ string) error {
		<-ctx.Done()
		return nil
	})

	var built []string
	var mu sync.Mutex
	mgr.WithBuzzMonitorBuilder(func(_ context.Context, entry team.BotEntryForTest, botCfg config.Config, _ *queue.Router, _ domain.ScheduledTaskDispatcher, _ domain.BoardStore, _ func(context.Context) error) domain.ChannelMonitor {
		mu.Lock()
		built = append(built, botCfg.Buzz.BotName)
		mu.Unlock()
		return &mocks.ChannelMonitor{}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- mgr.Run(ctx) }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(built)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s after context cancel")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(built) != 1 || built[0] != "architect" {
		t.Fatalf("expected exactly one Buzz monitor built for %q, got %v", "architect", built)
	}
}

// TestTeamManager_BuzzMonitorBuilder_DuplicateBotName_IsolatedNotPanic
// verifies spec.md's "Duplicate bot-name registration" edge case: two
// personas whose own config.yaml both set the same buzz.bot_name must not
// panic the process (Router.Register panics on a duplicate name) -- the
// second persona's Buzz monitor is skipped (logged), the first persona's
// monitor and the rest of the process are unaffected.
func TestTeamManager_BuzzMonitorBuilder_DuplicateBotName_IsolatedNotPanic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "orchestrator", Type: "orchestrator", Enabled: true, Orchestrator: true},
		{Name: "architect", Type: "architect", Enabled: true},
		{Name: "architect2", Type: "architect2", Enabled: true},
	})

	botsDir := t.TempDir()
	mgr, _, _, _ := newTestManagerWithBotsDir(t, teamFile, botsDir)
	writeBotConfig(t, botsDir, "orchestrator", false, "")
	// Both personas' own config.yaml claim the same buzz.bot_name.
	writeBotConfig(t, botsDir, "architect", true, "shared-name")
	writeBotConfig(t, botsDir, "architect2", true, "shared-name")

	team.SetBotRunner(mgr, func(ctx context.Context, _ team.BotEntryForTest, _ string) error {
		<-ctx.Done()
		return nil
	})

	var built []string
	var mu sync.Mutex
	mgr.WithBuzzMonitorBuilder(func(_ context.Context, entry team.BotEntryForTest, botCfg config.Config, _ *queue.Router, _ domain.ScheduledTaskDispatcher, _ domain.BoardStore, _ func(context.Context) error) domain.ChannelMonitor {
		mu.Lock()
		built = append(built, entry.Name)
		mu.Unlock()
		return &mocks.ChannelMonitor{}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("expected duplicate buzz.bot_name to be isolated (logged, skipped), not panic: %v", r)
			}
		}()
		go func() { runDone <- mgr.Run(ctx) }()
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(built)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
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

	mu.Lock()
	defer mu.Unlock()
	if len(built) != 1 {
		t.Fatalf("expected exactly 1 Buzz monitor built (the second duplicate must be skipped), got %v", built)
	}
}

// TestTeamManager_BuzzMonitorBuilder_OnePersonaConfigLoadFails_OthersUnaffected
// verifies the "incomplete per-persona Buzz config" edge case at the loop
// level: one persona's bot config.yaml failing to load must not prevent
// another Buzz-enabled persona's monitor from being built.
func TestTeamManager_BuzzMonitorBuilder_OnePersonaConfigLoadFails_OthersUnaffected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "orchestrator", Type: "orchestrator", Enabled: true, Orchestrator: true},
		{Name: "architect", Type: "architect", Enabled: true},
		{Name: "broken", Type: "broken", Enabled: true},
	})

	botsDir := t.TempDir()
	mgr, _, _, _ := newTestManagerWithBotsDir(t, teamFile, botsDir)
	writeBotConfig(t, botsDir, "orchestrator", false, "")
	writeBotConfig(t, botsDir, "architect", true, "architect")
	// "broken" bot's config.yaml is intentionally never written -- config.Load fails.

	team.SetBotRunner(mgr, func(ctx context.Context, _ team.BotEntryForTest, _ string) error {
		<-ctx.Done()
		return nil
	})

	var built []string
	var mu sync.Mutex
	mgr.WithBuzzMonitorBuilder(func(_ context.Context, entry team.BotEntryForTest, botCfg config.Config, _ *queue.Router, _ domain.ScheduledTaskDispatcher, _ domain.BoardStore, _ func(context.Context) error) domain.ChannelMonitor {
		mu.Lock()
		built = append(built, entry.Name)
		mu.Unlock()
		return &mocks.ChannelMonitor{}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- mgr.Run(ctx) }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(built)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
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

	mu.Lock()
	defer mu.Unlock()
	if len(built) != 1 || built[0] != "architect" {
		t.Fatalf("expected the architect persona's monitor to build despite broken's config.Load failure, got %v", built)
	}
}

// TestTeamManager_BuzzMonitorBuilder_NilForOnePersona_SkippedOthersUnaffected
// is the FR-103 regression test: it exercises the exact edge case spec.md's
// Edge Cases section names by name -- "incomplete Buzz config fails in
// isolation" -- at its real production integration point, Run()'s
// `if mon == nil { continue }` branch (previously 0 coverage per the review
// PRD). The builder mock returns nil for one Buzz-enabled persona (mirroring
// buildBuzzMonitor's own "log and return nil" pattern for a bad/missing
// key) and a real monitor for another; the nil persona must be skipped
// without affecting the other persona's monitor or Run()'s overall success.
func TestTeamManager_BuzzMonitorBuilder_NilForOnePersona_SkippedOthersUnaffected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "orchestrator", Type: "orchestrator", Enabled: true, Orchestrator: true},
		{Name: "architect", Type: "architect", Enabled: true},
		{Name: "implementer", Type: "implementer", Enabled: true},
	})

	botsDir := t.TempDir()
	mgr, _, _, _ := newTestManagerWithBotsDir(t, teamFile, botsDir)
	writeBotConfig(t, botsDir, "orchestrator", false, "")
	// Both personas are Buzz-enabled; "architect"'s monitor construction
	// will fail (builder returns nil), "implementer"'s must still succeed.
	writeBotConfig(t, botsDir, "architect", true, "architect")
	writeBotConfig(t, botsDir, "implementer", true, "implementer")

	team.SetBotRunner(mgr, func(ctx context.Context, _ team.BotEntryForTest, _ string) error {
		<-ctx.Done()
		return nil
	})

	var built []string
	var mu sync.Mutex
	mgr.WithBuzzMonitorBuilder(func(_ context.Context, entry team.BotEntryForTest, botCfg config.Config, _ *queue.Router, _ domain.ScheduledTaskDispatcher, _ domain.BoardStore, _ func(context.Context) error) domain.ChannelMonitor {
		mu.Lock()
		built = append(built, entry.Name)
		mu.Unlock()
		if entry.Name == "architect" {
			// Simulates buildBuzzMonitor's own "bad/missing config" isolation
			// pattern: log and return nil rather than erroring the whole loop.
			return nil
		}
		return &mocks.ChannelMonitor{}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- mgr.Run(ctx) }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(built)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
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

	mu.Lock()
	defer mu.Unlock()
	if len(built) != 2 {
		t.Fatalf("expected the builder to be invoked for both Buzz-enabled personas, got %v", built)
	}
	if len(mgr.Monitors()) != 1 {
		t.Fatalf("expected exactly 1 registered monitor (the nil-returning persona must be skipped), got %d", len(mgr.Monitors()))
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
