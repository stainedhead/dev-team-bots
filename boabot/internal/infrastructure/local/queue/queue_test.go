package queue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
)

func newMsg(id, from, to string) domain.Message {
	return domain.Message{
		ID:        id,
		Type:      domain.MessageTypeTask,
		From:      from,
		To:        to,
		Timestamp: time.Now(),
	}
}

// TestRouter_RegisterAndSendReceive verifies the basic send/receive path.
func TestRouter_RegisterAndSendReceive(t *testing.T) {
	t.Parallel()
	r := queue.NewRouter()
	q := r.Register("alice", 10)

	msg := newMsg("msg-1", "bob", "alice")
	ctx := context.Background()

	if err := q.Send(ctx, "alice", msg); err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}

	msgs, err := q.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Message.ID != "msg-1" {
		t.Errorf("expected ID msg-1, got %s", msgs[0].Message.ID)
	}
	if msgs[0].ReceiptHandle != "msg-1" {
		t.Errorf("expected receipt handle msg-1, got %s", msgs[0].ReceiptHandle)
	}
}

// TestRouter_SendToUnknownBot verifies that sending to an unregistered bot returns an error.
func TestRouter_SendToUnknownBot(t *testing.T) {
	t.Parallel()
	r := queue.NewRouter()
	q := r.Register("alice", 10)

	msg := newMsg("msg-2", "alice", "nobody")
	err := q.Send(context.Background(), "nobody", msg)
	if err == nil {
		t.Fatal("expected error when sending to unknown bot, got nil")
	}
}

// TestRouter_SendFullBuffer verifies that Send returns an error immediately when the channel is full.
func TestRouter_SendFullBuffer(t *testing.T) {
	t.Parallel()
	r := queue.NewRouter()
	q := r.Register("alice", 2)

	ctx := context.Background()
	msg1 := newMsg("m1", "bob", "alice")
	msg2 := newMsg("m2", "bob", "alice")
	msg3 := newMsg("m3", "bob", "alice")

	if err := q.Send(ctx, "alice", msg1); err != nil {
		t.Fatalf("first Send: %v", err)
	}
	if err := q.Send(ctx, "alice", msg2); err != nil {
		t.Fatalf("second Send: %v", err)
	}
	err := q.Send(ctx, "alice", msg3)
	if err == nil {
		t.Fatal("expected error on full buffer, got nil")
	}
}

// TestQueue_ReceiveMultiple drains all queued messages in one Receive call.
func TestQueue_ReceiveMultiple(t *testing.T) {
	t.Parallel()
	r := queue.NewRouter()
	q := r.Register("alice", 10)

	ctx := context.Background()
	for i := range 3 {
		id := "m" + string(rune('0'+i))
		msg := newMsg(id, "bob", "alice")
		if err := q.Send(ctx, "alice", msg); err != nil {
			t.Fatalf("Send %d: %v", i, err)
		}
	}

	msgs, err := q.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
}

// TestQueue_ReceiveBlocksUntilMessage verifies that Receive blocks when channel is empty.
func TestQueue_ReceiveBlocksUntilMessage(t *testing.T) {
	t.Parallel()
	r := queue.NewRouter()
	q := r.Register("alice", 10)

	ctx := context.Background()
	done := make(chan []domain.ReceivedMessage, 1)

	go func() {
		msgs, _ := q.Receive(ctx)
		done <- msgs
	}()

	// Give the goroutine time to block.
	time.Sleep(20 * time.Millisecond)

	msg := newMsg("late", "bob", "alice")
	if err := q.Send(ctx, "alice", msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case msgs := <-done:
		if len(msgs) == 0 {
			t.Error("expected at least one message after unblock")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Receive did not unblock after message was sent")
	}
}

// TestQueue_ReceiveCancelledContext verifies that Receive returns ctx.Err() on cancellation.
func TestQueue_ReceiveCancelledContext(t *testing.T) {
	t.Parallel()
	r := queue.NewRouter()
	q := r.Register("alice", 10)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		_, err := q.Receive(ctx)
		done <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Receive did not return after context cancellation")
	}
}

// TestQueue_Delete verifies that Delete is a no-op and returns nil.
func TestQueue_Delete(t *testing.T) {
	t.Parallel()
	r := queue.NewRouter()
	q := r.Register("alice", 10)

	err := q.Delete(context.Background(), "any-handle")
	if err != nil {
		t.Errorf("Delete: expected nil, got %v", err)
	}
}

// TestQueue_GeneratesIDWhenEmpty verifies that Send generates an ID when msg.ID is empty.
func TestQueue_GeneratesIDWhenEmpty(t *testing.T) {
	t.Parallel()
	r := queue.NewRouter()
	q := r.Register("alice", 10)

	msg := domain.Message{
		Type:      domain.MessageTypeTask,
		From:      "bob",
		To:        "alice",
		Timestamp: time.Now(),
		// ID intentionally omitted
	}
	if err := q.Send(context.Background(), "alice", msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	msgs, err := q.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("no messages received")
	}
	if msgs[0].Message.ID == "" {
		t.Error("expected generated ID, got empty string")
	}
	if msgs[0].ReceiptHandle == "" {
		t.Error("expected non-empty receipt handle")
	}
}

// TestRouter_MultipleBotsIsolated verifies that messages go to the correct bot.
func TestRouter_MultipleBotsIsolated(t *testing.T) {
	t.Parallel()
	r := queue.NewRouter()
	qa := r.Register("alice", 10)
	qb := r.Register("bob", 10)

	ctx := context.Background()
	msgA := newMsg("for-alice", "charlie", "alice")
	msgB := newMsg("for-bob", "charlie", "bob")

	if err := qa.Send(ctx, "alice", msgA); err != nil {
		t.Fatalf("Send to alice: %v", err)
	}
	if err := qb.Send(ctx, "bob", msgB); err != nil {
		t.Fatalf("Send to bob: %v", err)
	}

	msgsA, err := qa.Receive(ctx)
	if err != nil {
		t.Fatalf("alice Receive: %v", err)
	}
	if len(msgsA) != 1 || msgsA[0].Message.ID != "for-alice" {
		t.Errorf("alice: expected for-alice, got %v", msgsA)
	}

	msgsB, err := qb.Receive(ctx)
	if err != nil {
		t.Fatalf("bob Receive: %v", err)
	}
	if len(msgsB) != 1 || msgsB[0].Message.ID != "for-bob" {
		t.Errorf("bob: expected for-bob, got %v", msgsB)
	}
}

// TestRouter_RegisterDuplicatePanics verifies that registering the same bot name twice panics.
func TestRouter_RegisterDuplicatePanics(t *testing.T) {
	t.Parallel()
	r := queue.NewRouter()
	r.Register("alice", 10)

	defer func() {
		if rec := recover(); rec == nil {
			t.Error("expected panic on duplicate Register, got none")
		}
	}()
	r.Register("alice", 10) // should panic
}

// TestQueue_DefaultBufferSize verifies that Register with bufferSize 0 uses the default.
func TestQueue_DefaultBufferSize(t *testing.T) {
	t.Parallel()
	r := queue.NewRouter()
	// bufferSize 0 should use default (100)
	q := r.Register("alice", 0)

	ctx := context.Background()
	// Send 50 messages without blocking
	for i := range 50 {
		id := string(rune('a' + i%26))
		msg := newMsg(id+"-"+string(rune('0'+i%10)), "bob", "alice")
		if err := q.Send(ctx, "alice", msg); err != nil {
			t.Fatalf("Send %d with default buffer: %v", i, err)
		}
	}
}
