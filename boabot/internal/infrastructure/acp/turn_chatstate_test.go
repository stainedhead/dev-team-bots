package acp

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/coder/acp-go-sdk"
	apporchestrator "github.com/stainedhead/dev-team-bots/boabot/internal/application/orchestrator"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	orchestratorlocal "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
)

// newChatStateAgent builds an Agent with FR-503 (ChatStore), FR-504/504a
// (DirectTaskStore, BoardStore, ChatTaskManager) all wired -- the shape
// buildACPAgent constructs in production once a persona's config activates
// them, mirroring buildACPMCPOptions' construction pattern for board/plugin
// stores in acp.go.
func newChatStateAgent(t *testing.T, worker domain.Worker) (a *Agent, chatStore *orchestratorlocal.InMemoryChatStore, taskStore *orchestratorlocal.InMemoryDirectTaskStore, board *orchestratorlocal.InMemoryBoardStore) {
	t.Helper()
	chatStore = orchestratorlocal.NewInMemoryChatStore("")
	taskStore = orchestratorlocal.NewInMemoryDirectTaskStore("")
	board = orchestratorlocal.NewInMemoryBoardStore("")
	dispatcher := orchestratorlocal.NewLocalTaskDispatcher(taskStore, noImmediateDispatchQueue{}, "test-bot")
	chatTaskManager := apporchestrator.NewChatTaskManager(dispatcher)

	a = New(&fakeWorkerFactory{worker: worker}, "/work",
		WithChatStore(chatStore),
		WithDirectTaskStore(taskStore),
		WithBoardStore(board),
		WithChatTaskManager(chatTaskManager),
		WithBotName("test-bot"),
	)
	a.setUpdater(&fakeConn{})
	return a, chatStore, taskStore, board
}

// TestAgent_Prompt_HistoryReplay_SecondTurnIncludesFirst is FR-503's
// end-to-end acceptance test: a follow-up question in the same conversation
// must reach the Worker with prior-turn context, mirroring
// BuzzTaskBridge.buildInstructionWithHistory's exact replay pattern. Both
// turns share the same buzz-acp [Context] channel block so extractChannelID
// resolves them to the same threadID.
func TestAgent_Prompt_HistoryReplay_SecondTurnIncludesFirst(t *testing.T) {
	fw := &fakeWorker{result: domain.TaskResult{Output: "second reply", Success: true}}
	a, _, _, _ := newChatStateAgent(t, fw)
	sid := newSessionForTest(t, a)

	channelBlock := "Channel: general (#3a6a69fc-05a5-55cb-a601-0e12afc77c07)\n"
	first := channelBlock + "What is the weather?"
	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock(first)}}); err != nil {
		t.Fatalf("first Prompt: %v", err)
	}

	second := channelBlock + "And tomorrow?"
	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock(second)}}); err != nil {
		t.Fatalf("second Prompt: %v", err)
	}

	if !strings.Contains(fw.receivedTask.Instruction, "What is the weather?") {
		t.Errorf("second turn's Instruction = %q, want it to include the first turn's message as prior history", fw.receivedTask.Instruction)
	}
	if !strings.Contains(fw.receivedTask.Instruction, "And tomorrow?") {
		t.Errorf("second turn's Instruction = %q, want it to include the current message", fw.receivedTask.Instruction)
	}
}

// TestAgent_Prompt_EveryTaskCreatesDirectTaskAndBoardItem is FR-504a's
// acceptance test: every ACP-dispatched task -- not just scheduled ones --
// creates a real, bot_name-tagged DirectTask and Kanban board item, visible
// live in the dashboard, matching native-mode Buzz's existing automatic
// behavior (BuzzTaskBridge.createBoardItem).
func TestAgent_Prompt_EveryTaskCreatesDirectTaskAndBoardItem(t *testing.T) {
	fw := &fakeWorker{result: domain.TaskResult{Output: "done", Success: true}}
	a, _, taskStore, board := newChatStateAgent(t, fw)
	sid := newSessionForTest(t, a)

	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock("please do the thing")}}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	tasks, err := taskStore.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 DirectTask to be recorded, got %d: %+v", len(tasks), tasks)
	}
	if tasks[0].Source != domain.DirectTaskSourceACP {
		t.Errorf("Source = %q, want %q", tasks[0].Source, domain.DirectTaskSourceACP)
	}
	if tasks[0].BotName != "test-bot" {
		t.Errorf("BotName = %q, want %q", tasks[0].BotName, "test-bot")
	}
	if tasks[0].Status != domain.DirectTaskStatusSucceeded {
		t.Errorf("Status = %q, want %q", tasks[0].Status, domain.DirectTaskStatusSucceeded)
	}
	if tasks[0].Output != "done" {
		t.Errorf("Output = %q, want %q", tasks[0].Output, "done")
	}

	items, err := board.List(context.Background(), domain.WorkItemFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 board item, got %d: %+v", len(items), items)
	}
	if items[0].ActiveTaskID != tasks[0].ID {
		t.Errorf("board item ActiveTaskID = %q, want it to link to the DirectTask %q", items[0].ActiveTaskID, tasks[0].ID)
	}
}

// TestAgent_Prompt_FailedTaskRecordsFailedStatus verifies FR-504a's record
// reflects real outcomes, not just successes -- an operator watching the
// board must be able to tell a stuck/failed ACP task apart from a
// successful one.
func TestAgent_Prompt_FailedTaskRecordsFailedStatus(t *testing.T) {
	fw := &fakeWorker{result: domain.TaskResult{Success: false, Err: context.DeadlineExceeded}}
	a, _, taskStore, _ := newChatStateAgent(t, fw)
	sid := newSessionForTest(t, a)

	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock("do the thing")}}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	tasks, _ := taskStore.ListAll(context.Background())
	if len(tasks) != 1 {
		t.Fatalf("expected 1 DirectTask, got %d", len(tasks))
	}
	if tasks[0].Status != domain.DirectTaskStatusFailed {
		t.Errorf("Status = %q, want %q", tasks[0].Status, domain.DirectTaskStatusFailed)
	}
}

// TestAgent_Prompt_SchedulingConfirmation_CreatesRecurringTask is FR-504's
// acceptance test: a recurring instruction, confirmed, creates a real
// Schedule/RecurrenceRule-backed DirectTask via the existing
// ChatTaskManager/DispatchWithSchedule flow -- and the confirmation itself
// does not invoke the Worker at all (the request is scheduled, not run now).
func TestAgent_Prompt_SchedulingConfirmation_CreatesRecurringTask(t *testing.T) {
	fw := &fakeWorker{result: domain.TaskResult{Output: "should not run", Success: true}}
	a, _, taskStore, _ := newChatStateAgent(t, fw)
	sid := newSessionForTest(t, a)

	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock("schedule a status report every day at 9am")}}); err != nil {
		t.Fatalf("first Prompt: %v", err)
	}
	resp, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock("yes")}})
	if err != nil {
		t.Fatalf("confirmation Prompt: %v", err)
	}
	if resp.StopReason != sdk.StopReasonEndTurn {
		t.Errorf("StopReason = %v, want %v", resp.StopReason, sdk.StopReasonEndTurn)
	}

	tasks, err := taskStore.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected exactly 1 scheduled DirectTask, got %d: %+v", len(tasks), tasks)
	}
	if tasks[0].Schedule.Mode != domain.ScheduleModeRecurring {
		t.Errorf("Schedule.Mode = %q, want %q", tasks[0].Schedule.Mode, domain.ScheduleModeRecurring)
	}
	if tasks[0].BotName != "test-bot" {
		t.Errorf("BotName = %q, want %q (ACP mode is single-persona -- the parsed NL bot name must not override it)", tasks[0].BotName, "test-bot")
	}
	if fw.receivedTask.ID != "" {
		t.Error("expected worker.Execute NOT to be called for a scheduling confirmation, but it was")
	}
}

// TestAgent_Prompt_SchedulingConfirmation_ImmediateModeDeclinesGracefully
// covers the one case ChatTaskManager's shared heuristic parser can produce
// that ACP mode's single-persona model cannot fulfil: a confirmed intent
// with no time/recurrence signal resolves to an ASAP schedule, which native
// mode's LocalTaskDispatcher would normally route to a DIFFERENT bot's
// message queue for delegation -- a multi-bot concept ACP mode does not
// have (architecture.md AD-1/AD-2, this feature's Non-Goals). The turn must
// still end normally with a clear reply, not a hard failure -- see
// noImmediateDispatchQueue. LocalTaskDispatcher.DispatchWithSchedule
// persists the DirectTask before attempting delivery (task_dispatcher.go),
// so the record survives marked Failed -- correct, honest observability
// (an operator sees why it didn't run), not nothing at all.
func TestAgent_Prompt_SchedulingConfirmation_ImmediateModeDeclinesGracefully(t *testing.T) {
	fw := &fakeWorker{result: domain.TaskResult{Output: "should not run", Success: true}}
	a, _, taskStore, _ := newChatStateAgent(t, fw)
	sid := newSessionForTest(t, a)

	if _, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock("create task for the architect")}}); err != nil {
		t.Fatalf("first Prompt: %v", err)
	}
	resp, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock("yes")}})
	if err != nil {
		t.Fatalf("confirmation Prompt returned a protocol error (must degrade gracefully instead): %v", err)
	}
	if resp.StopReason != sdk.StopReasonEndTurn {
		t.Errorf("StopReason = %v, want %v (graceful decline, not a hard failure)", resp.StopReason, sdk.StopReasonEndTurn)
	}

	tasks, _ := taskStore.ListAll(context.Background())
	if len(tasks) != 1 {
		t.Fatalf("expected the DirectTask record to survive (marked failed), got %d: %+v", len(tasks), tasks)
	}
	if tasks[0].Status != domain.DirectTaskStatusFailed {
		t.Errorf("Status = %q, want %q (delegation is unsupported, not silently dropped)", tasks[0].Status, domain.DirectTaskStatusFailed)
	}
	if fw.receivedTask.ID != "" {
		t.Error("expected worker.Execute NOT to be called for a declined immediate-delegation confirmation, but it was")
	}
}

// TestAgent_Prompt_NilChatState_BehavesAsBeforeThisFeature guards NFR-Correctness:
// a persona with none of FR-503/504/504a wired (nil chatStore/taskStore/
// board/chatTaskManager -- e.g. the tech-lead gate already used for the
// board store, architecture.md) must behave exactly as before this feature.
func TestAgent_Prompt_NilChatState_BehavesAsBeforeThisFeature(t *testing.T) {
	fw := &fakeWorker{result: domain.TaskResult{Output: "plain reply", Success: true}}
	a := New(&fakeWorkerFactory{worker: fw}, "/work")
	a.setUpdater(&fakeConn{})
	sid := newSessionForTest(t, a)

	resp, err := a.Prompt(context.Background(), sdk.PromptRequest{SessionId: sid, Prompt: []sdk.ContentBlock{sdk.TextBlock("hello")}})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.StopReason != sdk.StopReasonEndTurn {
		t.Errorf("StopReason = %v, want %v", resp.StopReason, sdk.StopReasonEndTurn)
	}
	if fw.receivedTask.Instruction != "hello" {
		t.Errorf("Instruction = %q, want unaugmented %q", fw.receivedTask.Instruction, "hello")
	}
}
