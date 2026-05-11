package orchestrator

import (
	"context"
	"encoding/json"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// Compile-time assertion: *LocalTaskDispatcher must satisfy domain.ScheduledTaskDispatcher.
var _ domain.ScheduledTaskDispatcher = (*LocalTaskDispatcher)(nil)

// LocalTaskDispatcher sends task messages to bots via the in-process queue router.
type LocalTaskDispatcher struct {
	store    domain.DirectTaskStore
	queue    domain.MessageQueue // the local queue; queueURL == bot name
	selfName string              // "operator" or "orchestrator" as the from field
}

// NewLocalTaskDispatcher creates a LocalTaskDispatcher.
func NewLocalTaskDispatcher(store domain.DirectTaskStore, queue domain.MessageQueue, selfName string) *LocalTaskDispatcher {
	return &LocalTaskDispatcher{
		store:    store,
		queue:    queue,
		selfName: selfName,
	}
}

// Dispatch creates a DirectTask and either dispatches it immediately or schedules it.
//
// If scheduledAt is nil or in the past, the task is dispatched immediately and
// returns with status=running. The task's Schedule is set to ASAP and NextRunAt is nil.
// If scheduledAt is in the future, the task is stored with status=pending, Schedule is
// set to Future mode with RunAt=scheduledAt, NextRunAt=scheduledAt, and a goroutine is
// spawned to dispatch it at that time.
func (d *LocalTaskDispatcher) Dispatch(ctx context.Context, botName, instruction string, scheduledAt *time.Time, source domain.DirectTaskSource, threadID string, workDir string) (domain.DirectTask, error) {
	now := time.Now().UTC()

	task := domain.DirectTask{
		BotName:     botName,
		Source:      source,
		ThreadID:    threadID,
		Instruction: instruction,
		Status:      domain.DirectTaskStatusPending,
		WorkDir:     workDir,
		ScheduledAt: scheduledAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	// Title is set by callers that populate the struct directly; Dispatch does not
	// accept it as a parameter to keep the signature stable.

	// Populate Schedule and NextRunAt based on scheduledAt.
	if scheduledAt == nil || !scheduledAt.After(now) {
		task.Schedule = domain.Schedule{Mode: domain.ScheduleModeASAP}
		task.NextRunAt = nil
	} else {
		task.Schedule = domain.Schedule{Mode: domain.ScheduleModeFuture, RunAt: scheduledAt}
		task.NextRunAt = scheduledAt
	}

	created, err := d.store.Create(ctx, task)
	if err != nil {
		return domain.DirectTask{}, err
	}

	// Determine whether to dispatch immediately or defer to the scheduler loop.
	if scheduledAt == nil || !scheduledAt.After(time.Now()) {
		return d.dispatchNow(ctx, created)
	}

	// Future task: return pending — the SchedulerService loop will dispatch it when due.
	return created, nil
}

// DispatchWithSchedule creates a DirectTask with the given Schedule and dispatches it.
// For ASAP and past-Future schedules: dispatches immediately.
// For future-Future schedules: stores with NextRunAt set, returns pending (picked up by SchedulerService).
// For Recurring schedules: stores with NextRunAt set to Schedule.NextRunAt(now), returns pending (driven by SchedulerService).
func (d *LocalTaskDispatcher) DispatchWithSchedule(ctx context.Context, botName, instruction string, schedule domain.Schedule, source domain.DirectTaskSource, threadID, workDir, title string) (domain.DirectTask, error) {
	now := time.Now().UTC()

	task := domain.DirectTask{
		BotName:     botName,
		Source:      source,
		ThreadID:    threadID,
		Instruction: instruction,
		Status:      domain.DirectTaskStatusPending,
		WorkDir:     workDir,
		Schedule:    schedule,
		Title:       title,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Determine NextRunAt and whether to dispatch immediately.
	switch schedule.Mode {
	case domain.ScheduleModeASAP:
		task.NextRunAt = nil
	case domain.ScheduleModeFuture:
		if schedule.RunAt != nil && schedule.RunAt.After(now) {
			// Future run: store pending for SchedulerService to pick up.
			task.NextRunAt = schedule.RunAt
			created, err := d.store.Create(ctx, task)
			if err != nil {
				return domain.DirectTask{}, err
			}
			return created, nil
		}
		// Past or zero RunAt: dispatch immediately.
		task.NextRunAt = nil
	case domain.ScheduleModeRecurring:
		// Compute next occurrence and store pending for SchedulerService.
		nextRunAt := schedule.NextRunAt(now)
		task.NextRunAt = nextRunAt
		created, err := d.store.Create(ctx, task)
		if err != nil {
			return domain.DirectTask{}, err
		}
		return created, nil
	}

	// ASAP or past-Future: dispatch immediately.
	created, err := d.store.Create(ctx, task)
	if err != nil {
		return domain.DirectTask{}, err
	}
	return d.dispatchNow(ctx, created)
}

// dispatchNow sends the task message immediately and updates the store.
func (d *LocalTaskDispatcher) dispatchNow(ctx context.Context, task domain.DirectTask) (domain.DirectTask, error) {
	if err := d.sendMessage(ctx, task); err != nil {
		// Mark as failed in the store.
		task.Status = domain.DirectTaskStatusFailed
		_, _ = d.store.Update(ctx, task)
		return task, err
	}

	now := time.Now().UTC()
	task.Status = domain.DirectTaskStatusRunning
	task.DispatchedAt = &now

	updated, err := d.store.Update(ctx, task)
	if err != nil {
		return task, err
	}
	return updated, nil
}

// RunNow immediately dispatches an existing task regardless of its scheduled time.
// If the task is already dispatched it is returned unchanged.
func (d *LocalTaskDispatcher) RunNow(ctx context.Context, id string) (domain.DirectTask, error) {
	task, err := d.store.Get(ctx, id)
	if err != nil {
		return domain.DirectTask{}, err
	}
	if task.Status == domain.DirectTaskStatusRunning || task.Status == domain.DirectTaskStatusDispatching {
		return task, nil
	}
	return d.dispatchNow(ctx, task)
}

// sendMessage encodes the TaskPayload and sends it to the bot's queue.
func (d *LocalTaskDispatcher) sendMessage(ctx context.Context, task domain.DirectTask) error {
	payload := domain.TaskPayload{
		TaskID:      task.ID,
		Instruction: task.Instruction,
		WorkDir:     task.WorkDir,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	msgID, err := newID()
	if err != nil {
		return err
	}

	msg := domain.Message{
		ID:        msgID,
		Type:      domain.MessageTypeTask,
		From:      d.selfName,
		To:        task.BotName,
		Payload:   payloadBytes,
		Timestamp: time.Now().UTC(),
	}
	return d.queue.Send(ctx, task.BotName, msg)
}
