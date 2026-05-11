package orchestrator_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
)

// ── mock queue ────────────────────────────────────────────────────────────────

type mockQueue struct {
	mu      sync.Mutex
	sent    []sentMsg
	sendErr error
}

type sentMsg struct {
	queueURL string
	msg      domain.Message
}

func (m *mockQueue) Send(_ context.Context, queueURL string, msg domain.Message) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.mu.Lock()
	m.sent = append(m.sent, sentMsg{queueURL: queueURL, msg: msg})
	m.mu.Unlock()
	return nil
}

func (m *mockQueue) Receive(_ context.Context) ([]domain.ReceivedMessage, error) {
	return nil, nil
}

func (m *mockQueue) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockQueue) getSent() []sentMsg {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]sentMsg, len(m.sent))
	copy(out, m.sent)
	return out
}

// ── tests ──────────────────────────────────────────────────────────────────────

func TestLocalTaskDispatcher_Dispatch_Immediate(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore("")
	q := &mockQueue{}
	dispatcher := orchestrator.NewLocalTaskDispatcher(store, q, "orchestrator")
	ctx := context.Background()

	task, err := dispatcher.Dispatch(ctx, "dev-1", "write unit tests", nil, domain.DirectTaskSourceOperator, "", "")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// Returned task should be dispatched immediately.
	if task.Status != domain.DirectTaskStatusRunning {
		t.Errorf("expected status=dispatched, got %q", task.Status)
	}
	if task.DispatchedAt == nil {
		t.Error("expected DispatchedAt to be set")
	}
	if task.BotName != "dev-1" {
		t.Errorf("expected BotName=dev-1, got %q", task.BotName)
	}
	if task.Instruction != "write unit tests" {
		t.Errorf("expected Instruction set, got %q", task.Instruction)
	}

	// Queue should have received exactly one message to dev-1.
	sent := q.getSent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 message sent, got %d", len(sent))
	}
	if sent[0].queueURL != "dev-1" {
		t.Errorf("expected message sent to dev-1, got %q", sent[0].queueURL)
	}
	if sent[0].msg.Type != domain.MessageTypeTask {
		t.Errorf("expected message type %q, got %q", domain.MessageTypeTask, sent[0].msg.Type)
	}

	// Store should reflect dispatched status.
	stored, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Status != domain.DirectTaskStatusRunning {
		t.Errorf("stored task status: got %q, want dispatched", stored.Status)
	}
}

func TestLocalTaskDispatcher_Dispatch_ImmediateForPastScheduledAt(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore("")
	q := &mockQueue{}
	dispatcher := orchestrator.NewLocalTaskDispatcher(store, q, "orchestrator")
	ctx := context.Background()

	past := time.Now().Add(-5 * time.Minute)
	task, err := dispatcher.Dispatch(ctx, "dev-1", "do something", &past, domain.DirectTaskSourceOperator, "", "")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if task.Status != domain.DirectTaskStatusRunning {
		t.Errorf("expected dispatched for past scheduledAt, got %q", task.Status)
	}
	if len(q.getSent()) != 1 {
		t.Errorf("expected 1 message sent, got %d", len(q.getSent()))
	}
}

func TestLocalTaskDispatcher_Dispatch_Scheduled(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore("")
	q := &mockQueue{}
	dispatcher := orchestrator.NewLocalTaskDispatcher(store, q, "orchestrator")
	ctx := context.Background()

	future := time.Now().Add(100 * time.Millisecond)
	task, err := dispatcher.Dispatch(ctx, "dev-1", "scheduled work", &future, domain.DirectTaskSourceOperator, "", "")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// Immediately after dispatch, status should be pending.
	if task.Status != domain.DirectTaskStatusPending {
		t.Errorf("expected status=pending for future task, got %q", task.Status)
	}
	if len(q.getSent()) != 0 {
		t.Errorf("expected 0 messages sent immediately, got %d", len(q.getSent()))
	}

	// Wait for the goroutine to fire after 100ms.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		if len(q.getSent()) > 0 {
			break
		}
	}

	sent := q.getSent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 message sent after scheduled time, got %d", len(sent))
	}
	if sent[0].queueURL != "dev-1" {
		t.Errorf("expected message to dev-1, got %q", sent[0].queueURL)
	}

	// Store should now show dispatched.
	stored, _ := store.Get(ctx, task.ID)
	if stored.Status != domain.DirectTaskStatusRunning {
		t.Errorf("stored status after dispatch: got %q, want dispatched", stored.Status)
	}
}

func TestLocalTaskDispatcher_Dispatch_MessageContainsInstruction(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore("")
	q := &mockQueue{}
	dispatcher := orchestrator.NewLocalTaskDispatcher(store, q, "orchestrator")
	ctx := context.Background()

	instruction := "implement feature X"
	task, err := dispatcher.Dispatch(ctx, "dev-1", instruction, nil, domain.DirectTaskSourceOperator, "", "")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	sent := q.getSent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sent))
	}

	// The payload should contain the task ID and instruction encoded as JSON.
	payload := string(sent[0].msg.Payload)
	if payload == "" {
		t.Fatal("expected non-empty payload")
	}
	if sent[0].msg.From != "orchestrator" {
		t.Errorf("expected From=orchestrator, got %q", sent[0].msg.From)
	}
	_ = task
}

func TestLocalTaskDispatcher_Dispatch_StoreIsUpdated(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore("")
	q := &mockQueue{}
	dispatcher := orchestrator.NewLocalTaskDispatcher(store, q, "orchestrator")
	ctx := context.Background()

	task, err := dispatcher.Dispatch(ctx, "dev-1", "check logs", nil, domain.DirectTaskSourceOperator, "", "")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// Verify the store has the task.
	all, _ := store.ListAll(ctx)
	if len(all) != 1 {
		t.Fatalf("expected 1 task in store, got %d", len(all))
	}
	if all[0].ID != task.ID {
		t.Errorf("stored ID mismatch: got %q, want %q", all[0].ID, task.ID)
	}
}

// --- Dispatch Schedule population tests ---

func TestLocalTaskDispatcher_Dispatch_NilScheduledAt_SetsASAP(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore("")
	q := &mockQueue{}
	dispatcher := orchestrator.NewLocalTaskDispatcher(store, q, "orchestrator")
	ctx := context.Background()

	task, err := dispatcher.Dispatch(ctx, "dev-1", "do it now", nil, domain.DirectTaskSourceOperator, "", "")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	stored, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Schedule.Mode != domain.ScheduleModeASAP {
		t.Errorf("Schedule.Mode: got %q, want %q", stored.Schedule.Mode, domain.ScheduleModeASAP)
	}
	if stored.NextRunAt != nil {
		t.Errorf("NextRunAt should be nil for ASAP, got %v", stored.NextRunAt)
	}
}

func TestLocalTaskDispatcher_Dispatch_PastScheduledAt_SetsASAP(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore("")
	q := &mockQueue{}
	dispatcher := orchestrator.NewLocalTaskDispatcher(store, q, "orchestrator")
	ctx := context.Background()

	past := time.Now().Add(-10 * time.Minute)
	task, err := dispatcher.Dispatch(ctx, "dev-1", "past task", &past, domain.DirectTaskSourceOperator, "", "")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	stored, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Schedule.Mode != domain.ScheduleModeASAP {
		t.Errorf("Schedule.Mode: got %q, want %q", stored.Schedule.Mode, domain.ScheduleModeASAP)
	}
	if stored.NextRunAt != nil {
		t.Errorf("NextRunAt should be nil for past scheduledAt, got %v", stored.NextRunAt)
	}
}

func TestLocalTaskDispatcher_Dispatch_FutureScheduledAt_SetsFuture(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore("")
	q := &mockQueue{}
	dispatcher := orchestrator.NewLocalTaskDispatcher(store, q, "orchestrator")
	ctx := context.Background()

	future := time.Now().Add(1 * time.Hour)
	task, err := dispatcher.Dispatch(ctx, "dev-1", "future task", &future, domain.DirectTaskSourceOperator, "", "")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	stored, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Schedule.Mode != domain.ScheduleModeFuture {
		t.Errorf("Schedule.Mode: got %q, want %q", stored.Schedule.Mode, domain.ScheduleModeFuture)
	}
	if stored.Schedule.RunAt == nil {
		t.Fatal("Schedule.RunAt should be set for Future mode")
	}
	if !stored.Schedule.RunAt.Equal(future) {
		t.Errorf("Schedule.RunAt: got %v, want %v", stored.Schedule.RunAt, future)
	}
	if stored.NextRunAt == nil {
		t.Fatal("NextRunAt should be set for Future task")
	}
	if !stored.NextRunAt.Equal(future) {
		t.Errorf("NextRunAt: got %v, want %v", stored.NextRunAt, future)
	}
}

// --- DispatchWithSchedule tests ---

func TestLocalTaskDispatcher_DispatchWithSchedule_ASAP(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore("")
	q := &mockQueue{}
	dispatcher := orchestrator.NewLocalTaskDispatcher(store, q, "orchestrator")
	ctx := context.Background()

	sched := domain.Schedule{Mode: domain.ScheduleModeASAP}
	task, err := dispatcher.DispatchWithSchedule(ctx, "dev-1", "asap work", sched,
		domain.DirectTaskSourceOperator, "", "", "My Task")
	if err != nil {
		t.Fatalf("DispatchWithSchedule: %v", err)
	}

	if task.Status != domain.DirectTaskStatusRunning {
		t.Errorf("expected running for ASAP, got %q", task.Status)
	}
	if task.Title != "My Task" {
		t.Errorf("Title: got %q, want My Task", task.Title)
	}
	if len(q.getSent()) != 1 {
		t.Errorf("expected 1 message sent for ASAP, got %d", len(q.getSent()))
	}
}

func TestLocalTaskDispatcher_DispatchWithSchedule_FuturePast_ASAP(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore("")
	q := &mockQueue{}
	dispatcher := orchestrator.NewLocalTaskDispatcher(store, q, "orchestrator")
	ctx := context.Background()

	pastTime := time.Now().Add(-5 * time.Minute)
	sched := domain.Schedule{Mode: domain.ScheduleModeFuture, RunAt: &pastTime}
	task, err := dispatcher.DispatchWithSchedule(ctx, "dev-1", "past future", sched,
		domain.DirectTaskSourceOperator, "", "", "")
	if err != nil {
		t.Fatalf("DispatchWithSchedule: %v", err)
	}

	// Past Future should dispatch immediately.
	if task.Status != domain.DirectTaskStatusRunning {
		t.Errorf("expected running for past Future, got %q", task.Status)
	}
	if len(q.getSent()) != 1 {
		t.Errorf("expected 1 message, got %d", len(q.getSent()))
	}
}

func TestLocalTaskDispatcher_DispatchWithSchedule_FutureFuture_Pending(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore("")
	q := &mockQueue{}
	dispatcher := orchestrator.NewLocalTaskDispatcher(store, q, "orchestrator")
	ctx := context.Background()

	futureTime := time.Now().Add(1 * time.Hour)
	sched := domain.Schedule{Mode: domain.ScheduleModeFuture, RunAt: &futureTime}
	task, err := dispatcher.DispatchWithSchedule(ctx, "dev-1", "future task", sched,
		domain.DirectTaskSourceOperator, "", "", "")
	if err != nil {
		t.Fatalf("DispatchWithSchedule: %v", err)
	}

	if task.Status != domain.DirectTaskStatusPending {
		t.Errorf("expected pending for future Future, got %q", task.Status)
	}
	if task.NextRunAt == nil {
		t.Fatal("NextRunAt should be set")
	}
	if !task.NextRunAt.Equal(futureTime) {
		t.Errorf("NextRunAt: got %v, want %v", task.NextRunAt, futureTime)
	}
	if len(q.getSent()) != 0 {
		t.Errorf("expected 0 messages for future Future, got %d", len(q.getSent()))
	}
}

func TestLocalTaskDispatcher_DispatchWithSchedule_Recurring_Pending(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore("")
	q := &mockQueue{}
	dispatcher := orchestrator.NewLocalTaskDispatcher(store, q, "orchestrator")
	ctx := context.Background()

	rule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyDaily,
		TimeOfDay: 9 * time.Hour,
	}
	sched := domain.Schedule{Mode: domain.ScheduleModeRecurring, Rule: &rule}
	task, err := dispatcher.DispatchWithSchedule(ctx, "dev-1", "daily standup", sched,
		domain.DirectTaskSourceOperator, "", "", "Daily Standup")
	if err != nil {
		t.Fatalf("DispatchWithSchedule: %v", err)
	}

	if task.Status != domain.DirectTaskStatusPending {
		t.Errorf("expected pending for Recurring, got %q", task.Status)
	}
	if task.NextRunAt == nil {
		t.Fatal("NextRunAt should be set for Recurring task")
	}
	if !task.NextRunAt.After(time.Now().UTC().Add(-time.Second)) {
		t.Errorf("NextRunAt %v should be a future time", task.NextRunAt)
	}
	if task.Schedule.Mode != domain.ScheduleModeRecurring {
		t.Errorf("Schedule.Mode: got %q, want %q", task.Schedule.Mode, domain.ScheduleModeRecurring)
	}
	if len(q.getSent()) != 0 {
		t.Errorf("expected 0 messages for Recurring, got %d", len(q.getSent()))
	}
	if task.Title != "Daily Standup" {
		t.Errorf("Title: got %q, want Daily Standup", task.Title)
	}
}
