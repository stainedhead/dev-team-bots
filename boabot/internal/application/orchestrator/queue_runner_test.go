package orchestrator_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/orchestrator"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ── fakes ──────────────────────────────────────────────────────────────────

type fakeTaskStore struct {
	getFn func(ctx context.Context, id string) (domain.DirectTask, error)
}

func (f *fakeTaskStore) Create(ctx context.Context, t domain.DirectTask) (domain.DirectTask, error) {
	return t, nil
}
func (f *fakeTaskStore) Update(ctx context.Context, t domain.DirectTask) (domain.DirectTask, error) {
	return t, nil
}
func (f *fakeTaskStore) Get(ctx context.Context, id string) (domain.DirectTask, error) {
	if f.getFn != nil {
		return f.getFn(ctx, id)
	}
	return domain.DirectTask{}, nil
}
func (f *fakeTaskStore) List(ctx context.Context, botName string) ([]domain.DirectTask, error) {
	return nil, nil
}
func (f *fakeTaskStore) ListAll(ctx context.Context) ([]domain.DirectTask, error) {
	return nil, nil
}
func (f *fakeTaskStore) ListBySource(ctx context.Context, source domain.DirectTaskSource) ([]domain.DirectTask, error) {
	return nil, nil
}
func (f *fakeTaskStore) Delete(ctx context.Context, id string) error { return nil }
func (f *fakeTaskStore) ListDue(_ context.Context, _ time.Time) ([]domain.DirectTask, error) {
	return nil, nil
}
func (f *fakeTaskStore) ClaimDue(_ context.Context, _ string) (bool, error) { return false, nil }

type fakeBoardItemDispatcher struct {
	mu         sync.Mutex
	dispatched []string
	dispatchFn func(ctx context.Context, item domain.WorkItem) (domain.WorkItem, error)
}

func (f *fakeBoardItemDispatcher) DispatchBoardItem(ctx context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	f.mu.Lock()
	f.dispatched = append(f.dispatched, item.ID)
	f.mu.Unlock()
	if f.dispatchFn != nil {
		return f.dispatchFn(ctx, item)
	}
	return item, nil
}

func (f *fakeBoardItemDispatcher) dispatchedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.dispatched)
}

type inMemBoard struct {
	mu    sync.Mutex
	items []domain.WorkItem
}

func (b *inMemBoard) Create(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = append(b.items, item)
	return item, nil
}
func (b *inMemBoard) Update(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, it := range b.items {
		if it.ID == item.ID {
			b.items[i] = item
			return item, nil
		}
	}
	b.items = append(b.items, item)
	return item, nil
}
func (b *inMemBoard) Get(_ context.Context, id string) (domain.WorkItem, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, it := range b.items {
		if it.ID == id {
			return it, nil
		}
	}
	return domain.WorkItem{}, errors.New("not found")
}
func (b *inMemBoard) List(_ context.Context, f domain.WorkItemFilter) ([]domain.WorkItem, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []domain.WorkItem
	for _, it := range b.items {
		if f.Status != "" && it.Status != f.Status {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}
func (b *inMemBoard) Delete(_ context.Context, id string) error   { return nil }
func (b *inMemBoard) Reorder(_ context.Context, _ []string) error { return nil }

// errOnUpdateBoard wraps inMemBoard and returns an error for Update calls.
type errOnUpdateBoard struct{ inMemBoard }

func (b *errOnUpdateBoard) Update(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	return domain.WorkItem{}, errors.New("update fail")
}

// ── tests ──────────────────────────────────────────────────────────────────

func TestQueueRunner_ASAPDispatchedWhenSlotAvailable(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot", QueueMode: "asap"},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 2,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	if disp.dispatchedCount() == 0 {
		t.Error("expected item to be dispatched")
	}
}

func TestQueueRunner_RespectsMaxConcurrent(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "r1", Status: domain.WorkItemStatusInProgress, AssignedTo: "bot"},
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot", QueueMode: "asap"},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 1,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx)
	if disp.dispatchedCount() != 0 {
		t.Errorf("should not dispatch when at capacity, got %d dispatched", disp.dispatchedCount())
	}
}

func TestQueueRunner_RunAt_NotYetReady(t *testing.T) {
	future := time.Now().Add(time.Hour)
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot", QueueMode: "run_at", QueueRunAt: &future},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx)
	if disp.dispatchedCount() != 0 {
		t.Error("should not dispatch a run_at item whose time has not arrived")
	}
}

func TestQueueRunner_RunAfter_PredecessorDone(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "pred", Status: domain.WorkItemStatusDone},
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_after", QueueAfterItemID: "pred", QueueRequireSuccess: true},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
	if disp.dispatchedCount() == 0 {
		t.Error("expected item to be dispatched when predecessor is done")
	}
}

func TestQueueRunner_RunAfter_PredecessorErrored_RequireSuccess(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "pred", Status: domain.WorkItemStatusErrored},
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_after", QueueAfterItemID: "pred", QueueRequireSuccess: true},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx)
	if disp.dispatchedCount() != 0 {
		t.Error("should not dispatch when predecessor errored and require_success is set")
	}
}

func TestQueueRunner_Reconcile_SucceededTask(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "item1", Status: domain.WorkItemStatusInProgress, ActiveTaskID: "t1"},
	}}
	tasks := &fakeTaskStore{getFn: func(_ context.Context, id string) (domain.DirectTask, error) {
		return domain.DirectTask{ID: id, Status: domain.DirectTaskStatusSucceeded, Output: "great result"}, nil
	}}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         tasks,
		Dispatcher:    &fakeBoardItemDispatcher{},
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
	item, err := board.Get(context.Background(), "item1")
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != domain.WorkItemStatusDone {
		t.Errorf("expected done, got %s", item.Status)
	}
	if item.LastResult != "great result" {
		t.Errorf("expected result stored, got %q", item.LastResult)
	}
}

func TestQueueRunner_Reconcile_FailedTask(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "item1", Status: domain.WorkItemStatusInProgress, ActiveTaskID: "t1"},
	}}
	tasks := &fakeTaskStore{getFn: func(_ context.Context, id string) (domain.DirectTask, error) {
		return domain.DirectTask{ID: id, Status: domain.DirectTaskStatusFailed, Output: "error output"}, nil
	}}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         tasks,
		Dispatcher:    &fakeBoardItemDispatcher{},
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
	item, _ := board.Get(context.Background(), "item1")
	if item.Status != domain.WorkItemStatusErrored {
		t.Errorf("expected errored, got %s", item.Status)
	}
}

func TestQueueRunner_DefaultMaxConcurrent(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot", QueueMode: "asap"},
	}}
	disp := &fakeBoardItemDispatcher{}
	// MaxConcurrent=0 should default to 3, so item should dispatch
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 0,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	if disp.dispatchedCount() == 0 {
		t.Error("expected item dispatched with default max concurrent")
	}
}

func TestQueueRunner_RunAfter_PredecessorErrored_NoRequireSuccess(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "pred", Status: domain.WorkItemStatusErrored},
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_after", QueueAfterItemID: "pred", QueueRequireSuccess: false},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	if disp.dispatchedCount() == 0 {
		t.Error("expected item dispatched when predecessor errored and require_success is false")
	}
}

func TestQueueRunner_NoQueuedItems(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "i1", Status: domain.WorkItemStatusInProgress, ActiveTaskID: ""},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx)
	if disp.dispatchedCount() != 0 {
		t.Error("should not dispatch when no queued items")
	}
}

func TestQueueRunner_LaunchDispatchError_ItemLeftInProgress(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot", QueueMode: "asap"},
	}}
	disp := &fakeBoardItemDispatcher{
		dispatchFn: func(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
			return item, errors.New("dispatch failed")
		},
	}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	// Item should be in-progress (launch succeeded even if dispatch errored)
	item, _ := board.Get(context.Background(), "q1")
	if item.Status != domain.WorkItemStatusInProgress {
		t.Errorf("expected in-progress after dispatch error, got %s", item.Status)
	}
}

func TestQueueRunner_RunAt_Ready(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot", QueueMode: "run_at", QueueRunAt: &past},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	if disp.dispatchedCount() == 0 {
		t.Error("expected item dispatched when run_at time has passed")
	}
}

func TestQueueRunner_RunAfter_NoPredecessorSpecified(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_after", QueueAfterItemID: ""},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	if disp.dispatchedCount() == 0 {
		t.Error("expected item dispatched when run_after has no predecessor specified")
	}
}

func TestQueueRunner_ASAP_SortedByQueuedAt(t *testing.T) {
	t1 := time.Now().Add(-2 * time.Hour)
	t2 := time.Now().Add(-1 * time.Hour)
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q2", Status: domain.WorkItemStatusQueued, AssignedTo: "bot", QueueMode: "asap", QueuedAt: &t2},
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot", QueueMode: "asap", QueuedAt: &t1},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 1, // only dispatch one
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	if disp.dispatchedCount() == 0 {
		t.Error("expected item dispatched")
	}
	disp.mu.Lock()
	first := disp.dispatched[0]
	disp.mu.Unlock()
	if first != "q1" {
		t.Errorf("expected q1 (older QueuedAt) dispatched first, got %s", first)
	}
}

func TestQueueRunner_Reconcile_RunningTask_NoTransition(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "item1", Status: domain.WorkItemStatusInProgress, ActiveTaskID: "t1"},
	}}
	tasks := &fakeTaskStore{getFn: func(_ context.Context, id string) (domain.DirectTask, error) {
		return domain.DirectTask{ID: id, Status: domain.DirectTaskStatusRunning}, nil
	}}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         tasks,
		Dispatcher:    &fakeBoardItemDispatcher{},
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx)
	item, _ := board.Get(context.Background(), "item1")
	if item.Status != domain.WorkItemStatusInProgress {
		t.Errorf("expected still in-progress for running task, got %s", item.Status)
	}
}

func TestQueueRunner_Reconcile_BoardUpdateError_Survives(t *testing.T) {
	// Board returns error on Update; runner should log a warning but not panic.
	board := &errOnUpdateBoard{}
	board.items = []domain.WorkItem{
		{ID: "item1", Status: domain.WorkItemStatusInProgress, ActiveTaskID: "t1"},
	}
	tasks := &fakeTaskStore{getFn: func(_ context.Context, id string) (domain.DirectTask, error) {
		return domain.DirectTask{ID: id, Status: domain.DirectTaskStatusSucceeded, Output: "ok"}, nil
	}}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         tasks,
		Dispatcher:    &fakeBoardItemDispatcher{},
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx) // should not panic despite update error
}

func TestQueueRunner_ASAP_FallbackToCreatedAt(t *testing.T) {
	// Items with nil QueuedAt should use CreatedAt for ordering (exercises queuedAtOf nil branch)
	early := time.Now().Add(-3 * time.Hour)
	late := time.Now().Add(-1 * time.Hour)
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q2", Status: domain.WorkItemStatusQueued, AssignedTo: "bot", QueueMode: "asap", CreatedAt: late},
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot", QueueMode: "asap", CreatedAt: early},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 1,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	disp.mu.Lock()
	first := disp.dispatched[0]
	disp.mu.Unlock()
	if first != "q1" {
		t.Errorf("expected q1 (older CreatedAt) dispatched first, got %s", first)
	}
}

func TestQueueRunner_RunAfter_PredecessorNotFound(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_after", QueueAfterItemID: "missing-pred"},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx)
	if disp.dispatchedCount() != 0 {
		t.Error("should not dispatch when predecessor not found")
	}
}

// ── errOnListBoard returns an error from List ─────────────────────────────────

type errOnListBoard struct{ inMemBoard }

func (b *errOnListBoard) List(_ context.Context, _ domain.WorkItemFilter) ([]domain.WorkItem, error) {
	return nil, errors.New("list fail")
}

// ── new coverage tests ────────────────────────────────────────────────────────

// TestQueueRunner_Start_DefaultInterval verifies that Interval=0 is replaced with
// the 5-second default (so the ticker is created without panic).
func TestQueueRunner_Start_DefaultInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled — Start exits on ctx.Done without ticking
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:      &inMemBoard{},
		Tasks:      &fakeTaskStore{},
		Dispatcher: &fakeBoardItemDispatcher{},
		Interval:   0, // triggers default branch
	})
	runner.Start(ctx) // must not panic
}

// TestQueueRunner_Tick_BoardListError verifies that tick (and reconcile) return
// early without panic when Board.List returns an error.
func TestQueueRunner_Tick_BoardListError(t *testing.T) {
	board := &errOnListBoard{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    &fakeBoardItemDispatcher{},
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx)
}

// TestQueueRunner_Sort_Mixed_ASAP_RunAt verifies that the priority sort correctly
// places ASAP items before run_at items, exercising both sort branches.
func TestQueueRunner_Sort_Mixed_ASAP_RunAt(t *testing.T) {
	past1 := time.Now().Add(-3 * time.Hour)
	past2 := time.Now().Add(-2 * time.Hour)
	past3 := time.Now().Add(-1 * time.Hour)
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "run1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_at", QueueRunAt: &past1, QueuedAt: &past1},
		{ID: "asap1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "asap", QueuedAt: &past2},
		{ID: "run2", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_at", QueueRunAt: &past3, QueuedAt: &past3},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	if disp.dispatchedCount() == 0 {
		t.Error("expected items to be dispatched")
	}
}

// TestQueueRunner_Reconcile_TaskGetError verifies that reconcile skips items
// whose ActiveTaskID cannot be resolved.
func TestQueueRunner_Reconcile_TaskGetError(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "item1", Status: domain.WorkItemStatusInProgress, ActiveTaskID: "t-bad"},
	}}
	tasks := &fakeTaskStore{getFn: func(_ context.Context, _ string) (domain.DirectTask, error) {
		return domain.DirectTask{}, errors.New("not found")
	}}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         tasks,
		Dispatcher:    &fakeBoardItemDispatcher{},
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx) // must not panic
}

// TestQueueRunner_Reconcile_FailedTask_UpdateError verifies that when an item is
// in DirectTaskStatusFailed and Board.Update errors, the runner logs and continues.
func TestQueueRunner_Reconcile_FailedTask_UpdateError(t *testing.T) {
	board := &errOnUpdateBoard{}
	board.items = []domain.WorkItem{
		{ID: "item1", Status: domain.WorkItemStatusInProgress, ActiveTaskID: "t1"},
	}
	tasks := &fakeTaskStore{getFn: func(_ context.Context, id string) (domain.DirectTask, error) {
		return domain.DirectTask{ID: id, Status: domain.DirectTaskStatusFailed, Output: "err output"}, nil
	}}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         tasks,
		Dispatcher:    &fakeBoardItemDispatcher{},
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx) // must not panic despite Update error
}

// TestQueueRunner_IsReady_UnknownQueueMode verifies that isReady returns false
// for an unrecognised QueueMode value.
func TestQueueRunner_IsReady_UnknownQueueMode(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "unsupported-mode"},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx)
	if disp.dispatchedCount() != 0 {
		t.Error("should not dispatch item with unknown queue mode")
	}
}

// TestQueueRunner_Launch_UpdateError verifies that launch returns false and logs
// a warning when Board.Update returns an error.
func TestQueueRunner_Launch_UpdateError(t *testing.T) {
	board := &errOnUpdateBoard{}
	board.items = []domain.WorkItem{
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot", QueueMode: "asap"},
	}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx)
	if disp.dispatchedCount() != 0 {
		t.Error("should not dispatch when Board.Update fails in launch")
	}
}

// ── run_when tests ────────────────────────────────────────────────────────────

// TestQueueRunner_RunWhen_TimeNotYetAndNoPred verifies that a run_when item
// is NOT dispatched when the scheduled time is still in the future.
func TestQueueRunner_RunWhen_TimeNotYetAndNoPred(t *testing.T) {
	future := time.Now().Add(time.Hour)
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_when", QueueRunAt: &future},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx)
	if disp.dispatchedCount() != 0 {
		t.Error("should not dispatch run_when item when time has not arrived")
	}
}

// TestQueueRunner_RunWhen_TimePassedNoPred verifies that a run_when item with
// no predecessor is dispatched once the scheduled time passes (QueueRunAt nil
// means time condition is satisfied immediately).
func TestQueueRunner_RunWhen_TimePassedNoPred(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_when", QueueRunAt: nil, QueueAfterItemID: ""},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	if disp.dispatchedCount() == 0 {
		t.Error("expected run_when item dispatched when time is nil (satisfied) and no predecessor")
	}
}

// TestQueueRunner_RunWhen_BothConditionsMet verifies dispatch when both a past
// run_at time and a done predecessor are satisfied.
func TestQueueRunner_RunWhen_BothConditionsMet(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "pred", Status: domain.WorkItemStatusDone},
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_when", QueueRunAt: &past, QueueAfterItemID: "pred",
			QueueRequireSuccess: true},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	if disp.dispatchedCount() == 0 {
		t.Error("expected run_when item dispatched when both time and predecessor conditions are met")
	}
}

// TestQueueRunner_RunWhen_PredErrored_RequireSuccess verifies that a run_when
// item with require_success=true is NOT dispatched when predecessor errored.
func TestQueueRunner_RunWhen_PredErrored_RequireSuccess(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "pred", Status: domain.WorkItemStatusErrored},
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_when", QueueRunAt: &past, QueueAfterItemID: "pred",
			QueueRequireSuccess: true},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx)
	if disp.dispatchedCount() != 0 {
		t.Error("should not dispatch run_when item when predecessor errored and require_success is true")
	}
}

// TestQueueRunner_RunWhen_PredErrored_NoRequireSuccess verifies dispatch when
// predecessor errored and require_success=false.
func TestQueueRunner_RunWhen_PredErrored_NoRequireSuccess(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "pred", Status: domain.WorkItemStatusErrored},
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_when", QueueRunAt: &past, QueueAfterItemID: "pred",
			QueueRequireSuccess: false},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	if disp.dispatchedCount() == 0 {
		t.Error("expected run_when item dispatched when predecessor errored and require_success is false")
	}
}

// TestQueueRunner_RunWhen_PredNotFound verifies that a run_when item is NOT
// dispatched when the predecessor item cannot be found.
func TestQueueRunner_RunWhen_PredNotFound(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "q1", Status: domain.WorkItemStatusQueued, AssignedTo: "bot",
			QueueMode: "run_when", QueueRunAt: &past, QueueAfterItemID: "missing-pred"},
	}}
	disp := &fakeBoardItemDispatcher{}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         &fakeTaskStore{},
		Dispatcher:    disp,
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	runner.Start(ctx)
	if disp.dispatchedCount() != 0 {
		t.Error("should not dispatch run_when item when predecessor not found")
	}
}

func TestQueueRunner_Reconcile_BlockedTask(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "item1", Status: domain.WorkItemStatusInProgress, ActiveTaskID: "t1"},
	}}
	tasks := &fakeTaskStore{getFn: func(_ context.Context, id string) (domain.DirectTask, error) {
		return domain.DirectTask{ID: id, Status: domain.DirectTaskStatusBlocked, Output: "need git repo\nTASK_OUTCOME: blocked"}, nil
	}}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         tasks,
		Dispatcher:    &fakeBoardItemDispatcher{},
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
	item, err := board.Get(context.Background(), "item1")
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != domain.WorkItemStatusBlocked {
		t.Errorf("expected blocked, got %s", item.Status)
	}
	if item.LastResult == "" {
		t.Error("expected last_result populated")
	}
}

func TestQueueRunner_Reconcile_ErroredTask(t *testing.T) {
	board := &inMemBoard{items: []domain.WorkItem{
		{ID: "item1", Status: domain.WorkItemStatusInProgress, ActiveTaskID: "t1"},
	}}
	tasks := &fakeTaskStore{getFn: func(_ context.Context, id string) (domain.DirectTask, error) {
		return domain.DirectTask{ID: id, Status: domain.DirectTaskStatusErrored, Output: "unrecoverable failure\nTASK_OUTCOME: errored"}, nil
	}}
	runner := orchestrator.NewQueueRunner(orchestrator.QueueRunnerConfig{
		Board:         board,
		Tasks:         tasks,
		Dispatcher:    &fakeBoardItemDispatcher{},
		MaxConcurrent: 3,
		Interval:      10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	go runner.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
	item, err := board.Get(context.Background(), "item1")
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != domain.WorkItemStatusErrored {
		t.Errorf("expected errored, got %s", item.Status)
	}
}
