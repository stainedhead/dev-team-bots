// Package team internal tests — exercises unexported types that cannot be
// reached through the public API without the export_test.go seams.
package team

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
)

func TestNoopMCPClient_ListTools(t *testing.T) {
	c := &noopMCPClient{}
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned unexpected error: %v", err)
	}
	if tools != nil {
		t.Errorf("expected nil tools, got %v", tools)
	}
}

func TestNoopMCPClient_CallTool(t *testing.T) {
	c := &noopMCPClient{}
	_, err := c.CallTool(context.Background(), "any", nil)
	if err == nil {
		t.Fatal("expected error from noopMCPClient.CallTool, got nil")
	}
}

func TestSimpleWorkerFactory_New(t *testing.T) {
	sw := &simpleWorkerFactory{worker: nil}
	if sw.New() != nil {
		t.Error("expected nil worker from uninitialized factory")
	}
}

func TestManagerConfig_ApplyDefaults(t *testing.T) {
	var cfg ManagerConfig
	cfg.applyDefaults()
	if cfg.RestartDelay <= 0 {
		t.Error("RestartDelay should be positive after applyDefaults")
	}
	if cfg.MaxRestartDelay <= 0 {
		t.Error("MaxRestartDelay should be positive after applyDefaults")
	}
	if cfg.MemoryRoot == "" {
		t.Error("MemoryRoot should be non-empty after applyDefaults")
	}
}

func TestLoadTeamConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	badYAML := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badYAML, []byte("team: [\ninvalid"), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_, err := loadTeamConfig(badYAML)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoadTeamConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `team:
  - name: bot1
    type: worker
    enabled: true
    orchestrator: false
`
	p := filepath.Join(dir, "team.yaml")
	if err := os.WriteFile(p, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("write team.yaml: %v", err)
	}
	tc, err := loadTeamConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tc.Team) != 1 || tc.Team[0].Name != "bot1" {
		t.Errorf("unexpected team config: %+v", tc)
	}
}

// ── teamAskRouter ─────────────────────────────────────────────────────────────

func TestTeamAskRouter_GetOrCreate_SameChannel(t *testing.T) {
	r := &teamAskRouter{chs: make(map[string]chan domain.AskRequest)}
	ch1 := r.getOrCreate("bot1")
	ch2 := r.getOrCreate("bot1")
	if ch1 != ch2 {
		t.Error("getOrCreate must return the same channel for the same bot")
	}
	if ch1 == nil {
		t.Error("getOrCreate must return a non-nil channel")
	}
}

func TestTeamAskRouter_Enqueue_NoChannel_ReturnsFalse(t *testing.T) {
	r := &teamAskRouter{chs: make(map[string]chan domain.AskRequest)}
	if r.Enqueue("unknown", domain.AskRequest{}) {
		t.Error("Enqueue must return false when no channel has been created")
	}
}

func TestTeamAskRouter_Enqueue_Success(t *testing.T) {
	r := &teamAskRouter{chs: make(map[string]chan domain.AskRequest)}
	r.getOrCreate("bot1")
	if !r.Enqueue("bot1", domain.AskRequest{Question: "hello"}) {
		t.Error("Enqueue must return true when channel has capacity")
	}
}

func TestTeamAskRouter_Enqueue_Full_ReturnsFalse(t *testing.T) {
	r := &teamAskRouter{chs: make(map[string]chan domain.AskRequest)}
	ch := r.getOrCreate("full-bot")
	for i := 0; i < cap(ch); i++ {
		ch <- domain.AskRequest{}
	}
	if r.Enqueue("full-bot", domain.AskRequest{}) {
		t.Error("Enqueue must return false when channel is at capacity")
	}
}

// ── isDirEmpty ────────────────────────────────────────────────────────────────

func TestIsDirEmpty_NonExistentPath_ReturnsTrue(t *testing.T) {
	empty, err := isDirEmpty(filepath.Join(t.TempDir(), "no-such-dir"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !empty {
		t.Error("expected empty=true for non-existent path")
	}
}

func TestIsDirEmpty_EmptyDir_ReturnsTrue(t *testing.T) {
	empty, err := isDirEmpty(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !empty {
		t.Error("expected empty=true for empty directory")
	}
}

func TestIsDirEmpty_NonEmptyDir_ReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	empty, err := isDirEmpty(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if empty {
		t.Error("expected empty=false for non-empty directory")
	}
}

// ── slackMonitors ─────────────────────────────────────────────────────────────

func TestTeamManager_SlackMonitors_Nil(t *testing.T) {
	tm := &TeamManager{slackMonitor: nil}
	if tm.slackMonitors() != nil {
		t.Error("expected nil slice when no monitor configured")
	}
}

// ── spawnTechLead / stopTechLead / isTechLeadRunning ─────────────────────────

func TestSpawnTechLead_NoEntry_ReturnsError(t *testing.T) {
	tm := &TeamManager{
		teamEntries: []BotEntry{{Name: "bot", Type: "worker"}},
		dynamicBots: make(map[string]*dynamicBot),
	}
	if err := tm.spawnTechLead(context.Background(), "tl-1"); err == nil {
		t.Error("expected error when team has no tech-lead entry")
	}
}

func TestStopTechLead_UnknownInstance_ReturnsError(t *testing.T) {
	tm := &TeamManager{dynamicBots: make(map[string]*dynamicBot)}
	if err := tm.stopTechLead(context.Background(), "unknown"); err == nil {
		t.Error("expected error for unknown instance name")
	}
}

func TestIsTechLeadRunning_UnknownInstance_ReturnsFalse(t *testing.T) {
	tm := &TeamManager{dynamicBots: make(map[string]*dynamicBot)}
	if tm.isTechLeadRunning(context.Background(), "unknown") {
		t.Error("expected false for unknown instance name")
	}
}

func TestSpawnStopIsTechLeadRunning(t *testing.T) {
	r := queue.NewRouter()
	tm := &TeamManager{
		cfg:         ManagerConfig{RestartDelay: 5 * time.Millisecond, MaxRestartDelay: 20 * time.Millisecond},
		router:      r,
		teamEntries: []BotEntry{{Name: "lead", Type: "tech-lead", Enabled: true}},
		dynamicBots: make(map[string]*dynamicBot),
		botRunner: func(ctx context.Context, _ BotEntry, _ string) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tm.spawnTechLead(ctx, "tl-1"); err != nil {
		t.Fatalf("spawnTechLead: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	if !tm.isTechLeadRunning(ctx, "tl-1") {
		t.Error("expected tech lead to be running after spawn")
	}

	if err := tm.stopTechLead(ctx, "tl-1"); err != nil {
		t.Errorf("stopTechLead: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if tm.isTechLeadRunning(ctx, "tl-1") {
		t.Error("expected tech lead to be stopped after stopTechLead")
	}

	tm.wg.Wait()
}
