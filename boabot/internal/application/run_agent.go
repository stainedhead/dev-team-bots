package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// TaskResultHandler is a callback invoked when a task.result message is received.
type TaskResultHandler func(ctx context.Context, p domain.TaskResultPayload)

type RunAgentUseCase struct {
	identity             domain.BotIdentity
	queue                domain.MessageQueue
	broadcaster          domain.Broadcaster
	workerFactory        domain.WorkerFactory
	monitors             []domain.ChannelMonitor
	orchestratorQueueURL string
	taskResultHandler    TaskResultHandler
}

// WithTaskResultHandler registers a callback that is invoked whenever a
// task.result message is received by the agent. The handler is called
// synchronously in the message-handling goroutine.
func (u *RunAgentUseCase) WithTaskResultHandler(h TaskResultHandler) {
	u.taskResultHandler = h
}

func NewRunAgentUseCase(
	identity domain.BotIdentity,
	queue domain.MessageQueue,
	broadcaster domain.Broadcaster,
	workerFactory domain.WorkerFactory,
	monitors []domain.ChannelMonitor,
	orchestratorQueueURL string,
) *RunAgentUseCase {
	return &RunAgentUseCase{
		identity:             identity,
		queue:                queue,
		broadcaster:          broadcaster,
		workerFactory:        workerFactory,
		monitors:             monitors,
		orchestratorQueueURL: orchestratorQueueURL,
	}
}

func (u *RunAgentUseCase) Run(ctx context.Context) error {
	if err := u.register(ctx); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	for _, m := range u.monitors {
		if err := m.Start(ctx); err != nil {
			return fmt.Errorf("channel monitor start failed: %w", err)
		}
	}

	return u.poll(ctx)
}

func (u *RunAgentUseCase) Shutdown(ctx context.Context) error {
	for _, m := range u.monitors {
		_ = m.Stop(ctx)
	}
	return u.broadcastShutdown(ctx)
}

func (u *RunAgentUseCase) register(ctx context.Context) error {
	payload, err := json.Marshal(domain.RegisterPayload{
		Name:     u.identity.Name,
		BotType:  u.identity.BotType,
		QueueURL: u.identity.QueueURL,
	})
	if err != nil {
		return err
	}
	return u.queue.Send(ctx, u.orchestratorQueueURL, domain.Message{
		Type:    domain.MessageTypeRegister,
		From:    u.identity.Name,
		Payload: payload,
	})
}

func (u *RunAgentUseCase) broadcastShutdown(ctx context.Context) error {
	return u.broadcaster.Broadcast(ctx, domain.Message{
		Type: domain.MessageTypeShutdown,
		From: u.identity.Name,
	})
}

func (u *RunAgentUseCase) poll(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msgs, err := u.queue.Receive(ctx)
		if err != nil {
			slog.Error("queue receive error", "err", err)
			continue
		}

		for _, rm := range msgs {
			go u.handle(ctx, rm)
		}
	}
}

func (u *RunAgentUseCase) handle(ctx context.Context, rm domain.ReceivedMessage) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("worker panic recovered", "panic", r)
		}
	}()

	switch rm.Message.Type {
	case domain.MessageTypeTask:
		u.handleTask(ctx, rm)
	case domain.MessageTypeTaskResult:
		u.handleTaskResult(ctx, rm)
	case domain.MessageTypeOrchestratorPresence:
		u.handleOrchestratorPresence(ctx, rm)
	case domain.MessageTypeShutdown:
		// another bot shut down — no action required for non-orchestrator bots
	default:
		slog.Warn("unhandled message type", "type", rm.Message.Type)
	}

	_ = u.queue.Delete(ctx, rm.ReceiptHandle)
}

func (u *RunAgentUseCase) handleTask(ctx context.Context, rm domain.ReceivedMessage) {
	var p domain.TaskPayload
	if err := json.Unmarshal(rm.Message.Payload, &p); err != nil {
		slog.Error("failed to unmarshal task payload", "err", err)
		return
	}

	worker := u.workerFactory.New()
	result, err := worker.Execute(ctx, domain.Task{
		ID:          p.TaskID,
		BoardItemID: p.BoardItemID,
		Instruction: p.Instruction,
		Source:      string(rm.Message.From),
	})
	if err != nil {
		slog.Error("task execution error", "task_id", p.TaskID, "err", err)
	}

	slog.Info("task completed", "task_id", result.TaskID, "success", result.Success)

	// Notify the result handler directly — worker bots would send a task.result
	// message back over the queue, but when the bot executes its own task there
	// is no return message, so we call the handler inline here.
	if u.taskResultHandler != nil {
		u.taskResultHandler(ctx, domain.TaskResultPayload{
			TaskID:  result.TaskID,
			Output:  result.Output,
			Success: result.Success,
		})
	}
}

// handleTaskResult processes an incoming task.result message. If a handler is
// registered via WithTaskResultHandler, it is invoked with the decoded payload.
func (u *RunAgentUseCase) handleTaskResult(ctx context.Context, rm domain.ReceivedMessage) {
	if u.taskResultHandler == nil {
		return
	}
	var p domain.TaskResultPayload
	if err := json.Unmarshal(rm.Message.Payload, &p); err != nil {
		slog.Error("failed to unmarshal task result payload", "err", err)
		return
	}
	u.taskResultHandler(ctx, p)
}

// re-register with the new orchestrator instance when it broadcasts its presence.
func (u *RunAgentUseCase) handleOrchestratorPresence(ctx context.Context, _ domain.ReceivedMessage) {
	if err := u.register(ctx); err != nil {
		slog.Error("re-registration failed", "err", err)
	}
}
