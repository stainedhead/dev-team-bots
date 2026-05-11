package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/orchestrator"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// fakeScheduledDispatcher records calls to DispatchWithSchedule for assertions.
type fakeScheduledDispatcher struct {
	calls []dispatchCall
	err   error
}

type dispatchCall struct {
	BotName     string
	Instruction string
	Schedule    domain.Schedule
	ThreadID    string
}

func (f *fakeScheduledDispatcher) DispatchWithSchedule(
	_ context.Context,
	botName, instruction string,
	schedule domain.Schedule,
	_ domain.DirectTaskSource,
	threadID, _, _ string,
) (domain.DirectTask, error) {
	f.calls = append(f.calls, dispatchCall{
		BotName:     botName,
		Instruction: instruction,
		Schedule:    schedule,
		ThreadID:    threadID,
	})
	if f.err != nil {
		return domain.DirectTask{}, f.err
	}
	now := time.Now().UTC()
	return domain.DirectTask{
		ID:        "test-task-id",
		BotName:   botName,
		Schedule:  schedule,
		NextRunAt: schedule.NextRunAt(now),
		CreatedAt: now,
	}, nil
}

// --- DetectAndHandle: task request -------------------------------------------

func TestDetectAndHandle_TaskRequest_ReturnsConfirmationPrompt(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	m := orchestrator.NewChatTaskManager(d)

	msg := "schedule a weekly code review every Monday at 9am for the architect bot"
	resp, handled, err := m.DetectAndHandle(context.Background(), "thread-1", msg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}
	if resp == "" {
		t.Fatal("expected non-empty confirmation prompt")
	}
	if !strings.Contains(resp, "Confirm?") {
		t.Errorf("expected 'Confirm?' in prompt, got: %s", resp)
	}
	if !strings.Contains(resp, "architect") {
		t.Errorf("expected bot name in prompt, got: %s", resp)
	}
	// No task dispatched yet.
	if len(d.calls) != 0 {
		t.Errorf("expected no dispatch calls, got %d", len(d.calls))
	}
}

// --- DetectAndHandle: confirmation after pending ----------------------------

func TestDetectAndHandle_Confirmation_CreatesTask(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	m := orchestrator.NewChatTaskManager(d)

	// Step 1: send task request.
	taskMsg := "schedule a weekly code review every Monday at 9am for the architect bot"
	_, handled, err := m.DetectAndHandle(context.Background(), "thread-1", taskMsg)
	if err != nil || !handled {
		t.Fatalf("expected handled=true err=nil, got handled=%v err=%v", handled, err)
	}

	// Step 2: confirm.
	for _, confirmWord := range []string{"yes", "y", "confirm", "ok", "sure", "do it", "go ahead"} {
		d2 := &fakeScheduledDispatcher{}
		m2 := orchestrator.NewChatTaskManager(d2)
		_, _, _ = m2.DetectAndHandle(context.Background(), "thread-2", taskMsg)

		resp, handled2, err := m2.DetectAndHandle(context.Background(), "thread-2", confirmWord)
		if err != nil {
			t.Errorf("[%s] unexpected error: %v", confirmWord, err)
		}
		if !handled2 {
			t.Errorf("[%s] expected handled=true", confirmWord)
		}
		if !strings.Contains(resp, "Task created") {
			t.Errorf("[%s] expected 'Task created' in response, got: %s", confirmWord, resp)
		}
		if len(d2.calls) != 1 {
			t.Errorf("[%s] expected 1 dispatch call, got %d", confirmWord, len(d2.calls))
		}
	}

	// Verify original manager also dispatched exactly once.
	resp, handled2, err := m.DetectAndHandle(context.Background(), "thread-1", "yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled2 {
		t.Fatal("expected handled=true")
	}
	if !strings.Contains(resp, "Task created") {
		t.Errorf("expected 'Task created' in response, got: %s", resp)
	}
	if len(d.calls) != 1 {
		t.Errorf("expected 1 dispatch call, got %d", len(d.calls))
	}
}

func TestDetectAndHandle_Confirmation_ClearsPending(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	m := orchestrator.NewChatTaskManager(d)

	taskMsg := "schedule a weekly code review every Monday at 9am for the architect bot"
	_, _, _ = m.DetectAndHandle(context.Background(), "thread-1", taskMsg)
	_, _, _ = m.DetectAndHandle(context.Background(), "thread-1", "yes")

	// Sending "yes" again with no pending should return handled=false.
	resp, handled, err := m.DetectAndHandle(context.Background(), "thread-1", "yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Errorf("expected handled=false after pending cleared, got resp=%s", resp)
	}
}

// --- DetectAndHandle: cancellation ------------------------------------------

func TestDetectAndHandle_Cancellation_ClearsPending(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	m := orchestrator.NewChatTaskManager(d)

	taskMsg := "schedule a weekly code review every Monday at 9am for the architect bot"
	_, _, _ = m.DetectAndHandle(context.Background(), "thread-1", taskMsg)

	for _, cancelWord := range []string{"no", "cancel", "nevermind", "stop"} {
		d2 := &fakeScheduledDispatcher{}
		m2 := orchestrator.NewChatTaskManager(d2)
		_, _, _ = m2.DetectAndHandle(context.Background(), "thread-3", taskMsg)

		resp, handled, err := m2.DetectAndHandle(context.Background(), "thread-3", cancelWord)
		if err != nil {
			t.Errorf("[%s] unexpected error: %v", cancelWord, err)
		}
		if !handled {
			t.Errorf("[%s] expected handled=true", cancelWord)
		}
		if resp == "" {
			t.Errorf("[%s] expected non-empty cancellation message", cancelWord)
		}
		if len(d2.calls) != 0 {
			t.Errorf("[%s] expected 0 dispatch calls, got %d", cancelWord, len(d2.calls))
		}
	}
}

// --- DetectAndHandle: unrelated message -------------------------------------

func TestDetectAndHandle_UnrelatedMessage_NotHandled(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	m := orchestrator.NewChatTaskManager(d)

	resp, handled, err := m.DetectAndHandle(context.Background(), "thread-1", "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Errorf("expected handled=false, got resp=%s", resp)
	}
	if resp != "" {
		t.Errorf("expected empty response, got: %s", resp)
	}
}

// --- DetectAndHandle: "yes" with no pending ---------------------------------

func TestDetectAndHandle_YesWithNoPending_NotHandled(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	m := orchestrator.NewChatTaskManager(d)

	resp, handled, err := m.DetectAndHandle(context.Background(), "thread-1", "yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Errorf("expected handled=false, got resp=%s", resp)
	}
}

// --- Schedule parsing tests -------------------------------------------------

func TestParseScheduleNL_WeeklyMonWed_9am(t *testing.T) {
	schedule := orchestrator.ParseScheduleNL("every Monday and Wednesday at 9am")

	if schedule.Mode != domain.ScheduleModeRecurring {
		t.Fatalf("expected Recurring, got %q", schedule.Mode)
	}
	if schedule.Rule == nil {
		t.Fatal("expected Rule to be set")
	}
	if schedule.Rule.Frequency != domain.RecurrenceFrequencyWeekly {
		t.Errorf("expected Weekly, got %q", schedule.Rule.Frequency)
	}

	// Mon=bit1 (2), Wed=bit3 (8) — mask 0b0001010 = 10
	wantMask := uint8(1<<uint(time.Monday) | 1<<uint(time.Wednesday))
	if schedule.Rule.DaysMask != wantMask {
		t.Errorf("expected DaysMask=0b%08b (%d), got 0b%08b (%d)",
			wantMask, wantMask, schedule.Rule.DaysMask, schedule.Rule.DaysMask)
	}

	wantTimeOfDay := 9 * time.Hour
	if schedule.Rule.TimeOfDay != wantTimeOfDay {
		t.Errorf("expected TimeOfDay=%v, got %v", wantTimeOfDay, schedule.Rule.TimeOfDay)
	}
}

func TestParseScheduleNL_Daily_8am(t *testing.T) {
	schedule := orchestrator.ParseScheduleNL("every day at 8am")

	if schedule.Mode != domain.ScheduleModeRecurring {
		t.Fatalf("expected Recurring, got %q", schedule.Mode)
	}
	if schedule.Rule == nil {
		t.Fatal("expected Rule to be set")
	}
	if schedule.Rule.Frequency != domain.RecurrenceFrequencyDaily {
		t.Errorf("expected Daily, got %q", schedule.Rule.Frequency)
	}
	wantTimeOfDay := 8 * time.Hour
	if schedule.Rule.TimeOfDay != wantTimeOfDay {
		t.Errorf("expected TimeOfDay=%v, got %v", wantTimeOfDay, schedule.Rule.TimeOfDay)
	}
}

func TestParseScheduleNL_Monthly_15th(t *testing.T) {
	schedule := orchestrator.ParseScheduleNL("monthly on the 15th")

	if schedule.Mode != domain.ScheduleModeRecurring {
		t.Fatalf("expected Recurring, got %q", schedule.Mode)
	}
	if schedule.Rule == nil {
		t.Fatal("expected Rule to be set")
	}
	if schedule.Rule.Frequency != domain.RecurrenceFrequencyMonthly {
		t.Errorf("expected Monthly, got %q", schedule.Rule.Frequency)
	}
	if schedule.Rule.MonthDay != 15 {
		t.Errorf("expected MonthDay=15, got %d", schedule.Rule.MonthDay)
	}
}

func TestParseScheduleNL_Fallback_ASAP(t *testing.T) {
	schedule := orchestrator.ParseScheduleNL("do the thing")

	if schedule.Mode != domain.ScheduleModeASAP {
		t.Errorf("expected ASAP fallback, got %q", schedule.Mode)
	}
}

// --- Thread isolation -------------------------------------------------------

func TestDetectAndHandle_DifferentThreads_Isolated(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	m := orchestrator.NewChatTaskManager(d)

	taskMsg := "schedule a daily standup every day at 9am for the architect bot"

	// Pending in thread-A.
	_, handled, err := m.DetectAndHandle(context.Background(), "thread-A", taskMsg)
	if err != nil || !handled {
		t.Fatalf("expected handled=true err=nil, got handled=%v err=%v", handled, err)
	}

	// "yes" on thread-B (no pending) should not dispatch.
	resp, handled2, err := m.DetectAndHandle(context.Background(), "thread-B", "yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled2 {
		t.Errorf("expected handled=false for thread-B, got resp=%s", resp)
	}
	if len(d.calls) != 0 {
		t.Errorf("expected 0 dispatch calls, got %d", len(d.calls))
	}
}

// --- ASAP schedule (no time keyword) ----------------------------------------

// TestDetectAndHandle_ASAPDispatch_SuccessMessageHasASAP verifies that when a
// task is confirmed with an ASAP schedule, the success message contains "ASAP"
// and not a formatted timestamp.
func TestDetectAndHandle_ASAPDispatch_SuccessMessageHasASAP(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	m := orchestrator.NewChatTaskManager(d)

	// "create task" = action, "architect" = known bot keyword → 2 hits, no time.
	taskMsg := "create task for the architect bot"
	_, handled, err := m.DetectAndHandle(context.Background(), "thread-asap", taskMsg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true")
	}

	resp, handled2, err := m.DetectAndHandle(context.Background(), "thread-asap", "yes")
	if err != nil {
		t.Fatalf("unexpected error on confirm: %v", err)
	}
	if !handled2 {
		t.Fatal("expected handled=true on confirm")
	}
	if !strings.Contains(resp, "ASAP") {
		t.Errorf("expected 'ASAP' in success message for ASAP schedule, got: %s", resp)
	}
}

// --- "at N" time detection ---------------------------------------------------

// TestDetectAndHandle_AtTimeKeyword_Detected verifies that " at N" (digit
// after "at") triggers time detection even without standard recurrence words.
func TestDetectAndHandle_AtTimeKeyword_Detected(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	m := orchestrator.NewChatTaskManager(d)

	// No "every"/"daily"/day-name — relies on " at 2pm" digit detection.
	msg := "schedule a task at 2pm for the architect bot"
	_, handled, err := m.DetectAndHandle(context.Background(), "thread-at", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Errorf("expected handled=true when ' at N' present, got false")
	}
}

// --- "for" without "the" bot name extraction ---------------------------------

// TestDetectAndHandle_ForWithoutThe_ExtractsBotName verifies that
// extractBotNameFromFor handles the " for " pattern (no "the").
func TestDetectAndHandle_ForWithoutThe_ExtractsBotName(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	m := orchestrator.NewChatTaskManager(d)

	// "add task" = action, "bot" present, " for " (no "for the") → bot name extraction
	msg := "add task run reports for reporting bot"
	_, handled, err := m.DetectAndHandle(context.Background(), "thread-for", msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Two signals: action + bot. Should be detected.
	if !handled {
		t.Errorf("expected handled=true, got false (resp would be empty)")
	}
}

// --- ParseScheduleNL: HH:MM format ------------------------------------------

// TestParseScheduleNL_HHMMFormat verifies that "at 09:30" is parsed as a
// 9h30m TimeOfDay on a recurring daily rule.
func TestParseScheduleNL_HHMMFormat(t *testing.T) {
	schedule := orchestrator.ParseScheduleNL("daily at 09:30")

	if schedule.Mode != domain.ScheduleModeRecurring {
		t.Fatalf("expected Recurring, got %q", schedule.Mode)
	}
	if schedule.Rule == nil {
		t.Fatal("expected Rule to be set")
	}
	wantTimeOfDay := 9*time.Hour + 30*time.Minute
	if schedule.Rule.TimeOfDay != wantTimeOfDay {
		t.Errorf("expected TimeOfDay=%v, got %v", wantTimeOfDay, schedule.Rule.TimeOfDay)
	}
}

// --- TTL: expired pending intent (FR-010) ------------------------------------

// TestDetectAndHandle_ExpiredIntent_NotConfirmable verifies that a pending
// ChatTaskIntent older than the TTL is treated as absent — "yes" is not handled.
func TestDetectAndHandle_ExpiredIntent_NotConfirmable(t *testing.T) {
	d := &fakeScheduledDispatcher{}
	// Create manager with a very short TTL (1ms) so intents expire immediately.
	m := orchestrator.NewChatTaskManagerWithTTL(d, time.Millisecond)

	taskMsg := "schedule a weekly code review every Monday at 9am for the architect bot"
	_, handled, err := m.DetectAndHandle(context.Background(), "thread-ttl", taskMsg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true for task request")
	}

	// Wait for TTL to expire.
	time.Sleep(5 * time.Millisecond)

	// "yes" with expired intent should not be confirmable.
	_, handled2, err2 := m.DetectAndHandle(context.Background(), "thread-ttl", "yes")
	if err2 != nil {
		t.Fatalf("unexpected error on confirmation: %v", err2)
	}
	if handled2 {
		t.Error("expected handled=false for expired intent, got true")
	}
	if len(d.calls) != 0 {
		t.Errorf("expected 0 dispatch calls for expired intent, got %d", len(d.calls))
	}
}
