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
	_, err := LoadTeamConfig(badYAML)
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
	tc, err := LoadTeamConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tc.Team) != 1 || tc.Team[0].Name != "bot1" {
		t.Errorf("unexpected team config: %+v", tc)
	}
}

// ── resolveTaskOutcome ────────────────────────────────────────────────────────

func TestResolveTaskOutcome(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		success   bool
		wantTask  domain.DirectTaskStatus
		wantBoard domain.WorkItemStatus
	}{
		{
			name:      "success no marker → done",
			output:    "All done.",
			success:   true,
			wantTask:  domain.DirectTaskStatusSucceeded,
			wantBoard: domain.WorkItemStatusDone,
		},
		{
			name:      "blocked marker overrides success",
			output:    "Missing git repo.\nTASK_OUTCOME: blocked",
			success:   true,
			wantTask:  domain.DirectTaskStatusBlocked,
			wantBoard: domain.WorkItemStatusBlocked,
		},
		{
			name:      "errored marker overrides success",
			output:    "Fatal failure.\nTASK_OUTCOME: errored",
			success:   true,
			wantTask:  domain.DirectTaskStatusErrored,
			wantBoard: domain.WorkItemStatusErrored,
		},
		{
			name:      "runtime failure with no marker → errored",
			output:    "",
			success:   false,
			wantTask:  domain.DirectTaskStatusErrored,
			wantBoard: domain.WorkItemStatusErrored,
		},
		{
			name:      "blocked marker with runtime failure",
			output:    "TASK_OUTCOME: blocked",
			success:   false,
			wantTask:  domain.DirectTaskStatusBlocked,
			wantBoard: domain.WorkItemStatusBlocked,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTask, gotBoard := resolveTaskOutcome(tc.output, tc.success)
			if gotTask != tc.wantTask {
				t.Errorf("task status: got %q, want %q", gotTask, tc.wantTask)
			}
			if gotBoard != tc.wantBoard {
				t.Errorf("board status: got %q, want %q", gotBoard, tc.wantBoard)
			}
		})
	}
}

// ── boardTracksSource ─────────────────────────────────────────────────────────

// TestBoardTracksSource verifies which DirectTaskSource values have a
// corresponding board item whose status the shared TaskResultHandler
// updates on completion (startBot's inline closure, team_manager.go).
// Board-sourced tasks always do (BoardDispatch); Buzz-sourced tasks now do
// too, since BuzzTaskBridge (P2.2) creates a board item alongside every
// Buzz-dispatched DirectTask (FR-005's "updates as the task progresses ...
// reflects completion"). Chat/operator-sourced tasks have no board item.
func TestBoardTracksSource(t *testing.T) {
	tests := []struct {
		source domain.DirectTaskSource
		want   bool
	}{
		{domain.DirectTaskSourceBoard, true},
		{domain.DirectTaskSourceBuzz, true},
		{domain.DirectTaskSourceChat, false},
		{domain.DirectTaskSourceOperator, false},
	}
	for _, tc := range tests {
		t.Run(string(tc.source), func(t *testing.T) {
			if got := boardTracksSource(tc.source); got != tc.want {
				t.Errorf("boardTracksSource(%q) = %v, want %v", tc.source, got, tc.want)
			}
		})
	}
}

// ── chatMessageThreadID ────────────────────────────────────────────────────────

// TestChatMessageThreadID verifies which DirectTaskSource values' own
// task.ThreadID is safe to record on the shared chat feed's inbound
// completion message. Only chat-sourced tasks have a ThreadID that
// corresponds to a real domain.ChatThread the operator created via the
// web-UI chat interface. Board/operator-sourced tasks already dispatch with
// an empty ThreadID (pre-existing convention). Buzz-sourced tasks carry a
// real, non-empty ThreadID -- the Nostr channel UUID, needed by
// BuzzTaskBridge/ChatTaskManager's own scheduling-confirmation pending map
// -- which must NOT leak into the shared chat feed as a message ThreadID:
// it does not correspond to any registered ChatThread and would render as
// an orphaned/mislabeled grouping in GET /api/v1/chat's flat listing
// (spec.md's non-goal: this feature populates Board/Tasks, not a new chat
// surface).
func TestChatMessageThreadID(t *testing.T) {
	tests := []struct {
		name string
		task domain.DirectTask
		want string
	}{
		{
			name: "chat source keeps its own ThreadID",
			task: domain.DirectTask{Source: domain.DirectTaskSourceChat, ThreadID: "thread-abc"},
			want: "thread-abc",
		},
		{
			name: "buzz source's channel-UUID ThreadID is not leaked into the chat feed",
			task: domain.DirectTask{Source: domain.DirectTaskSourceBuzz, ThreadID: "nostr-channel-uuid"},
			want: "",
		},
		{
			name: "board source (already empty ThreadID today)",
			task: domain.DirectTask{Source: domain.DirectTaskSourceBoard, ThreadID: ""},
			want: "",
		},
		{
			name: "operator source (already empty ThreadID today)",
			task: domain.DirectTask{Source: domain.DirectTaskSourceOperator, ThreadID: ""},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatMessageThreadID(tc.task); got != tc.want {
				t.Errorf("chatMessageThreadID(%+v) = %q, want %q", tc.task, got, tc.want)
			}
		})
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

// ── monitors / WithSlackMonitor / forwardResultToMonitors ──────────────────

// fakeChannelMonitor is a minimal domain.ChannelMonitor test double used to
// verify that TeamManager forwards task results to every registered monitor,
// not just a single hardcoded Slack instance.
type fakeChannelMonitor struct {
	handled []domain.TaskResultPayload
}

func (f *fakeChannelMonitor) Start(context.Context) error { return nil }
func (f *fakeChannelMonitor) Stop(context.Context) error  { return nil }
func (f *fakeChannelMonitor) HandleResult(_ context.Context, p domain.TaskResultPayload) {
	f.handled = append(f.handled, p)
}

func TestTeamManager_Monitors_EmptyByDefault(t *testing.T) {
	tm := &TeamManager{}
	if len(tm.monitors) != 0 {
		t.Errorf("expected no monitors by default, got %d", len(tm.monitors))
	}
}

func TestTeamManager_WithChannelMonitor_AppendsToMonitors(t *testing.T) {
	tm := &TeamManager{}
	m1 := &fakeChannelMonitor{}
	tm.WithChannelMonitor(m1)

	if len(tm.monitors) != 1 {
		t.Fatalf("expected 1 monitor, got %d", len(tm.monitors))
	}
	if tm.monitors[0] != domain.ChannelMonitor(m1) {
		t.Error("expected the registered monitor to be present in tm.monitors")
	}
}

func TestTeamManager_WithChannelMonitor_MultipleCallsAppendEachMonitor(t *testing.T) {
	tm := &TeamManager{}
	m1 := &fakeChannelMonitor{}
	m2 := &fakeChannelMonitor{}
	tm.WithChannelMonitor(m1)
	tm.WithChannelMonitor(m2)

	if len(tm.monitors) != 2 {
		t.Fatalf("expected 2 monitors after two calls (e.g. Slack + Buzz), got %d", len(tm.monitors))
	}
}

func TestForwardResultToMonitors_CallsHandleResultOnEveryMonitor(t *testing.T) {
	m1 := &fakeChannelMonitor{}
	m2 := &fakeChannelMonitor{}
	payload := domain.TaskResultPayload{TaskID: "t-1", Output: "done", Success: true}

	forwardResultToMonitors(context.Background(), []domain.ChannelMonitor{m1, m2}, payload)

	if len(m1.handled) != 1 || m1.handled[0].TaskID != "t-1" {
		t.Errorf("expected m1 to receive the payload, got %+v", m1.handled)
	}
	if len(m2.handled) != 1 || m2.handled[0].TaskID != "t-1" {
		t.Errorf("expected m2 to receive the payload, got %+v", m2.handled)
	}
}

func TestForwardResultToMonitors_EmptySlice_NoOp(t *testing.T) {
	// Must not panic when no monitors are registered (e.g. Slack and Buzz
	// both disabled).
	forwardResultToMonitors(context.Background(), nil, domain.TaskResultPayload{TaskID: "t-2"})
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
