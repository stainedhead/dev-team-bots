package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

const testOrchestratorQueueURL = "https://sqs.us-east-1.amazonaws.com/123/orchestrator"

func buildUseCase(
	queue *mocks.MessageQueue,
	broadcaster *mocks.Broadcaster,
	factory *mocks.WorkerFactory,
	monitors []domain.ChannelMonitor,
) *application.RunAgentUseCase {
	identity := domain.BotIdentity{
		Name:     "test-bot",
		BotType:  "developer",
		QueueURL: "https://sqs.us-east-1.amazonaws.com/123/test-bot",
	}
	return application.NewRunAgentUseCase(
		identity, queue, broadcaster, factory, monitors, testOrchestratorQueueURL,
	)
}

// TestRunAgent_Register verifies that Run sends a register message to the
// orchestrator queue. The context is cancelled immediately after registration
// to avoid hanging in poll().
func TestRunAgent_Register(t *testing.T) {
	registered := make(chan struct{}, 1)
	queue := &mocks.MessageQueue{
		SendFn: func(_ context.Context, url string, msg domain.Message) error {
			if msg.Type == domain.MessageTypeRegister && url == testOrchestratorQueueURL {
				registered <- struct{}{}
			}
			return nil
		},
		// Return an error immediately so poll() exits after 1 cycle.
		ReceiveFn: func(_ context.Context) ([]domain.ReceivedMessage, error) {
			return nil, errors.New("done")
		},
	}
	broadcaster := &mocks.Broadcaster{}
	worker := &mocks.Worker{}
	factory := &mocks.WorkerFactory{Worker: worker}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	uc := buildUseCase(queue, broadcaster, factory, nil)
	go func() { _ = uc.Run(ctx) }()

	select {
	case <-registered:
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for registration message")
	}

	// Verify payload.
	if len(queue.SendCalls) == 0 {
		t.Fatal("expected at least one Send call")
	}
	registerCall := queue.SendCalls[0]
	var payload domain.RegisterPayload
	if err := json.Unmarshal(registerCall.Message.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal register payload: %v", err)
	}
	if payload.Name != "test-bot" {
		t.Fatalf("expected Name=test-bot got %s", payload.Name)
	}
	if payload.BotType != "developer" {
		t.Fatalf("expected BotType=developer got %s", payload.BotType)
	}
}

// TestRunAgent_Register_SendError verifies that Run returns an error when
// registration fails.
func TestRunAgent_Register_SendError(t *testing.T) {
	sentinel := errors.New("queue unavailable")
	queue := &mocks.MessageQueue{
		SendFn: func(_ context.Context, _ string, _ domain.Message) error { return sentinel },
	}
	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)

	err := uc.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from registration failure")
	}
}

// TestRunAgent_MonitorStart verifies that all channel monitors are started.
func TestRunAgent_MonitorStart(t *testing.T) {
	queue := &mocks.MessageQueue{
		ReceiveFn: func(_ context.Context) ([]domain.ReceivedMessage, error) {
			return nil, errors.New("stop")
		},
	}
	m1 := &mocks.ChannelMonitor{}
	m2 := &mocks.ChannelMonitor{}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, []domain.ChannelMonitor{m1, m2})
	go func() { _ = uc.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)

	if m1.StartCalls == 0 {
		t.Fatal("expected monitor 1 to be started")
	}
	if m2.StartCalls == 0 {
		t.Fatal("expected monitor 2 to be started")
	}
}

// TestRunAgent_MonitorStart_Error verifies that Run returns an error when a
// monitor fails to start.
func TestRunAgent_MonitorStart_Error(t *testing.T) {
	queue := &mocks.MessageQueue{}
	sentinel := errors.New("monitor start failed")
	m := &mocks.ChannelMonitor{
		StartFn: func(_ context.Context) error { return sentinel },
	}

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, []domain.ChannelMonitor{m})
	err := uc.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when monitor fails to start")
	}
}

// TestRunAgent_Shutdown verifies that Shutdown broadcasts a shutdown message
// and stops monitors.
func TestRunAgent_Shutdown(t *testing.T) {
	broadcaster := &mocks.Broadcaster{}
	m := &mocks.ChannelMonitor{}

	queue := &mocks.MessageQueue{}
	uc := buildUseCase(queue, broadcaster, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, []domain.ChannelMonitor{m})

	err := uc.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(broadcaster.BroadcastCalls) != 1 {
		t.Fatalf("expected 1 broadcast call got %d", len(broadcaster.BroadcastCalls))
	}
	if broadcaster.BroadcastCalls[0].Type != domain.MessageTypeShutdown {
		t.Fatalf("expected MessageTypeShutdown got %s", broadcaster.BroadcastCalls[0].Type)
	}
	if m.StopCalls == 0 {
		t.Fatal("expected monitor Stop to be called during Shutdown")
	}
}

// TestRunAgent_Poll_TaskMessage verifies that receiving a task message causes
// the worker to execute and the message to be deleted.
func TestRunAgent_Poll_TaskMessage(t *testing.T) {
	taskPayload, _ := json.Marshal(domain.TaskPayload{
		TaskID:      "task-42",
		BoardItemID: "board-1",
		Instruction: "write the tests",
	})

	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeTask,
				From:    "orchestrator",
				Payload: taskPayload,
			},
			ReceiptHandle: "receipt-abc",
		},
	}

	workerExecuted := make(chan domain.Task, 1)
	worker := &mocks.Worker{
		ExecuteFn: func(_ context.Context, task domain.Task) (domain.TaskResult, error) {
			workerExecuted <- task
			return domain.TaskResult{TaskID: task.ID, Success: true}, nil
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				// Block until context cancels.
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			deleted <- rh
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: worker}, nil)
	go func() { _ = uc.Run(ctx) }()

	select {
	case task := <-workerExecuted:
		if task.ID != "task-42" {
			t.Fatalf("expected task ID task-42 got %s", task.ID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for task execution")
	}

	select {
	case rh := <-deleted:
		if rh != "receipt-abc" {
			t.Fatalf("expected receipt-abc got %s", rh)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for message delete")
	}
}

// TestRunAgent_Poll_ContextCancel verifies that poll() exits cleanly when the
// context is cancelled.
func TestRunAgent_Poll_ContextCancel(t *testing.T) {
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)

	done := make(chan error, 1)
	go func() { done <- uc.Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil error on context cancel, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to exit after context cancel")
	}
}

// TestRunAgent_Poll_InvalidTaskPayload verifies that a task message with a
// malformed JSON payload is dropped without panicking.
func TestRunAgent_Poll_InvalidTaskPayload(t *testing.T) {
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeTask,
				From:    "orchestrator",
				Payload: []byte("not-valid-json"),
			},
			ReceiptHandle: "receipt-bad",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	go func() { _ = uc.Run(ctx) }()

	select {
	case rh := <-deleted:
		if rh != "receipt-bad" {
			t.Fatalf("expected receipt-bad got %s", rh)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for bad-payload message to be deleted")
	}
}

// TestRunAgent_Poll_WorkerError verifies that a worker execution error is
// logged but does not stop the agent.
func TestRunAgent_Poll_WorkerError(t *testing.T) {
	taskPayload, _ := json.Marshal(domain.TaskPayload{TaskID: "t-err", Instruction: "fail"})
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeTask,
				From:    "orchestrator",
				Payload: taskPayload,
			},
			ReceiptHandle: "receipt-worker-err",
		},
	}

	workerCalled := make(chan struct{}, 1)
	worker := &mocks.Worker{
		ExecuteFn: func(_ context.Context, _ domain.Task) (domain.TaskResult, error) {
			workerCalled <- struct{}{}
			return domain.TaskResult{}, errors.New("worker failed")
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: worker}, nil)
	go func() { _ = uc.Run(ctx) }()

	select {
	case <-workerCalled:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for worker to be called")
	}
}

// TestRunAgent_Poll_ShutdownMessage verifies that a shutdown message from
// another bot is processed without panicking.
func TestRunAgent_Poll_ShutdownMessage(t *testing.T) {
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type: domain.MessageTypeShutdown,
				From: "another-bot",
			},
			ReceiptHandle: "receipt-shutdown",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	go func() { _ = uc.Run(ctx) }()

	select {
	case rh := <-deleted:
		if rh != "receipt-shutdown" {
			t.Fatalf("expected receipt-shutdown got %s", rh)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for shutdown message to be deleted")
	}
}

// TestRunAgent_Poll_UnknownMessageType verifies that unknown message types are
// logged and discarded.
func TestRunAgent_Poll_UnknownMessageType(t *testing.T) {
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type: domain.MessageType("unknown.type"),
				From: "somewhere",
			},
			ReceiptHandle: "receipt-unknown",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	go func() { _ = uc.Run(ctx) }()

	select {
	case <-deleted:
		// OK — message was processed and deleted.
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for unknown message to be deleted")
	}
}

// TestRunAgent_Poll_PanicRecovery verifies that a panicking worker does not
// crash the agent — the panic is recovered and the poll loop continues.
func TestRunAgent_Poll_PanicRecovery(t *testing.T) {
	// Two messages: one whose worker panics, then one that succeeds.
	taskPayload, _ := json.Marshal(domain.TaskPayload{TaskID: "t-panic", Instruction: "panic!"})
	goodPayload, _ := json.Marshal(domain.TaskPayload{TaskID: "t-good", Instruction: "succeed"})

	msgCh := make(chan []domain.ReceivedMessage, 2)
	msgCh <- []domain.ReceivedMessage{
		{
			Message:       domain.Message{Type: domain.MessageTypeTask, From: "orchestrator", Payload: taskPayload},
			ReceiptHandle: "receipt-panic",
		},
	}
	msgCh <- []domain.ReceivedMessage{
		{
			Message:       domain.Message{Type: domain.MessageTypeTask, From: "orchestrator", Payload: goodPayload},
			ReceiptHandle: "receipt-good",
		},
	}

	goodWorkerCalled := make(chan struct{}, 1)
	worker := &mocks.Worker{
		ExecuteFn: func(_ context.Context, task domain.Task) (domain.TaskResult, error) {
			if task.ID == "t-panic" {
				panic("intentional panic for test")
			}
			goodWorkerCalled <- struct{}{}
			return domain.TaskResult{TaskID: task.ID, Success: true}, nil
		},
	}
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: worker}, nil)
	go func() { _ = uc.Run(ctx) }()

	// The agent must survive the panic and process the next message.
	select {
	case <-goodWorkerCalled:
		// OK — agent recovered from panic and continued.
	case <-time.After(1 * time.Second):
		t.Fatal("timed out — agent did not recover from panic and continue processing")
	}
}

// TestRunAgent_Poll_OrchestratorPresence_ReregistrationError verifies that a
// re-registration failure is handled gracefully (logged, not panicked).
func TestRunAgent_Poll_OrchestratorPresence_ReregistrationError(t *testing.T) {
	// First receive returns an orchestrator presence message.
	// Subsequent receives block until context is cancelled.
	presenceMsgCh := make(chan []domain.ReceivedMessage, 1)
	presenceMsgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type: domain.MessageTypeOrchestratorPresence,
				From: "orchestrator",
			},
			ReceiptHandle: "rh-re-reg-error",
		},
	}

	// Gate channel: initial registration succeeds (gate open), re-registration fails (gate closed).
	allowSend := make(chan struct{}, 1)
	allowSend <- struct{}{} // permit first (initial) registration

	deleted := make(chan string, 1)

	queue := &mocks.MessageQueue{
		SendFn: func(_ context.Context, _ string, msg domain.Message) error {
			if msg.Type == domain.MessageTypeRegister {
				select {
				case <-allowSend:
					// Initial registration succeeds.
					return nil
				default:
					// Re-registration fails.
					return errors.New("re-registration failed")
				}
			}
			return nil
		},
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-presenceMsgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	go func() { _ = uc.Run(ctx) }()

	// Must still delete the message even when re-registration fails.
	select {
	case <-deleted:
		// OK
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for presence message to be deleted after re-registration failure")
	}
}

// TestRunAgent_TaskResultHandler_CalledOnTaskResult verifies that when a
// task.result message is received and a handler is registered via
// WithTaskResultHandler, the handler is called with the decoded payload.
func TestRunAgent_TaskResultHandler_CalledOnTaskResult(t *testing.T) {
	resultPayload, _ := json.Marshal(domain.TaskResultPayload{
		TaskID:  "task-42",
		Output:  "all tests pass",
		Success: true,
	})

	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeTaskResult,
				From:    "dev-1",
				Payload: resultPayload,
			},
			ReceiptHandle: "receipt-result",
		},
	}

	handlerCalled := make(chan domain.TaskResultPayload, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, _ string) error { return nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	uc.WithTaskResultHandler(func(_ context.Context, p domain.TaskResultPayload) {
		handlerCalled <- p
	})
	go func() { _ = uc.Run(ctx) }()

	select {
	case p := <-handlerCalled:
		if p.TaskID != "task-42" {
			t.Errorf("expected TaskID=task-42, got %q", p.TaskID)
		}
		if p.Output != "all tests pass" {
			t.Errorf("expected Output='all tests pass', got %q", p.Output)
		}
		if !p.Success {
			t.Error("expected Success=true")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for task result handler to be called")
	}
}

// TestRunAgent_SubTeamSpawn_CallsManager verifies that a subteam.spawn message
// causes SubTeamManager.Spawn to be called with the correct arguments.
func TestRunAgent_SubTeamSpawn_CallsManager(t *testing.T) {
	spawnPayload, _ := json.Marshal(domain.SubTeamSpawnPayload{
		BotType: "tech-lead",
		Name:    "tech-lead-1",
		WorkDir: "/tmp/work",
	})

	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeSubTeamSpawn,
				From:    "orchestrator",
				Payload: spawnPayload,
			},
			ReceiptHandle: "receipt-spawn",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	spawned := make(chan mocks.SpawnCall, 1)
	sm := &mocks.SubTeamManager{
		SpawnFn: func(_ context.Context, botType, name, workDir string) (*domain.SpawnedAgent, error) {
			spawned <- mocks.SpawnCall{BotType: botType, Name: name, WorkDir: workDir}
			return &domain.SpawnedAgent{Name: name}, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	uc.WithSubTeamManager(sm)
	go func() { _ = uc.Run(ctx) }()

	select {
	case call := <-spawned:
		if call.BotType != "tech-lead" {
			t.Errorf("expected BotType=tech-lead, got %q", call.BotType)
		}
		if call.Name != "tech-lead-1" {
			t.Errorf("expected Name=tech-lead-1, got %q", call.Name)
		}
		if call.WorkDir != "/tmp/work" {
			t.Errorf("expected WorkDir=/tmp/work, got %q", call.WorkDir)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SubTeamManager.Spawn to be called")
	}

	select {
	case rh := <-deleted:
		if rh != "receipt-spawn" {
			t.Errorf("expected receipt-spawn, got %q", rh)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message to be deleted")
	}
}

// TestRunAgent_SubTeamTerminate_CallsManager verifies that a subteam.terminate
// message causes SubTeamManager.Terminate to be called.
func TestRunAgent_SubTeamTerminate_CallsManager(t *testing.T) {
	termPayload, _ := json.Marshal(domain.SubTeamTerminatePayload{Name: "tech-lead-1"})

	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeSubTeamTerminate,
				From:    "orchestrator",
				Payload: termPayload,
			},
			ReceiptHandle: "receipt-term",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	terminated := make(chan string, 1)
	sm := &mocks.SubTeamManager{
		TerminateFn: func(_ context.Context, name string) error {
			terminated <- name
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	uc.WithSubTeamManager(sm)
	go func() { _ = uc.Run(ctx) }()

	select {
	case name := <-terminated:
		if name != "tech-lead-1" {
			t.Errorf("expected tech-lead-1, got %q", name)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SubTeamManager.Terminate to be called")
	}

	select {
	case <-deleted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message to be deleted")
	}
}

// TestRunAgent_SubTeamHeartbeat_DeletesMessage verifies that a subteam.heartbeat
// message is processed (deleted) without error.
func TestRunAgent_SubTeamHeartbeat_DeletesMessage(t *testing.T) {
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type: domain.MessageTypeSubTeamHeartbeat,
				From: "orchestrator",
			},
			ReceiptHandle: "receipt-hb",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	go func() { _ = uc.Run(ctx) }()

	select {
	case rh := <-deleted:
		if rh != "receipt-hb" {
			t.Errorf("expected receipt-hb, got %q", rh)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for heartbeat message to be deleted")
	}
}

// TestRunAgent_SubTeamSpawn_InvalidPayload_Graceful verifies that a subteam.spawn
// message with invalid JSON payload is dropped without panicking.
func TestRunAgent_SubTeamSpawn_InvalidPayload_Graceful(t *testing.T) {
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeSubTeamSpawn,
				From:    "orchestrator",
				Payload: []byte("not-json"),
			},
			ReceiptHandle: "receipt-bad-spawn",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sm := &mocks.SubTeamManager{}
	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	uc.WithSubTeamManager(sm)
	go func() { _ = uc.Run(ctx) }()

	select {
	case <-deleted:
		// OK — bad message deleted, no panic.
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bad spawn message to be deleted")
	}
}

// TestRunAgent_SubTeamSpawn_NoManager_Graceful verifies that a subteam.spawn
// message when no SubTeamManager is registered is dropped without panic.
func TestRunAgent_SubTeamSpawn_NoManager_Graceful(t *testing.T) {
	spawnPayload, _ := json.Marshal(domain.SubTeamSpawnPayload{
		BotType: "tech-lead",
		Name:    "tech-lead-1",
	})
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeSubTeamSpawn,
				From:    "orchestrator",
				Payload: spawnPayload,
			},
			ReceiptHandle: "receipt-no-mgr",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// No sub team manager set.
	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	go func() { _ = uc.Run(ctx) }()

	select {
	case <-deleted:
		// OK — deleted without panicking.
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

// TestRunAgent_Poll_OrchestratorPresence verifies that receiving an
// orchestrator presence message triggers re-registration.
func TestRunAgent_Poll_OrchestratorPresence(t *testing.T) {
	// First receive: orchestrator presence.  Subsequent receives: nothing (block on ctx).
	callCount := 0
	reregistered := make(chan struct{}, 10)

	queue := &mocks.MessageQueue{
		SendFn: func(_ context.Context, url string, msg domain.Message) error {
			if msg.Type == domain.MessageTypeRegister {
				reregistered <- struct{}{}
			}
			return nil
		},
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			callCount++
			if callCount == 1 {
				// Initial registration already done; first poll returns presence msg.
				return []domain.ReceivedMessage{
					{
						Message: domain.Message{
							Type: domain.MessageTypeOrchestratorPresence,
							From: "orchestrator",
						},
						ReceiptHandle: "rh-presence",
					},
				}, nil
			}
			// Block until context done.
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	go func() { _ = uc.Run(ctx) }()

	// We expect at least 2 register messages: initial + re-registration.
	count := 0
	timeout := time.After(1 * time.Second)
	for count < 2 {
		select {
		case <-reregistered:
			count++
		case <-timeout:
			t.Fatalf("timed out waiting for re-registration, got %d register calls", count)
		}
	}
}

// TestRunAgent_SubTeamTerminate_InvalidPayload_Graceful verifies that a
// subteam.terminate message with invalid JSON is dropped without panic.
func TestRunAgent_SubTeamTerminate_InvalidPayload_Graceful(t *testing.T) {
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeSubTeamTerminate,
				From:    "orchestrator",
				Payload: []byte("bad-json"),
			},
			ReceiptHandle: "receipt-bad-term",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	sm := &mocks.SubTeamManager{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	uc.WithSubTeamManager(sm)
	go func() { _ = uc.Run(ctx) }()

	select {
	case <-deleted:
		// OK -- bad message dropped.
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

// TestRunAgent_SubTeamTerminate_ManagerError_Graceful verifies that a Terminate
// error from the SubTeamManager is logged but does not crash the agent.
func TestRunAgent_SubTeamTerminate_ManagerError_Graceful(t *testing.T) {
	termPayload, _ := json.Marshal(domain.SubTeamTerminatePayload{Name: "unknown-bot"})
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeSubTeamTerminate,
				From:    "orchestrator",
				Payload: termPayload,
			},
			ReceiptHandle: "receipt-term-err",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	sm := &mocks.SubTeamManager{
		TerminateFn: func(_ context.Context, _ string) error {
			return errors.New("agent not found")
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	uc.WithSubTeamManager(sm)
	go func() { _ = uc.Run(ctx) }()

	select {
	case <-deleted:
		// OK -- error logged, message deleted.
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

// TestRunAgent_SubTeamSpawn_ManagerError_Graceful verifies that a Spawn error
// from the SubTeamManager is logged but does not crash the agent.
func TestRunAgent_SubTeamSpawn_ManagerError_Graceful(t *testing.T) {
	spawnPayload, _ := json.Marshal(domain.SubTeamSpawnPayload{BotType: "tech-lead", Name: "tech-lead-1"})
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeSubTeamSpawn,
				From:    "orchestrator",
				Payload: spawnPayload,
			},
			ReceiptHandle: "receipt-spawn-err",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	sm := &mocks.SubTeamManager{
		SpawnFn: func(_ context.Context, _, _, _ string) (*domain.SpawnedAgent, error) {
			return nil, errors.New("spawn failed")
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	uc.WithSubTeamManager(sm)
	go func() { _ = uc.Run(ctx) }()

	select {
	case <-deleted:
		// OK -- error logged, message deleted.
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

// TestRunAgent_TaskResultHandler_NoHandler_Graceful verifies that a task.result
// message is deleted gracefully when no handler is registered.
func TestRunAgent_TaskResultHandler_NoHandler_Graceful(t *testing.T) {
	resultPayload, _ := json.Marshal(domain.TaskResultPayload{TaskID: "t-1", Success: true})
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeTaskResult,
				From:    "dev-1",
				Payload: resultPayload,
			},
			ReceiptHandle: "receipt-result-noh",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// No task result handler set.
	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	go func() { _ = uc.Run(ctx) }()

	select {
	case <-deleted:
		// OK.
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

// TestRunAgent_SubTeamTerminate_NoManager_Graceful verifies that a
// subteam.terminate with no SubTeamManager does not panic.
func TestRunAgent_SubTeamTerminate_NoManager_Graceful(t *testing.T) {
	termPayload, _ := json.Marshal(domain.SubTeamTerminatePayload{Name: "tech-lead-1"})
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{
		{
			Message: domain.Message{
				Type:    domain.MessageTypeSubTeamTerminate,
				From:    "orchestrator",
				Payload: termPayload,
			},
			ReceiptHandle: "receipt-noterm-mgr",
		},
	}

	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			select {
			case deleted <- rh:
			default:
			}
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	go func() { _ = uc.Run(ctx) }()

	select {
	case <-deleted:
		// OK.
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

// TestRunAgent_Poll_TaskMessage_WithResultHandler verifies that when a task
// completes and a result handler is set, the handler is called inline.
func TestRunAgent_Poll_TaskMessage_WithResultHandler(t *testing.T) {
	taskPayload, _ := json.Marshal(domain.TaskPayload{
		TaskID:      "task-rh",
		Instruction: "do something",
	})
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{{
		Message: domain.Message{
			Type:    domain.MessageTypeTask,
			From:    "orchestrator",
			Payload: taskPayload,
		},
		ReceiptHandle: "rh-receipt",
	}}
	worker := &mocks.Worker{
		ExecuteFn: func(_ context.Context, task domain.Task) (domain.TaskResult, error) {
			return domain.TaskResult{TaskID: task.ID, Success: true, Output: "result text"}, nil
		},
	}
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, _ string) error { return nil },
	}
	handlerCalled := make(chan domain.TaskResultPayload, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: worker}, nil)
	uc.WithTaskResultHandler(func(_ context.Context, p domain.TaskResultPayload) {
		handlerCalled <- p
	})
	go func() { _ = uc.Run(ctx) }()
	select {
	case p := <-handlerCalled:
		if p.TaskID != "task-rh" {
			t.Errorf("expected task-rh, got %q", p.TaskID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result handler")
	}
}

// TestRunAgent_TaskResult_MalformedPayload verifies that handleTaskResult
// handles a malformed JSON payload gracefully (logs and returns).
func TestRunAgent_TaskResult_MalformedPayload(t *testing.T) {
	msgCh := make(chan []domain.ReceivedMessage, 1)
	msgCh <- []domain.ReceivedMessage{{
		Message: domain.Message{
			Type:    domain.MessageTypeTaskResult,
			From:    "dev-1",
			Payload: []byte(`{invalid json`),
		},
		ReceiptHandle: "receipt-bad",
	}}
	deleted := make(chan string, 1)
	queue := &mocks.MessageQueue{
		ReceiveFn: func(ctx context.Context) ([]domain.ReceivedMessage, error) {
			select {
			case msgs := <-msgCh:
				return msgs, nil
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		},
		DeleteFn: func(_ context.Context, rh string) error {
			deleted <- rh
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc := buildUseCase(queue, &mocks.Broadcaster{}, &mocks.WorkerFactory{Worker: &mocks.Worker{}}, nil)
	uc.WithTaskResultHandler(func(_ context.Context, _ domain.TaskResultPayload) {})
	go func() { _ = uc.Run(ctx) }()
	select {
	case <-deleted:
		// OK — message was processed (and deleted) despite bad payload.
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}
