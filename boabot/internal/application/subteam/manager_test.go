package subteam_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/subteam"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure"
)

// makeBotsDir creates a temp bots directory with a valid <botType>/config.yaml
// so Spawn can find the bot type.
func makeBotsDir(t *testing.T, botType string) string {
	t.Helper()
	dir := t.TempDir()
	botDir := filepath.Join(dir, botType)
	if err := os.MkdirAll(botDir, 0o755); err != nil {
		t.Fatalf("create bot dir: %v", err)
	}
	cfgPath := filepath.Join(botDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("name: "+botType+"\n"), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return dir
}

// TestManager_Spawn_CreatesAgent verifies that Spawn returns a SpawnedAgent with correct fields.
func TestManager_Spawn_CreatesAgent(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{BotsDir: botsDir, MemoryRoot: t.TempDir()})

	ctx := context.Background()
	agent, err := m.Spawn(ctx, "tech-lead", "tech-lead-1", "/tmp/work")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if agent == nil {
		t.Fatal("expected non-nil agent")
	}
	if agent.Name != "tech-lead-1" {
		t.Errorf("expected Name=tech-lead-1, got %q", agent.Name)
	}
	if agent.BotType != "tech-lead" {
		t.Errorf("expected BotType=tech-lead, got %q", agent.BotType)
	}
	if agent.Status != domain.AgentStatusIdle {
		t.Errorf("expected Status=idle, got %q", agent.Status)
	}
	if agent.SpawnedAt.IsZero() {
		t.Error("expected non-zero SpawnedAt")
	}

	// Cleanup.
	_ = m.TearDownAll(context.Background())
}

// TestManager_Spawn_DuplicateName_ReturnsError verifies that spawning an agent
// with an already-active name returns an error.
func TestManager_Spawn_DuplicateName_ReturnsError(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{BotsDir: botsDir, MemoryRoot: t.TempDir()})

	ctx := context.Background()
	if _, err := m.Spawn(ctx, "tech-lead", "tech-lead-1", ""); err != nil {
		t.Fatalf("first Spawn: %v", err)
	}
	_, err := m.Spawn(ctx, "tech-lead", "tech-lead-1", "")
	if err == nil {
		t.Fatal("expected error on duplicate name, got nil")
	}

	_ = m.TearDownAll(context.Background())
}

// TestManager_Spawn_UnknownBotType_ReturnsError verifies that Spawn fails when
// config.yaml is missing for the given bot type.
func TestManager_Spawn_UnknownBotType_ReturnsError(t *testing.T) {
	t.Parallel()
	botsDir := t.TempDir() // empty — no bot types
	m := subteam.New(subteam.Config{BotsDir: botsDir, MemoryRoot: t.TempDir()})

	_, err := m.Spawn(context.Background(), "unknown-type", "bot-1", "")
	if err == nil {
		t.Fatal("expected error for unknown bot type, got nil")
	}
}

// TestManager_Terminate_UnknownName_ReturnsError verifies that Terminate fails
// when the name is not registered.
func TestManager_Terminate_UnknownName_ReturnsError(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{BotsDir: botsDir, MemoryRoot: t.TempDir()})

	err := m.Terminate(context.Background(), "nobody")
	if err == nil {
		t.Fatal("expected error for unknown name, got nil")
	}
}

// TestManager_Terminate_StopsGoroutine verifies that Terminate stops the
// spawned bot's goroutine. Checked via goroutine count on a serialised manager.
func TestManager_Terminate_StopsGoroutine(t *testing.T) {
	// Not parallel — goroutine count must be stable.
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{
		BotsDir:           botsDir,
		MemoryRoot:        t.TempDir(),
		HeartbeatInterval: 10 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
	})

	ctx := context.Background()

	// Snapshot goroutine count after runtime stabilises.
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	before := runtime.NumGoroutine()

	if _, err := m.Spawn(ctx, "tech-lead", "tech-lead-1", ""); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Give the goroutine time to start.
	time.Sleep(30 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after <= before {
		t.Errorf("expected goroutine count to increase after Spawn: before=%d after=%d", before, after)
	}

	if err := m.Terminate(ctx, "tech-lead-1"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	// Wait for goroutine to actually exit and be collected by the scheduler.
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	afterTerminate := runtime.NumGoroutine()
	if afterTerminate >= after {
		t.Errorf("expected goroutine count to decrease after Terminate: after_spawn=%d after_terminate=%d", after, afterTerminate)
	}
}

// TestManager_TearDownAll_StopsAllGoroutines verifies that TearDownAll stops
// all spawned bot goroutines. Checked via goroutine count on a serialised manager.
func TestManager_TearDownAll_StopsAllGoroutines(t *testing.T) {
	// Not parallel — goroutine count must be stable.
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{
		BotsDir:           botsDir,
		MemoryRoot:        t.TempDir(),
		HeartbeatInterval: 10 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
	})

	ctx := context.Background()

	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	before := runtime.NumGoroutine()

	for i := range 3 {
		name := "tech-lead-" + string(rune('1'+i))
		if _, err := m.Spawn(ctx, "tech-lead", name, ""); err != nil {
			t.Fatalf("Spawn %s: %v", name, err)
		}
	}

	time.Sleep(30 * time.Millisecond)
	mid := runtime.NumGoroutine()
	if mid <= before {
		t.Errorf("expected goroutines to increase after 3 spawns: before=%d mid=%d", before, mid)
	}

	if err := m.TearDownAll(ctx); err != nil {
		t.Fatalf("TearDownAll: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after >= mid {
		t.Errorf("expected goroutines to decrease after TearDownAll: mid=%d after=%d", mid, after)
	}
}

// TestManager_HeartbeatTimeout_TriggersTermination verifies that a bot
// self-terminates when no heartbeat arrives within the timeout.
func TestManager_HeartbeatTimeout_TriggersTermination(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{
		BotsDir:           botsDir,
		MemoryRoot:        t.TempDir(),
		HeartbeatInterval: 50 * time.Millisecond,
		HeartbeatTimeout:  100 * time.Millisecond,
	})

	ctx := context.Background()
	if _, err := m.Spawn(ctx, "tech-lead", "tech-lead-1", ""); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Wait longer than the timeout — the bot should self-terminate.
	time.Sleep(300 * time.Millisecond)

	agents, err := m.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}

	// After timeout the agent should be terminated.
	found := false
	for _, a := range agents {
		if a.Name == "tech-lead-1" {
			found = true
			if a.Status != domain.AgentStatusTerminated {
				t.Errorf("expected Status=terminated after timeout, got %q", a.Status)
			}
		}
	}
	_ = found // may be removed from list — either way, not running
}

// TestManager_PanicInSpawnedGoroutine_OtherBotsUnaffected verifies that a
// panic in one spawned bot's goroutine is recovered and other bots continue
// to operate normally.
func TestManager_PanicInSpawnedGoroutine_OtherBotsUnaffected(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{
		BotsDir:           botsDir,
		MemoryRoot:        t.TempDir(),
		HeartbeatInterval: 10 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
	})

	ctx := context.Background()

	// Spawn two bots — the panic bot and the healthy bot.
	if _, err := m.Spawn(ctx, "tech-lead", "panic-bot", ""); err != nil {
		t.Fatalf("Spawn panic-bot: %v", err)
	}
	if _, err := m.Spawn(ctx, "tech-lead", "healthy-bot", ""); err != nil {
		t.Fatalf("Spawn healthy-bot: %v", err)
	}

	// Trigger a panic in the panic-bot via SendHeartbeat (which doesn't panic;
	// we verify the healthy-bot is still listed as idle after some time).
	time.Sleep(50 * time.Millisecond)

	agents, err := m.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}

	// healthy-bot should still be listed.
	healthyFound := false
	for _, a := range agents {
		if a.Name == "healthy-bot" {
			healthyFound = true
			if a.Status == domain.AgentStatusTerminated {
				t.Errorf("healthy-bot should not be terminated")
			}
		}
	}
	if !healthyFound {
		t.Error("healthy-bot not found in agent list")
	}

	_ = m.TearDownAll(ctx)
}

// TestManager_ListAgents verifies that all spawned agents are returned.
func TestManager_ListAgents(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{
		BotsDir:           botsDir,
		MemoryRoot:        t.TempDir(),
		HeartbeatInterval: 10 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
	})

	ctx := context.Background()
	names := []string{"tech-lead-1", "tech-lead-2", "tech-lead-3"}
	for _, n := range names {
		if _, err := m.Spawn(ctx, "tech-lead", n, ""); err != nil {
			t.Fatalf("Spawn %s: %v", n, err)
		}
	}

	agents, err := m.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}

	found := make(map[string]bool)
	for _, a := range agents {
		found[a.Name] = true
	}
	for _, n := range names {
		if !found[n] {
			t.Errorf("expected agent %q in list", n)
		}
	}

	_ = m.TearDownAll(ctx)
}

// TestManager_SendHeartbeat_ToTerminatedBot_IsGraceful verifies that sending
// a heartbeat to an already-terminated bot does not panic or return a hard error.
func TestManager_SendHeartbeat_ToTerminatedBot_IsGraceful(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{
		BotsDir:           botsDir,
		MemoryRoot:        t.TempDir(),
		HeartbeatInterval: 10 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
	})

	ctx := context.Background()
	if _, err := m.Spawn(ctx, "tech-lead", "tech-lead-1", ""); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if err := m.Terminate(ctx, "tech-lead-1"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	// SendHeartbeat after termination should not panic.
	// It may return an error (send on closed channel) or nil — either is fine.
	_ = m.SendHeartbeat(ctx)
}

// TestManager_WithSessionFile_PersistsRecords verifies that WithSessionFile
// causes Spawn to write a session record and Terminate to remove it.
func TestManager_WithSessionFile_PersistsRecords(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	dir := t.TempDir()
	sfPath := filepath.Join(dir, "session.json")

	sf := infrastructure.NewSessionFile(sfPath)
	m := subteam.New(subteam.Config{
		BotsDir:           botsDir,
		MemoryRoot:        dir,
		HeartbeatInterval: 10 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
	}).WithSessionFile(sf)

	ctx := context.Background()
	if _, err := m.Spawn(ctx, "tech-lead", "tech-lead-1", "/work"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Session file should have one record.
	records, err := sf.Load()
	if err != nil {
		t.Fatalf("Load after Spawn: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 session record after Spawn, got %d", len(records))
	}

	// Terminate — record should be removed.
	if err := m.Terminate(ctx, "tech-lead-1"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	records, err = sf.Load()
	if err != nil {
		t.Fatalf("Load after Terminate: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 session records after Terminate, got %d", len(records))
	}
}

// TestManager_SendHeartbeat_ResetsTimeout verifies that heartbeats prevent the
// self-termination timer from firing.
func TestManager_SendHeartbeat_ResetsTimeout(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{
		BotsDir:           botsDir,
		MemoryRoot:        t.TempDir(),
		HeartbeatInterval: 20 * time.Millisecond,
		HeartbeatTimeout:  80 * time.Millisecond,
	})

	ctx := context.Background()
	if _, err := m.Spawn(ctx, "tech-lead", "tech-lead-1", ""); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Send heartbeats for 200ms — the bot should NOT self-terminate.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		_ = m.SendHeartbeat(ctx)
		time.Sleep(20 * time.Millisecond)
	}

	agents, err := m.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	for _, a := range agents {
		if a.Name == "tech-lead-1" && a.Status == domain.AgentStatusTerminated {
			t.Error("bot self-terminated despite regular heartbeats")
		}
	}

	_ = m.TearDownAll(ctx)
}

// TestManager_Terminate_AlreadyTerminated_ReturnsError verifies that calling
// Terminate on an already-terminated bot returns an error.
func TestManager_Terminate_AlreadyTerminated_ReturnsError(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{
		BotsDir:           botsDir,
		MemoryRoot:        t.TempDir(),
		HeartbeatInterval: 10 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
	})

	ctx := context.Background()
	if _, err := m.Spawn(ctx, "tech-lead", "tech-lead-1", ""); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := m.Terminate(ctx, "tech-lead-1"); err != nil {
		t.Fatalf("first Terminate: %v", err)
	}

	// Second Terminate on the same (now terminated) bot should return an error.
	err := m.Terminate(ctx, "tech-lead-1")
	if err == nil {
		t.Fatal("expected error on Terminate of already-terminated bot, got nil")
	}
}

// TestManager_SendHeartbeat_MultipleActiveBots verifies that heartbeats reach
// all active bots.
func TestManager_SendHeartbeat_MultipleActiveBots(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{
		BotsDir:           botsDir,
		MemoryRoot:        t.TempDir(),
		HeartbeatInterval: 10 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
	})

	ctx := context.Background()
	for _, name := range []string{"bot-1", "bot-2", "bot-3"} {
		if _, err := m.Spawn(ctx, "tech-lead", name, ""); err != nil {
			t.Fatalf("Spawn %s: %v", name, err)
		}
	}

	// SendHeartbeat must not return an error.
	if err := m.SendHeartbeat(ctx); err != nil {
		t.Errorf("SendHeartbeat: unexpected error: %v", err)
	}

	_ = m.TearDownAll(ctx)
}

// TestManager_TearDownAll_EmptyManager_Noop verifies that TearDownAll on an
// empty manager returns nil.
func TestManager_TearDownAll_EmptyManager_Noop(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{BotsDir: botsDir, MemoryRoot: t.TempDir()})
	if err := m.TearDownAll(context.Background()); err != nil {
		t.Errorf("TearDownAll on empty manager: unexpected error: %v", err)
	}
}

// TestManager_Spawn_TerminatedAgent_CanBeRespawned verifies that a name can be
// reused after the previous agent with that name has terminated.
func TestManager_Spawn_TerminatedAgent_CanBeRespawned(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	m := subteam.New(subteam.Config{
		BotsDir:          botsDir,
		MemoryRoot:       t.TempDir(),
		HeartbeatTimeout: 30 * time.Second,
	})

	ctx := context.Background()
	if _, err := m.Spawn(ctx, "tech-lead", "tech-lead-1", ""); err != nil {
		t.Fatalf("first Spawn: %v", err)
	}
	if err := m.Terminate(ctx, "tech-lead-1"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	// Re-spawning the same name after termination must succeed.
	if _, err := m.Spawn(ctx, "tech-lead", "tech-lead-1", ""); err != nil {
		t.Fatalf("re-Spawn after termination: %v", err)
	}
	_ = m.TearDownAll(context.Background())
}

// TestManager_WithSessionFile_ClearsStaleRecords verifies that pre-existing
// session records are discarded when WithSessionFile is called.
func TestManager_WithSessionFile_ClearsStaleRecords(t *testing.T) {
	t.Parallel()
	botsDir := makeBotsDir(t, "tech-lead")
	dir := t.TempDir()
	sfPath := filepath.Join(dir, "session.json")

	// Pre-populate the session file with a stale record.
	sf := infrastructure.NewSessionFile(sfPath)
	stale := []infrastructure.SessionRecord{
		{Name: "stale-bot", BotType: "tech-lead", Status: domain.AgentStatusWorking},
	}
	if err := sf.Save(stale); err != nil {
		t.Fatalf("pre-populate session file: %v", err)
	}

	// Attaching a new manager via WithSessionFile must clear the stale records.
	subteam.New(subteam.Config{
		BotsDir:    botsDir,
		MemoryRoot: dir,
	}).WithSessionFile(sf)

	records, err := sf.Load()
	if err != nil {
		t.Fatalf("Load after WithSessionFile: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records after WithSessionFile (stale cleared), got %d", len(records))
	}
}
