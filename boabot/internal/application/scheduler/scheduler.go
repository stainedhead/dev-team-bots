// Package scheduler provides a cron-based task scheduler that runs
// ScheduledTasks on their configured intervals.
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// ScheduledTask is a task that can be scheduled to run on a cron schedule.
type ScheduledTask struct {
	// ID is the unique identifier of this task.
	ID string

	// Name is a human-readable label.
	Name string

	// CronExpr is a standard 5-field cron expression (e.g. "*/5 * * * *").
	CronExpr string

	// Enabled controls whether the task is eligible to run.
	Enabled bool

	// FutureStartAt, if non-nil, holds the task until after this time.
	FutureStartAt *time.Time

	// LastRunAt is the last time the task was successfully executed.
	LastRunAt *time.Time
}

// TaskRunner executes a scheduled task.
type TaskRunner interface {
	Run(ctx context.Context, task ScheduledTask) error
}

// ScheduledTaskStore persists and retrieves scheduled tasks.
type ScheduledTaskStore interface {
	List(ctx context.Context) ([]ScheduledTask, error)
	UpdateLastRun(ctx context.Context, id string, lastRunAt time.Time) error
}

// Clock is an injectable time source.
type Clock interface {
	Now() time.Time
}

// realClock uses the actual system clock.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Scheduler runs ScheduledTasks on their cron schedules.
type Scheduler struct {
	store    ScheduledTaskStore
	runner   TaskRunner
	location *time.Location
	clock    Clock

	mu    sync.Mutex
	tasks map[string]ScheduledTask
}

// NewScheduler constructs a Scheduler. If location is nil, UTC is used.
func NewScheduler(store ScheduledTaskStore, runner TaskRunner, location *time.Location) *Scheduler {
	if location == nil {
		location = time.UTC
	}
	return &Scheduler{
		store:    store,
		runner:   runner,
		location: location,
		clock:    realClock{},
		tasks:    make(map[string]ScheduledTask),
	}
}

// WithClock replaces the clock for testing.
func (s *Scheduler) WithClock(c Clock) *Scheduler {
	s.clock = c
	return s
}

// Schedule registers or updates a task definition in the in-memory registry.
func (s *Scheduler) Schedule(task ScheduledTask) error {
	if _, err := cron.ParseStandard(task.CronExpr); err != nil {
		return fmt.Errorf("scheduler: invalid cron expression %q: %w", task.CronExpr, err)
	}
	s.mu.Lock()
	s.tasks[task.ID] = task
	s.mu.Unlock()
	return nil
}

// Start begins the scheduler loop. It ticks every minute (rounded to the next
// minute boundary) and runs all due tasks concurrently. It returns when ctx is
// cancelled.
func (s *Scheduler) Start(ctx context.Context) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	// Run an immediate check on start.
	s.runDue(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.runDue(ctx)
		}
	}
}

func (s *Scheduler) runDue(ctx context.Context) {
	tasks, err := s.store.List(ctx)
	if err != nil {
		return
	}

	now := s.clock.Now()
	var wg sync.WaitGroup

	for _, task := range tasks {
		if !task.Enabled {
			continue
		}
		if task.FutureStartAt != nil && now.Before(*task.FutureStartAt) {
			continue
		}

		nextRun, err := NextRun(task.CronExpr, s.lastRun(task))
		if err != nil {
			continue
		}
		if now.Before(nextRun) {
			continue
		}

		wg.Add(1)
		t := task // capture
		go func() {
			defer wg.Done()
			_ = s.runner.Run(ctx, t)
			_ = s.store.UpdateLastRun(ctx, t.ID, now)
		}()
	}

	wg.Wait()
}

// lastRun returns the LastRunAt for a task, or a zero time if never run.
func (s *Scheduler) lastRun(task ScheduledTask) time.Time {
	if task.LastRunAt != nil {
		return *task.LastRunAt
	}
	return time.Time{}
}

// NextRun parses a standard 5-field cron expression and returns the next run
// time after the given time t.
func NextRun(cronExpr string, after time.Time) (time.Time, error) {
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := p.Parse(cronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("scheduler: parse cron %q: %w", cronExpr, err)
	}
	return sched.Next(after), nil
}
