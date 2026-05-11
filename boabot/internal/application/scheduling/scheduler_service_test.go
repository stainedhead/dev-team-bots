package scheduling_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/scheduling"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// --- mock DirectTaskStore ---

type mockDirectTaskStore struct {
	mu sync.Mutex

	// ListDue returns these tasks.
	listDueResult []domain.DirectTask
	listDueErr    error

	// ClaimDue controls per-ID claim results.
	claimResults map[string]bool
	claimErr     error

	// UpdateCalls records every Update call.
	UpdateCalls []domain.DirectTask
	updateErr   error

	// other methods are no-ops.
}

func newMockStore() *mockDirectTaskStore {
	return &mockDirectTaskStore{
		claimResults: make(map[string]bool),
	}
}

func (m *mockDirectTaskStore) ListDue(_ context.Context, _ time.Time) ([]domain.DirectTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listDueResult, m.listDueErr
}

func (m *mockDirectTaskStore) ClaimDue(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.claimErr != nil {
		return false, m.claimErr
	}
	claimed, ok := m.claimResults[id]
	if !ok {
		return true, nil // default: claim succeeds
	}
	return claimed, nil
}

func (m *mockDirectTaskStore) Update(_ context.Context, task domain.DirectTask) (domain.DirectTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return domain.DirectTask{}, m.updateErr
	}
	m.UpdateCalls = append(m.UpdateCalls, task)
	return task, nil
}

func (m *mockDirectTaskStore) getUpdateCalls() []domain.DirectTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.DirectTask, len(m.UpdateCalls))
	copy(out, m.UpdateCalls)
	return out
}

// Unused interface methods.
func (m *mockDirectTaskStore) Create(_ context.Context, task domain.DirectTask) (domain.DirectTask, error) {
	return task, nil
}
func (m *mockDirectTaskStore) Get(_ context.Context, _ string) (domain.DirectTask, error) {
	return domain.DirectTask{}, errors.New("not implemented")
}
func (m *mockDirectTaskStore) List(_ context.Context, _ string) ([]domain.DirectTask, error) {
	return nil, nil
}
func (m *mockDirectTaskStore) ListAll(_ context.Context) ([]domain.DirectTask, error) {
	return nil, nil
}
func (m *mockDirectTaskStore) ListBySource(_ context.Context, _ domain.DirectTaskSource) ([]domain.DirectTask, error) {
	return nil, nil
}
func (m *mockDirectTaskStore) Delete(_ context.Context, _ string) error { return nil }

// --- mock TaskDispatcher ---

type mockDispatcher struct {
	mu           sync.Mutex
	RunNowCalls  []string
	runNowErr    error
	runNowResult domain.DirectTask
}

func (m *mockDispatcher) RunNow(_ context.Context, id string) (domain.DirectTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RunNowCalls = append(m.RunNowCalls, id)
	if m.runNowErr != nil {
		return domain.DirectTask{}, m.runNowErr
	}
	result := m.runNowResult
	result.ID = id
	return result, nil
}

func (m *mockDispatcher) Dispatch(_ context.Context, _, _ string, _ *time.Time, _ domain.DirectTaskSource, _, _ string) (domain.DirectTask, error) {
	return domain.DirectTask{}, errors.New("not implemented")
}

func (m *mockDispatcher) getRunNowCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.RunNowCalls))
	copy(out, m.RunNowCalls)
	return out
}

// --- helpers ---

func futureTask(id string, nextRunAt time.Time) domain.DirectTask {
	return domain.DirectTask{
		ID:        id,
		BotName:   "bot-a",
		Status:    domain.DirectTaskStatusPending,
		NextRunAt: &nextRunAt,
		Schedule: domain.Schedule{
			Mode:  domain.ScheduleModeFuture,
			RunAt: &nextRunAt,
		},
	}
}

func recurringTask(id string, nextRunAt time.Time) domain.DirectTask {
	dailyRule := domain.RecurrenceRule{
		Frequency: domain.RecurrenceFrequencyDaily,
		TimeOfDay: 9 * time.Hour, // 09:00
	}
	return domain.DirectTask{
		ID:        id,
		BotName:   "bot-a",
		Status:    domain.DirectTaskStatusPending,
		NextRunAt: &nextRunAt,
		Schedule: domain.Schedule{
			Mode: domain.ScheduleModeRecurring,
			Rule: &dailyRule,
		},
	}
}

// --- Tick tests ---

// TestTick_FutureTask_DispatchesOnce verifies that a due Future task is
// dispatched exactly once and NextRunAt is NOT updated.
func TestTick_FutureTask_DispatchesOnce(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Minute) // already past

	store := newMockStore()
	store.listDueResult = []domain.DirectTask{futureTask("task-1", dueAt)}

	disp := &mockDispatcher{}
	svc := scheduling.NewSchedulerService(store, disp)

	if err := svc.Tick(context.Background(), now); err != nil {
		t.Fatalf("Tick returned error: %v", err)
	}

	calls := disp.getRunNowCalls()
	if len(calls) != 1 || calls[0] != "task-1" {
		t.Errorf("expected RunNow called once with task-1, got %v", calls)
	}

	// NextRunAt must NOT be updated for a Future task.
	updates := store.getUpdateCalls()
	if len(updates) != 0 {
		t.Errorf("expected no Update calls for Future task, got %d", len(updates))
	}
}

// TestTick_RecurringTask_DispatchesAndAdvancesNextRunAt verifies that a due
// Recurring task is dispatched and NextRunAt is advanced to the next future slot.
func TestTick_RecurringTask_DispatchesAndAdvancesNextRunAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Minute) // already past

	task := recurringTask("task-2", dueAt)
	store := newMockStore()
	store.listDueResult = []domain.DirectTask{task}

	disp := &mockDispatcher{}
	svc := scheduling.NewSchedulerService(store, disp)

	if err := svc.Tick(context.Background(), now); err != nil {
		t.Fatalf("Tick returned error: %v", err)
	}

	calls := disp.getRunNowCalls()
	if len(calls) != 1 || calls[0] != "task-2" {
		t.Errorf("expected RunNow called once with task-2, got %v", calls)
	}

	// NextRunAt must be updated to a future time.
	updates := store.getUpdateCalls()
	if len(updates) != 1 {
		t.Fatalf("expected 1 Update call, got %d", len(updates))
	}
	if updates[0].NextRunAt == nil {
		t.Fatal("updated task has nil NextRunAt")
	}
	if !updates[0].NextRunAt.After(now) {
		t.Errorf("NextRunAt %v is not after now %v", updates[0].NextRunAt, now)
	}
	// Status should be reset to pending so the next occurrence can be claimed.
	if updates[0].Status != domain.DirectTaskStatusPending {
		t.Errorf("expected status pending after recurring reschedule, got %s", updates[0].Status)
	}
}

// TestTick_NoDueTasks_NoDispatches verifies that Tick is a no-op when there
// are no due tasks.
func TestTick_NoDueTasks_NoDispatches(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	store.listDueResult = nil

	disp := &mockDispatcher{}
	svc := scheduling.NewSchedulerService(store, disp)

	if err := svc.Tick(context.Background(), time.Now()); err != nil {
		t.Fatalf("Tick returned error: %v", err)
	}

	if len(disp.getRunNowCalls()) != 0 {
		t.Errorf("expected no RunNow calls, got %d", len(disp.getRunNowCalls()))
	}
}

// TestTick_AlreadyClaimed_NoReDispatch verifies that if ClaimDue returns false
// (task was already claimed by a concurrent goroutine), RunNow is not called.
func TestTick_AlreadyClaimed_NoReDispatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-time.Minute)

	store := newMockStore()
	store.listDueResult = []domain.DirectTask{futureTask("task-3", dueAt)}
	store.claimResults["task-3"] = false // already claimed

	disp := &mockDispatcher{}
	svc := scheduling.NewSchedulerService(store, disp)

	if err := svc.Tick(context.Background(), now); err != nil {
		t.Fatalf("Tick returned error: %v", err)
	}

	if len(disp.getRunNowCalls()) != 0 {
		t.Errorf("expected no RunNow calls when ClaimDue returns false, got %d", len(disp.getRunNowCalls()))
	}
}

// --- CatchUpMissedRuns tests ---

// TestCatchUpMissedRuns_RecurringTask_DispatchesOnceAdvancesNextRunAt verifies
// that a recurring task missed 3 times is dispatched once and NextRunAt advances
// to the next future occurrence.
func TestCatchUpMissedRuns_RecurringTask_DispatchesOnceAdvancesNextRunAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	// NextRunAt is 3 days in the past — 3 missed daily occurrences.
	missedAt := now.AddDate(0, 0, -3)

	task := recurringTask("task-4", missedAt)
	store := newMockStore()
	store.listDueResult = []domain.DirectTask{task}

	disp := &mockDispatcher{}
	svc := scheduling.NewSchedulerService(store, disp)

	if err := svc.CatchUpMissedRuns(context.Background(), now); err != nil {
		t.Fatalf("CatchUpMissedRuns returned error: %v", err)
	}

	// Exactly one dispatch regardless of how many occurrences were missed.
	calls := disp.getRunNowCalls()
	if len(calls) != 1 || calls[0] != "task-4" {
		t.Errorf("expected RunNow called once with task-4, got %v", calls)
	}

	// NextRunAt must be in the future.
	updates := store.getUpdateCalls()
	if len(updates) != 1 {
		t.Fatalf("expected 1 Update call, got %d", len(updates))
	}
	if updates[0].NextRunAt == nil || !updates[0].NextRunAt.After(now) {
		t.Errorf("NextRunAt should be a future time, got %v", updates[0].NextRunAt)
	}
}

// TestCatchUpMissedRuns_FutureTask_DispatchesOnce verifies that a missed
// Future (one-shot) task is dispatched once with no NextRunAt update.
func TestCatchUpMissedRuns_FutureTask_DispatchesOnce(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(-2 * time.Hour) // missed 2 hours ago

	store := newMockStore()
	store.listDueResult = []domain.DirectTask{futureTask("task-5", dueAt)}

	disp := &mockDispatcher{}
	svc := scheduling.NewSchedulerService(store, disp)

	if err := svc.CatchUpMissedRuns(context.Background(), now); err != nil {
		t.Fatalf("CatchUpMissedRuns returned error: %v", err)
	}

	calls := disp.getRunNowCalls()
	if len(calls) != 1 || calls[0] != "task-5" {
		t.Errorf("expected RunNow called once with task-5, got %v", calls)
	}

	// No Update for one-shot Future task.
	if len(store.getUpdateCalls()) != 0 {
		t.Errorf("expected no Update for Future task, got %d update(s)", len(store.getUpdateCalls()))
	}
}

// TestCatchUpMissedRuns_NoMissedTasks_NoDispatches verifies that
// CatchUpMissedRuns is a no-op when nothing is due.
func TestCatchUpMissedRuns_NoMissedTasks_NoDispatches(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	store.listDueResult = nil

	disp := &mockDispatcher{}
	svc := scheduling.NewSchedulerService(store, disp)

	if err := svc.CatchUpMissedRuns(context.Background(), time.Now()); err != nil {
		t.Fatalf("CatchUpMissedRuns returned error: %v", err)
	}

	if len(disp.getRunNowCalls()) != 0 {
		t.Errorf("expected no RunNow calls, got %d", len(disp.getRunNowCalls()))
	}
}
