// Package scheduling manages scheduling ticks and catch-up for DirectTask objects.
package scheduling

import (
	"context"
	"log/slog"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// SchedulerService manages scheduling ticks and catch-up for DirectTask objects.
type SchedulerService struct {
	store      domain.DirectTaskStore
	dispatcher domain.TaskDispatcher
}

// NewSchedulerService constructs a SchedulerService.
func NewSchedulerService(store domain.DirectTaskStore, dispatcher domain.TaskDispatcher) *SchedulerService {
	return &SchedulerService{
		store:      store,
		dispatcher: dispatcher,
	}
}

// Tick checks for due tasks and dispatches them. Safe to call concurrently —
// ClaimDue provides the atomicity guarantee.
// For recurring tasks: after dispatch, recalculates and persists NextRunAt.
// For Future (one-shot) tasks: after dispatch, NextRunAt is not updated (task is done).
func (s *SchedulerService) Tick(ctx context.Context, now time.Time) error {
	return s.processAllDue(ctx, now)
}

// CatchUpMissedRuns is called once on startup. Finds all tasks with NextRunAt in
// the past and dispatches them. For recurring tasks, multiple missed occurrences
// collapse into a single run — only one dispatch, then NextRunAt advances to the
// next future occurrence.
func (s *SchedulerService) CatchUpMissedRuns(ctx context.Context, now time.Time) error {
	return s.processAllDue(ctx, now)
}

// processAllDue is the shared implementation for Tick and CatchUpMissedRuns.
// It lists all due tasks and processes each one.
func (s *SchedulerService) processAllDue(ctx context.Context, now time.Time) error {
	tasks, err := s.store.ListDue(ctx, now)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		s.processTask(ctx, task, now)
	}
	return nil
}

// StartLoop runs a ticker every 10 seconds, calling Tick each time.
// Runs until ctx is cancelled. Calls CatchUpMissedRuns once before the first tick.
func (s *SchedulerService) StartLoop(ctx context.Context) error {
	if err := s.CatchUpMissedRuns(ctx, time.Now()); err != nil {
		slog.Error("scheduling: catch-up missed runs failed", "err", err)
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case t := <-ticker.C:
			if err := s.Tick(ctx, t); err != nil {
				slog.Error("scheduling: tick failed", "err", err)
			}
		}
	}
}

// processTask claims and dispatches a single due task, then reschedules it if
// it is a recurring task.
func (s *SchedulerService) processTask(ctx context.Context, task domain.DirectTask, now time.Time) {
	claimed, err := s.store.ClaimDue(ctx, task.ID)
	if err != nil {
		slog.Error("scheduling: ClaimDue failed", "task_id", task.ID, "err", err)
		return
	}
	if !claimed {
		return
	}

	if _, err := s.dispatcher.RunNow(ctx, task.ID); err != nil {
		slog.Error("scheduling: RunNow failed", "task_id", task.ID, "err", err)
		return
	}

	// For recurring tasks: advance NextRunAt to the next future slot.
	if task.Schedule.Mode != domain.ScheduleModeRecurring {
		return
	}
	if task.Schedule.Rule == nil {
		return
	}

	next := advanceToFuture(task.Schedule, now)
	if next == nil {
		return
	}

	// Fetch the fresh record so we don't overwrite the status that RunNow set.
	fresh, err := s.store.Get(ctx, task.ID)
	if err != nil {
		slog.Error("scheduling: Get after RunNow failed", "task_id", task.ID, "err", err)
		return
	}
	fresh.NextRunAt = next
	if _, err := s.store.Update(ctx, fresh); err != nil {
		slog.Error("scheduling: Update NextRunAt failed", "task_id", task.ID, "err", err)
	}
}

// advanceToFuture iterates NextAfter until it produces a time strictly after now,
// handling tasks that missed multiple occurrences by collapsing them into one.
func advanceToFuture(sched domain.Schedule, now time.Time) *time.Time {
	if sched.Rule == nil {
		return nil
	}
	// Start from now to find the next future occurrence.
	next := sched.Rule.NextAfter(now)
	t := next
	return &t
}
