package orchestrator_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/orchestrator"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// --- fakeBuzzBoardStore: minimal domain.BoardStore fake for BuzzTaskBridge tests. ---

type fakeBuzzBoardStore struct {
	mu        sync.Mutex
	created   []domain.WorkItem
	createErr error
}

func (f *fakeBuzzBoardStore) Create(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return domain.WorkItem{}, f.createErr
	}
	item.ID = "board-item-1"
	f.created = append(f.created, item)
	return item, nil
}
func (f *fakeBuzzBoardStore) Update(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	return item, nil
}
func (f *fakeBuzzBoardStore) Get(_ context.Context, _ string) (domain.WorkItem, error) {
	return domain.WorkItem{}, nil
}
func (f *fakeBuzzBoardStore) List(_ context.Context, _ domain.WorkItemFilter) ([]domain.WorkItem, error) {
	return nil, nil
}
func (f *fakeBuzzBoardStore) Delete(_ context.Context, _ string) error    { return nil }
func (f *fakeBuzzBoardStore) Reorder(_ context.Context, _ []string) error { return nil }

func (f *fakeBuzzBoardStore) getCreated() []domain.WorkItem {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.WorkItem, len(f.created))
	copy(out, f.created)
	return out
}

var _ domain.BoardStore = (*fakeBuzzBoardStore)(nil)

// --- Dispatch: plain (non-scheduling) instruction ---------------------------

func TestBuzzTaskBridge_Dispatch_PlainInstruction_DispatchesImmediatelyAndCreatesBoardItem(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	ctm := orchestrator.NewChatTaskManager(d)
	bridge := orchestrator.NewBuzzTaskBridge(d, board, ctm)

	result, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "chan-1", "please review the PR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Duplicate {
		t.Fatal("expected Duplicate=false")
	}
	if result.TaskID != "test-task-id" {
		t.Fatalf("expected TaskID from dispatcher, got %q", result.TaskID)
	}
	if !result.AwaitResult {
		t.Fatal("expected AwaitResult=true for an immediate dispatch")
	}
	if result.Reply != "" {
		t.Fatalf("expected no immediate Reply for a plain immediate dispatch, got %q", result.Reply)
	}

	if len(d.calls) != 1 {
		t.Fatalf("expected 1 dispatch call, got %d", len(d.calls))
	}
	if d.calls[0].BotName != "architect" || d.calls[0].Source != domain.DirectTaskSourceBuzz {
		t.Errorf("unexpected dispatch call: %+v", d.calls[0])
	}

	created := board.getCreated()
	if len(created) != 1 {
		t.Fatalf("expected 1 board item created, got %d", len(created))
	}
	if created[0].AssignedTo != "architect" || created[0].ActiveTaskID != "test-task-id" {
		t.Errorf("unexpected board item: %+v", created[0])
	}
	if created[0].Status != domain.WorkItemStatusInProgress {
		t.Errorf("expected board item Status=in-progress for an immediate dispatch, got %q", created[0].Status)
	}
}

// --- Dispatch: scheduling request (confirmation prompt, no task yet) --------

func TestBuzzTaskBridge_Dispatch_SchedulingRequest_ReturnsPromptNoTaskNoBoardItem(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	ctm := orchestrator.NewChatTaskManager(d)
	bridge := orchestrator.NewBuzzTaskBridge(d, board, ctm)

	result, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "chan-1",
		"schedule a weekly code review every Monday at 9am for the architect bot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TaskID != "" {
		t.Fatalf("expected no TaskID on the confirmation-prompt turn, got %q", result.TaskID)
	}
	if result.Reply == "" {
		t.Fatal("expected a non-empty confirmation prompt Reply")
	}
	if result.AwaitResult {
		t.Fatal("expected AwaitResult=false when nothing was dispatched yet")
	}
	if len(d.calls) != 0 {
		t.Fatalf("expected no dispatch calls yet, got %d", len(d.calls))
	}
	if len(board.getCreated()) != 0 {
		t.Fatal("expected no board item created for a bare confirmation prompt")
	}
}

// --- Dispatch: confirmed scheduling (future/recurring) creates a Backlog item

func TestBuzzTaskBridge_Dispatch_ConfirmedSchedule_CreatesBacklogBoardItem(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	ctm := orchestrator.NewChatTaskManager(d)
	bridge := orchestrator.NewBuzzTaskBridge(d, board, ctm)
	ctx := context.Background()

	_, err := bridge.Dispatch(ctx, "architect", "evt-1", "chan-1",
		"schedule a weekly code review every Monday at 9am for the architect bot")
	if err != nil {
		t.Fatalf("unexpected error on request turn: %v", err)
	}

	result, err := bridge.Dispatch(ctx, "architect", "evt-2", "chan-1", "yes")
	if err != nil {
		t.Fatalf("unexpected error on confirm turn: %v", err)
	}
	if result.TaskID != "test-task-id" {
		t.Fatalf("expected TaskID after confirmation, got %q", result.TaskID)
	}
	if result.Reply == "" {
		t.Fatal("expected a non-empty ack Reply after confirmation")
	}
	if result.AwaitResult {
		t.Fatal("expected AwaitResult=false for a recurring/future dispatch (not running now)")
	}
	if len(d.calls) != 1 || d.calls[0].Source != domain.DirectTaskSourceBuzz {
		t.Fatalf("expected 1 dispatch call tagged buzz, got %+v", d.calls)
	}

	created := board.getCreated()
	if len(created) != 1 {
		t.Fatalf("expected 1 board item created, got %d", len(created))
	}
	if created[0].Status != domain.WorkItemStatusBacklog {
		t.Errorf("expected board item Status=backlog for a not-yet-running scheduled task, got %q", created[0].Status)
	}
}

// --- Dispatch: cancellation ---------------------------------------------------

func TestBuzzTaskBridge_Dispatch_Cancellation_NoTaskNoBoardItem(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	ctm := orchestrator.NewChatTaskManager(d)
	bridge := orchestrator.NewBuzzTaskBridge(d, board, ctm)
	ctx := context.Background()

	_, _ = bridge.Dispatch(ctx, "architect", "evt-1", "chan-1",
		"schedule a weekly code review every Monday at 9am for the architect bot")

	result, err := bridge.Dispatch(ctx, "architect", "evt-2", "chan-1", "cancel")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TaskID != "" {
		t.Fatal("expected no TaskID after cancellation")
	}
	if len(d.calls) != 0 {
		t.Fatal("expected no dispatch calls after cancellation")
	}
	if len(board.getCreated()) != 0 {
		t.Fatal("expected no board item after cancellation")
	}
}

// --- Dispatch: relay-replay dedup by event ID --------------------------------

func TestBuzzTaskBridge_Dispatch_DuplicateEventID_NoOp(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	ctm := orchestrator.NewChatTaskManager(d)
	bridge := orchestrator.NewBuzzTaskBridge(d, board, ctm)
	ctx := context.Background()

	r1, err := bridge.Dispatch(ctx, "architect", "evt-dup", "chan-1", "please review the PR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r1.Duplicate {
		t.Fatal("expected first delivery to not be flagged duplicate")
	}

	r2, err := bridge.Dispatch(ctx, "architect", "evt-dup", "chan-1", "please review the PR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r2.Duplicate {
		t.Fatal("expected replayed event ID to be flagged duplicate")
	}
	if r2.TaskID != "" {
		t.Fatal("expected no TaskID on a duplicate delivery")
	}

	if len(d.calls) != 1 {
		t.Fatalf("expected exactly 1 dispatch call (the duplicate must not re-dispatch), got %d", len(d.calls))
	}
	if len(board.getCreated()) != 1 {
		t.Fatalf("expected exactly 1 board item (the duplicate must not create a second), got %d", len(board.getCreated()))
	}
}

// TestBuzzTaskBridge_Dispatch_FailedDispatch_ThenRedelivery_IsRetriedNotDuplicate
// is the FR-101 regression test: a dispatch attempt that fails must NOT mark
// its event ID as "seen". A relay reconnect/replay redelivering that same
// event ID afterward must be retried -- not silently reported Duplicate and
// dropped. Before the fix, markSeenIfDuplicate marked the event ID seen
// unconditionally on first delivery, regardless of whether the dispatch that
// followed succeeded.
func TestBuzzTaskBridge_Dispatch_FailedDispatch_ThenRedelivery_IsRetriedNotDuplicate(t *testing.T) {
	d := &fakeScheduledDispatcher{err: errors.New("transient failure")}
	board := &fakeBuzzBoardStore{}
	ctm := orchestrator.NewChatTaskManager(d)
	bridge := orchestrator.NewBuzzTaskBridge(d, board, ctm)
	ctx := context.Background()

	_, err := bridge.Dispatch(ctx, "architect", "evt-replay", "chan-1", "please review the PR")
	if err == nil {
		t.Fatal("expected the first dispatch attempt to fail")
	}

	// Relay reconnect/replay redelivers the identical event ID. The transient
	// failure is now gone.
	d.err = nil
	r2, err := bridge.Dispatch(ctx, "architect", "evt-replay", "chan-1", "please review the PR")
	if err != nil {
		t.Fatalf("unexpected error on redelivered dispatch: %v", err)
	}
	if r2.Duplicate {
		t.Fatal("expected the redelivered event ID (whose first attempt failed) to be retried, not reported Duplicate")
	}
	if r2.TaskID != "test-task-id" {
		t.Fatalf("expected the retried dispatch to succeed and return a TaskID, got %q", r2.TaskID)
	}
	if len(d.calls) != 2 {
		t.Fatalf("expected 2 dispatch calls (failed attempt + successful retry), got %d", len(d.calls))
	}
}

// --- Dispatch: errors ---------------------------------------------------------

func TestBuzzTaskBridge_Dispatch_DispatcherError_Propagates(t *testing.T) {
	d := &fakeScheduledDispatcher{err: errors.New("boom")}
	board := &fakeBuzzBoardStore{}
	ctm := orchestrator.NewChatTaskManager(d)
	bridge := orchestrator.NewBuzzTaskBridge(d, board, ctm)

	_, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "chan-1", "please review the PR")
	if err == nil {
		t.Fatal("expected an error to propagate from the dispatcher")
	}
}

func TestBuzzTaskBridge_Dispatch_BoardCreateError_NonFatal(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{createErr: errors.New("board unavailable")}
	ctm := orchestrator.NewChatTaskManager(d)
	bridge := orchestrator.NewBuzzTaskBridge(d, board, ctm)

	result, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "chan-1", "please review the PR")
	if err != nil {
		t.Fatalf("expected a board-item failure to be non-fatal to an already-dispatched task, got err=%v", err)
	}
	if result.TaskID != "test-task-id" {
		t.Fatalf("expected the task to still be reported dispatched, got TaskID=%q", result.TaskID)
	}
}

// --- buzzBoardTitle (via Dispatch's created board item): rune-safe truncation -

// TestBuzzTaskBridge_Dispatch_BoardTitle_MidRuneTruncation_IsValidUTF8 is the
// FR-102 regression test: an instruction whose 80-byte mark falls mid-rune
// (multi-byte UTF-8, e.g. Japanese text after 78 ASCII bytes) must not
// produce an invalid-UTF-8 board item title. Before the fix, buzzBoardTitle
// sliced by byte index (title[:80]), which can split a multi-byte rune in
// half.
func TestBuzzTaskBridge_Dispatch_BoardTitle_MidRuneTruncation_IsValidUTF8(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	ctm := orchestrator.NewChatTaskManager(d)
	bridge := orchestrator.NewBuzzTaskBridge(d, board, ctm)

	instr := strings.Repeat("a", 78) + "日本語のテキストです"
	if _, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "chan-1", instr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	created := board.getCreated()
	if len(created) != 1 {
		t.Fatalf("expected 1 board item, got %d", len(created))
	}
	title := created[0].Title
	if !utf8.ValidString(title) {
		t.Fatalf("expected a valid UTF-8 title, got invalid: %q", title)
	}
	if title == instr {
		t.Fatal("expected the title to actually be truncated for this input")
	}
}

// TestBuzzTaskBridge_Dispatch_BoardTitle_EmptyInstruction_FallsBackToDefault
// verifies the empty-instruction fallback ("Buzz task") still works after
// the rune-safe truncation fix.
func TestBuzzTaskBridge_Dispatch_BoardTitle_EmptyInstruction_FallsBackToDefault(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	ctm := orchestrator.NewChatTaskManager(d)
	bridge := orchestrator.NewBuzzTaskBridge(d, board, ctm)

	if _, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "chan-1", "   "); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	created := board.getCreated()
	if len(created) != 1 {
		t.Fatalf("expected 1 board item, got %d", len(created))
	}
	if created[0].Title != "Buzz task" {
		t.Fatalf("expected fallback title %q, got %q", "Buzz task", created[0].Title)
	}
}

// TestBuzzTaskBridge_Dispatch_BoardTitle_ASCIIOnly_TruncationUnchanged verifies
// the rune-safe fix does not change truncation behaviour for pure-ASCII
// instructions.
func TestBuzzTaskBridge_Dispatch_BoardTitle_ASCIIOnly_TruncationUnchanged(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	ctm := orchestrator.NewChatTaskManager(d)
	bridge := orchestrator.NewBuzzTaskBridge(d, board, ctm)

	instr := strings.Repeat("a", 100)
	if _, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "chan-1", instr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	created := board.getCreated()
	if len(created) != 1 {
		t.Fatalf("expected 1 board item, got %d", len(created))
	}
	want := strings.Repeat("a", 80) + "…"
	if created[0].Title != want {
		t.Fatalf("expected ASCII truncation to be unchanged: got %q, want %q", created[0].Title, want)
	}
}

// --- Dispatch: dedup TTL expiry ------------------------------------------------

func TestBuzzTaskBridge_Dispatch_EventIDDedupExpires(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	ctm := orchestrator.NewChatTaskManager(d)
	bridge := orchestrator.NewBuzzTaskBridgeWithDedupTTL(d, board, ctm, 10*time.Millisecond)
	ctx := context.Background()

	_, err := bridge.Dispatch(ctx, "architect", "evt-1", "chan-1", "please review the PR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(30 * time.Millisecond)

	r2, err := bridge.Dispatch(ctx, "architect", "evt-1", "chan-1", "please review the PR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r2.Duplicate {
		t.Fatal("expected the dedup window to have expired, allowing a second dispatch")
	}
}

// --- P1.3: KnownThread / dispatchedThreads -----------------------------------

func TestBuzzTaskBridge_KnownThread_UnknownRoot_ReturnsFalse(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	bridge := orchestrator.NewBuzzTaskBridge(d, board, nil)

	if bridge.KnownThread("architect", "never-dispatched") {
		t.Fatal("expected KnownThread=false for a thread never dispatched in")
	}
}

func TestBuzzTaskBridge_KnownThread_EmptyRootID_ReturnsFalse(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	bridge := orchestrator.NewBuzzTaskBridge(d, board, nil)
	_, _ = bridge.Dispatch(context.Background(), "architect", "evt-1", "some-thread", "hello")

	if bridge.KnownThread("architect", "") {
		t.Fatal("expected KnownThread=false for an empty rootID")
	}
}

func TestBuzzTaskBridge_KnownThread_AfterDispatch_ReturnsTrue(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	bridge := orchestrator.NewBuzzTaskBridge(d, board, nil)

	if _, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "thread-root-1", "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !bridge.KnownThread("architect", "thread-root-1") {
		t.Fatal("expected KnownThread=true after a successful Dispatch for this bot/thread")
	}
}

// TestBuzzTaskBridge_KnownThread_StrictlyPerPersona is the spec.md edge
// case: KnownThread must be keyed by botName + rootID, not just rootID, so
// persona A's dispatched thread never leaks into persona B's
// classification.
func TestBuzzTaskBridge_KnownThread_StrictlyPerPersona(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	bridge := orchestrator.NewBuzzTaskBridge(d, board, nil)

	if _, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "shared-thread-root", "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !bridge.KnownThread("architect", "shared-thread-root") {
		t.Fatal("expected KnownThread=true for architect/shared-thread-root")
	}
	if bridge.KnownThread("reviewer", "shared-thread-root") {
		t.Fatal("expected KnownThread=false for a different persona (reviewer) on the same rootID")
	}
}

func TestBuzzTaskBridge_KnownThread_DuplicateEventDoesNotUnmarkThread(t *testing.T) {
	// A duplicate (relay-replayed) event short-circuits before
	// markDispatchedThread would run again, but the thread should already
	// be known from the first, successful Dispatch.
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	bridge := orchestrator.NewBuzzTaskBridge(d, board, nil)
	ctx := context.Background()

	if _, err := bridge.Dispatch(ctx, "architect", "evt-1", "thread-root-2", "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := bridge.Dispatch(ctx, "architect", "evt-1", "thread-root-2", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Duplicate {
		t.Fatal("expected the second identical event ID to be reported as a duplicate")
	}
	if !bridge.KnownThread("architect", "thread-root-2") {
		t.Fatal("expected KnownThread=true to remain true after a duplicate-event no-op")
	}
}

// --- P1.5: ChatStore history replay ------------------------------------------

// fakeBuzzChatStore is a minimal, in-memory domain.ChatStore fake giving
// buzz_task_bridge_test.go control over List/Append errors (fail-open
// coverage) that the real InMemoryChatStore implementation doesn't easily
// let a test simulate.
type fakeBuzzChatStore struct {
	mu        sync.Mutex
	messages  []domain.ChatMessage
	listErr   error
	appendErr error
}

func (f *fakeBuzzChatStore) CreateThread(_ context.Context, title string, participants []string) (domain.ChatThread, error) {
	return domain.ChatThread{ID: "thread-1", Title: title, Participants: participants}, nil
}
func (f *fakeBuzzChatStore) ListThreads(_ context.Context) ([]domain.ChatThread, error) {
	return nil, nil
}
func (f *fakeBuzzChatStore) DeleteThread(_ context.Context, _ string) error { return nil }

func (f *fakeBuzzChatStore) Append(_ context.Context, msg domain.ChatMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appendErr != nil {
		return f.appendErr
	}
	// Newest-first, matching InMemoryChatStore's documented List order.
	f.messages = append([]domain.ChatMessage{msg}, f.messages...)
	return nil
}

func (f *fakeBuzzChatStore) List(_ context.Context, threadID string) ([]domain.ChatMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []domain.ChatMessage
	for _, m := range f.messages {
		if m.ThreadID == threadID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeBuzzChatStore) ListAll(_ context.Context) ([]domain.ChatMessage, error) { return nil, nil }
func (f *fakeBuzzChatStore) ListByBot(_ context.Context, _ string) ([]domain.ChatMessage, error) {
	return nil, nil
}

var _ domain.ChatStore = (*fakeBuzzChatStore)(nil)

// TestBuzzTaskBridge_Dispatch_NoChatStore_InstructionUnaugmented verifies
// the pre-existing (no history replay) behaviour is preserved when no
// ChatStore is wired (NewBuzzTaskBridge, not …WithChatStore).
func TestBuzzTaskBridge_Dispatch_NoChatStore_InstructionUnaugmented(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	bridge := orchestrator.NewBuzzTaskBridge(d, board, nil)

	if _, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "thread-1", "first message"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.calls[0].Instruction != "first message" {
		t.Fatalf("expected unaugmented instruction, got %q", d.calls[0].Instruction)
	}
}

// TestBuzzTaskBridge_Dispatch_ChatStore_FirstMessage_NoHistoryBlock verifies
// that a thread's first-ever message dispatches with no "Prior
// conversation" block (there is no prior history to replay yet).
func TestBuzzTaskBridge_Dispatch_ChatStore_FirstMessage_NoHistoryBlock(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	cs := &fakeBuzzChatStore{}
	bridge := orchestrator.NewBuzzTaskBridgeWithChatStore(d, board, nil, cs)

	if _, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "thread-1", "first message"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.calls[0].Instruction != "first message" {
		t.Fatalf("expected unaugmented instruction for a thread's first message, got %q", d.calls[0].Instruction)
	}

	msgs, _ := cs.List(context.Background(), "thread-1")
	if len(msgs) != 1 || msgs[0].Content != "first message" || msgs[0].Direction != domain.ChatDirectionOutbound {
		t.Fatalf("expected the inbound message recorded in ChatStore, got %+v", msgs)
	}
}

// TestBuzzTaskBridge_Dispatch_ChatStore_SecondTurn_ReplaysBotReply is P1.5's
// core acceptance criterion (FR-206): a two-turn thread conversation where
// the second turn's dispatched instruction reflects context from the
// first turn -- specifically the BOT'S OWN prior reply, not just the human
// side (advisor finding: ChatStore's outbound-append side must be owned
// somewhere, or continuation silently carries no bot context).
func TestBuzzTaskBridge_Dispatch_ChatStore_SecondTurn_ReplaysBotReply(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	cs := &fakeBuzzChatStore{}
	bridge := orchestrator.NewBuzzTaskBridgeWithChatStore(d, board, nil, cs)
	ctx := context.Background()

	if _, err := bridge.Dispatch(ctx, "architect", "evt-1", "thread-1", "what is the deploy status?"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Simulate the bot's reply being recorded (Monitor.recordOutbound's
	// job in production -- this test only needs BuzzTaskBridge's read side
	// to see it).
	if err := cs.Append(ctx, domain.ChatMessage{
		ThreadID: "thread-1", BotName: "architect",
		Direction: domain.ChatDirectionInbound, Content: "deploy is green",
	}); err != nil {
		t.Fatalf("append bot reply: %v", err)
	}

	if _, err := bridge.Dispatch(ctx, "architect", "evt-2", "thread-1", "and the last build?"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second := d.calls[len(d.calls)-1].Instruction
	if !strings.Contains(second, "deploy is green") {
		t.Fatalf("expected the second turn's instruction to replay the bot's prior reply, got %q", second)
	}
	if !strings.Contains(second, "what is the deploy status?") {
		t.Fatalf("expected the second turn's instruction to replay the human's prior message, got %q", second)
	}
	if !strings.Contains(second, "and the last build?") {
		t.Fatalf("expected the second turn's instruction to still contain the current message, got %q", second)
	}
}

// TestBuzzTaskBridge_Dispatch_ChatStore_DifferentThread_NoCrossTalk verifies
// that history replay is strictly per-threadID -- an unrelated thread's
// history never leaks into this thread's instruction.
func TestBuzzTaskBridge_Dispatch_ChatStore_DifferentThread_NoCrossTalk(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	cs := &fakeBuzzChatStore{}
	bridge := orchestrator.NewBuzzTaskBridgeWithChatStore(d, board, nil, cs)
	ctx := context.Background()

	if _, err := bridge.Dispatch(ctx, "architect", "evt-1", "thread-A", "secret project alpha details"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := bridge.Dispatch(ctx, "architect", "evt-2", "thread-B", "unrelated question"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second := d.calls[len(d.calls)-1].Instruction
	if strings.Contains(second, "secret project alpha details") {
		t.Fatalf("expected thread-B's instruction not to contain thread-A's history, got %q", second)
	}
}

// TestBuzzTaskBridge_Dispatch_ChatStore_ListError_FailsOpen verifies that a
// ChatStore.List failure degrades to an unaugmented instruction rather than
// blocking dispatch.
func TestBuzzTaskBridge_Dispatch_ChatStore_ListError_FailsOpen(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	cs := &fakeBuzzChatStore{listErr: errors.New("store unavailable")}
	bridge := orchestrator.NewBuzzTaskBridgeWithChatStore(d, board, nil, cs)

	result, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "thread-1", "hello")
	if err != nil {
		t.Fatalf("expected Dispatch to succeed despite a ChatStore.List failure, got err=%v", err)
	}
	if result.Duplicate {
		t.Fatal("did not expect a duplicate result")
	}
	if d.calls[0].Instruction != "hello" {
		t.Fatalf("expected unaugmented instruction on a List failure, got %q", d.calls[0].Instruction)
	}
}

// TestBuzzTaskBridge_Dispatch_ChatStore_AppendError_NonFatal verifies that
// a ChatStore.Append failure (recording the inbound message) is logged and
// non-fatal -- dispatch still proceeds.
func TestBuzzTaskBridge_Dispatch_ChatStore_AppendError_NonFatal(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	cs := &fakeBuzzChatStore{appendErr: errors.New("write failed")}
	bridge := orchestrator.NewBuzzTaskBridgeWithChatStore(d, board, nil, cs)

	result, err := bridge.Dispatch(context.Background(), "architect", "evt-1", "thread-1", "hello")
	if err != nil {
		t.Fatalf("expected Dispatch to succeed despite a ChatStore.Append failure, got err=%v", err)
	}
	if result.Duplicate {
		t.Fatal("did not expect a duplicate result")
	}
	if len(d.calls) != 1 {
		t.Fatalf("expected dispatch to proceed despite the append failure, got %d calls", len(d.calls))
	}
}

// TestBuzzTaskBridge_Dispatch_ChatStore_CapsAtTenPriorMessages verifies the
// history-replay window matches handleChatSend's own 10-message cap
// (architecture.md's RQ1 resolution: no separate dormancy mechanism, the
// conversation "naturally" fades past this window).
func TestBuzzTaskBridge_Dispatch_ChatStore_CapsAtTenPriorMessages(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	board := &fakeBuzzBoardStore{}
	cs := &fakeBuzzChatStore{}
	bridge := orchestrator.NewBuzzTaskBridgeWithChatStore(d, board, nil, cs)
	ctx := context.Background()

	// 12 prior turns, oldest ("turn-0") should be excluded from the replay.
	for i := 0; i < 12; i++ {
		if _, err := bridge.Dispatch(ctx, "architect", "", "thread-1", "turn-"+strconv.Itoa(i)); err != nil {
			t.Fatalf("dispatch turn %d: %v", i, err)
		}
	}
	// 13th turn: build its instruction and inspect what got replayed.
	if _, err := bridge.Dispatch(ctx, "architect", "", "thread-1", "turn-12"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := d.calls[len(d.calls)-1].Instruction
	if strings.Contains(last, "turn-0\n") || strings.Contains(last, "turn-0:") {
		t.Fatalf("expected the oldest prior message (turn-0) to fall outside the 10-message cap, got %q", last)
	}
	if !strings.Contains(last, "turn-11") {
		t.Fatalf("expected the most recent prior message (turn-11) within the cap, got %q", last)
	}
}
