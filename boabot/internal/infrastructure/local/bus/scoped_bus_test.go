package bus_test

import (
	"context"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bus"
)

// TestNewScopedBus_IsolatedFromOtherInstances verifies that two ScopedBus
// instances do not share subscriber state — a broadcast on one does not deliver
// to the other's subscribers.
func TestNewScopedBus_IsolatedFromOtherInstances(t *testing.T) {
	t.Parallel()

	b1 := bus.NewScopedBus()
	b2 := bus.NewScopedBus()

	// Register a subscriber on each bus.
	ch1 := b1.Register("alpha", 10)
	ch2 := b2.Register("beta", 10)

	ctx := context.Background()
	msg := domain.Message{
		ID:        "isolation-test",
		Type:      domain.MessageTypeHeartbeat,
		From:      "sender",
		Timestamp: time.Now(),
	}

	// Broadcast only on b1.
	if err := b1.Broadcast(ctx, msg); err != nil {
		t.Fatalf("Broadcast on b1: %v", err)
	}

	// ch1 should receive the message.
	select {
	case got := <-ch1:
		if got.ID != "isolation-test" {
			t.Errorf("b1 subscriber: expected isolation-test, got %s", got.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for b1 subscriber to receive message")
	}

	// ch2 must NOT receive anything — the two bus instances are isolated.
	select {
	case unexpected := <-ch2:
		t.Errorf("b2 subscriber received unexpected message: %s", unexpected.ID)
	case <-time.After(50 * time.Millisecond):
		// Good — no cross-delivery.
	}
}

// TestNewScopedBus_ReturnsNewBus verifies that NewScopedBus returns a non-nil Bus.
func TestNewScopedBus_ReturnsNewBus(t *testing.T) {
	t.Parallel()
	b := bus.NewScopedBus()
	if b == nil {
		t.Fatal("expected non-nil Bus from NewScopedBus")
	}
}

// TestNewScopedBus_MultipleBroadcastsIndependent verifies that two scoped buses
// each properly deliver to their own subscribers independently.
func TestNewScopedBus_MultipleBroadcastsIndependent(t *testing.T) {
	t.Parallel()

	b1 := bus.NewScopedBus()
	b2 := bus.NewScopedBus()

	ch1a := b1.Register("sub-a", 10)
	ch1b := b1.Register("sub-b", 10)
	ch2a := b2.Register("sub-c", 10)

	ctx := context.Background()
	msg1 := domain.Message{ID: "m1", Type: domain.MessageTypeHeartbeat, Timestamp: time.Now()}
	msg2 := domain.Message{ID: "m2", Type: domain.MessageTypeHeartbeat, Timestamp: time.Now()}

	_ = b1.Broadcast(ctx, msg1)
	_ = b2.Broadcast(ctx, msg2)

	// b1's subscribers should have received m1.
	for _, ch := range []<-chan domain.Message{ch1a, ch1b} {
		select {
		case got := <-ch:
			if got.ID != "m1" {
				t.Errorf("b1 sub: expected m1, got %s", got.ID)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for b1 broadcast")
		}
	}

	// b2's subscriber should have received m2.
	select {
	case got := <-ch2a:
		if got.ID != "m2" {
			t.Errorf("b2 sub: expected m2, got %s", got.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for b2 broadcast")
	}
}
