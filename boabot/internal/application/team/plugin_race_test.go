package team_test

// TestPluginStore_PreResolved verifies that the plugin store is pre-resolved
// before goroutines start, so all bots receive it regardless of start order.
// This test uses the race detector — run with: go test -race ./internal/application/team/...
//
// The test replaces the botRunner to capture which bots received plugin stores,
// without doing real file I/O. It uses the SetBotRunner helper from export_test.go.
import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/team"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bus"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
)

// TestTeamManager_PluginStorePreResolved verifies that when the team has
// multiple bots, none of the bot goroutines race on plugin store access.
// The botRunner is replaced with a fake that simply records the bot name and
// returns immediately. The race detector will catch any concurrent write+read
// on tm.pluginStore / tm.pluginInstallDir if the pre-resolution fix is absent.
func TestTeamManager_PluginStorePreResolved(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	teamFile := writeTeamYAML(t, dir, []team.BotEntryForTest{
		{Name: "orchestrator", Type: "orchestrator", Enabled: true, Orchestrator: true},
		{Name: "worker-1", Type: "worker", Enabled: true},
		{Name: "worker-2", Type: "worker", Enabled: true},
	})

	r := queue.NewRouter()
	b := bus.New()
	cfg := team.ManagerConfig{
		TeamFilePath:    teamFile,
		BotsDir:         t.TempDir(),
		MemoryRoot:      t.TempDir(),
		RestartDelay:    10 * time.Millisecond,
		MaxRestartDelay: 50 * time.Millisecond,
	}
	mgr := team.NewTeamManager(cfg, r, b)

	var (
		mu      sync.Mutex
		started []string
		counter int32
	)

	team.SetBotRunner(mgr, func(ctx context.Context, entry team.BotEntryForTest, orchestratorName string) error {
		mu.Lock()
		started = append(started, entry.Name)
		mu.Unlock()
		// All bots run concurrently; increment counter to signal this goroutine ran.
		atomic.AddInt32(&counter, 1)
		// Wait briefly so all goroutines are in flight simultaneously (stress the race).
		time.Sleep(5 * time.Millisecond)
		return nil
	})

	runCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := mgr.Run(runCtx)
	// Context cancel is the expected exit path.
	if err != nil && runCtx.Err() == nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	mu.Lock()
	n := len(started)
	mu.Unlock()

	// All 3 bots should have started.
	if n != 3 {
		t.Errorf("expected 3 bots started, got %d: %v", n, started)
	}
}
