package orchestrator

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

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
// returns with status=dispatched. If scheduledAt is in the future, the task is
// stored with status=pending and a goroutine is spawned to dispatch it at that time.
func (d *LocalTaskDispatcher) Dispatch(ctx context.Context, botName, instruction string, scheduledAt *time.Time, source domain.DirectTaskSource, threadID string) (domain.DirectTask, error) {
	now := time.Now().UTC()

	task := domain.DirectTask{
		BotName:     botName,
		Source:      source,
		ThreadID:    threadID,
		Instruction: instruction,
		Status:      domain.DirectTaskStatusPending,
		ScheduledAt: scheduledAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	created, err := d.store.Create(ctx, task)
	if err != nil {
		return domain.DirectTask{}, err
	}

	// Determine whether to dispatch immediately or schedule.
	if scheduledAt == nil || !scheduledAt.After(time.Now()) {
		return d.dispatchNow(ctx, created)
	}

	// Schedule for future dispatch.
	go d.dispatchAt(created, *scheduledAt)
	return created, nil
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
	task.Status = domain.DirectTaskStatusDispatched
	task.DispatchedAt = &now

	updated, err := d.store.Update(ctx, task)
	if err != nil {
		return task, err
	}
	return updated, nil
}

// dispatchAt waits until scheduledAt, then dispatches the task.
func (d *LocalTaskDispatcher) dispatchAt(task domain.DirectTask, scheduledAt time.Time) {
	delay := time.Until(scheduledAt)
	if delay > 0 {
		time.Sleep(delay)
	}
	ctx := context.Background()
	if _, err := d.dispatchNow(ctx, task); err != nil {
		slog.Error("scheduled task dispatch failed", "task_id", task.ID, "bot", task.BotName, "err", err)
	}
}

// sendMessage encodes the TaskPayload and sends it to the bot's queue.
func (d *LocalTaskDispatcher) sendMessage(ctx context.Context, task domain.DirectTask) error {
	payload := domain.TaskPayload{
		TaskID:      task.ID,
		Instruction: task.Instruction,
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
