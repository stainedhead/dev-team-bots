package orchestrator

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// QueueRunnerConfig holds the dependencies for the queue runner.
type QueueRunnerConfig struct {
	Board         domain.BoardStore
	Tasks         domain.DirectTaskStore
	Dispatcher    domain.BoardItemDispatcher
	MaxConcurrent int           // max in-progress items; default 3 if zero
	Interval      time.Duration // poll interval; default 5s if zero
}

// QueueRunner manages the Kanban queue: it reconciles completed tasks and
// dispatches eligible queued items when capacity is available.
type QueueRunner struct {
	cfg QueueRunnerConfig
}

// NewQueueRunner creates a QueueRunner with the supplied config.
func NewQueueRunner(cfg QueueRunnerConfig) *QueueRunner {
	return &QueueRunner{cfg: cfg}
}

// Start runs the queue loop until ctx is cancelled.
func (r *QueueRunner) Start(ctx context.Context) {
	interval := r.cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *QueueRunner) maxConcurrent() int {
	if r.cfg.MaxConcurrent > 0 {
		return r.cfg.MaxConcurrent
	}
	return 3
}

func (r *QueueRunner) tick(ctx context.Context) {
	// Step 1: Auto-transition completed tasks (in-progress → done/errored).
	r.reconcile(ctx)

	// Step 2: Count available slots.
	inProgress, err := r.cfg.Board.List(ctx, domain.WorkItemFilter{Status: domain.WorkItemStatusInProgress})
	if err != nil {
		return
	}
	slots := r.maxConcurrent() - len(inProgress)
	if slots <= 0 {
		return
	}

	// Step 3: Collect and sort queued items.
	queued, err := r.cfg.Board.List(ctx, domain.WorkItemFilter{Status: domain.WorkItemStatusQueued})
	if err != nil || len(queued) == 0 {
		return
	}

	// Priority: ASAP items first (FIFO by QueuedAt), then run_at/run_after (FIFO by QueuedAt).
	sort.SliceStable(queued, func(i, j int) bool {
		a, b := queued[i], queued[j]
		aAsap := a.QueueMode == "asap" || a.QueueMode == ""
		bAsap := b.QueueMode == "asap" || b.QueueMode == ""
		if aAsap && !bAsap {
			return true
		}
		if !aAsap && bAsap {
			return false
		}
		return queuedAtOf(a).Before(queuedAtOf(b))
	})

	dispatched := 0
	for _, item := range queued {
		if dispatched >= slots {
			break
		}
		if !r.isReady(ctx, item) {
			continue
		}
		if r.launch(ctx, item) {
			dispatched++
		}
	}
}

// reconcile checks all in-progress items and transitions those whose tasks
// have completed to done (success) or errored (failure).
func (r *QueueRunner) reconcile(ctx context.Context) {
	inProgress, err := r.cfg.Board.List(ctx, domain.WorkItemFilter{Status: domain.WorkItemStatusInProgress})
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, item := range inProgress {
		if item.ActiveTaskID == "" {
			continue
		}
		task, taskErr := r.cfg.Tasks.Get(ctx, item.ActiveTaskID)
		if taskErr != nil {
			continue
		}
		switch task.Status {
		case domain.DirectTaskStatusSucceeded:
			item.Status = domain.WorkItemStatusDone
			item.ActiveTaskID = ""
			if task.Output != "" {
				item.LastResult = task.Output
			}
			item.LastResultAt = &now
			if _, err := r.cfg.Board.Update(ctx, item); err != nil {
				slog.Warn("queue runner: failed to mark item done", "id", item.ID, "err", err)
			}
		case domain.DirectTaskStatusFailed:
			item.Status = domain.WorkItemStatusErrored
			item.ActiveTaskID = ""
			if task.Output != "" {
				item.LastResult = task.Output
			}
			item.LastResultAt = &now
			if _, err := r.cfg.Board.Update(ctx, item); err != nil {
				slog.Warn("queue runner: failed to mark item errored", "id", item.ID, "err", err)
			}
		}
	}
}

// isReady returns true if the queued item's scheduling rules are satisfied.
func (r *QueueRunner) isReady(ctx context.Context, item domain.WorkItem) bool {
	switch item.QueueMode {
	case "asap", "":
		return true
	case "run_at":
		return item.QueueRunAt != nil && !time.Now().Before(*item.QueueRunAt)
	case "run_after":
		if item.QueueAfterItemID == "" {
			return true // no predecessor specified — treat as ASAP
		}
		pred, err := r.cfg.Board.Get(ctx, item.QueueAfterItemID)
		if err != nil {
			return false
		}
		if item.QueueRequireSuccess {
			return pred.Status == domain.WorkItemStatusDone
		}
		return pred.Status == domain.WorkItemStatusDone || pred.Status == domain.WorkItemStatusErrored
	case "run_when":
		// Both conditions must be satisfied: time has passed AND predecessor is done.
		timeOK := item.QueueRunAt == nil || !time.Now().Before(*item.QueueRunAt)
		if !timeOK {
			return false
		}
		if item.QueueAfterItemID == "" {
			return true
		}
		pred, err := r.cfg.Board.Get(ctx, item.QueueAfterItemID)
		if err != nil {
			return false
		}
		if item.QueueRequireSuccess {
			return pred.Status == domain.WorkItemStatusDone
		}
		return pred.Status == domain.WorkItemStatusDone || pred.Status == domain.WorkItemStatusErrored
	}
	return false
}

// launch transitions an item from queued to in-progress and dispatches it.
func (r *QueueRunner) launch(ctx context.Context, item domain.WorkItem) bool {
	item.Status = domain.WorkItemStatusInProgress
	item.QueueMode = ""
	item.QueueRunAt = nil
	item.QueueAfterItemID = ""
	item.QueuedAt = nil

	updated, err := r.cfg.Board.Update(ctx, item)
	if err != nil {
		slog.Warn("queue runner: failed to set item in-progress", "id", item.ID, "err", err)
		return false
	}
	if _, dispErr := r.cfg.Dispatcher.DispatchBoardItem(ctx, updated); dispErr != nil {
		slog.Warn("queue runner: dispatch failed", "id", item.ID, "err", dispErr)
		// Item is already in-progress; leave it so the operator can investigate.
	}
	return true
}

func queuedAtOf(item domain.WorkItem) time.Time {
	if item.QueuedAt != nil {
		return *item.QueuedAt
	}
	return item.CreatedAt
}
