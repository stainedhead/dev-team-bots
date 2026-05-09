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
