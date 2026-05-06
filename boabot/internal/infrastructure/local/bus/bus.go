// Package bus provides a local in-process implementation of domain.Broadcaster.
// It is intended for single-binary operation without any cloud infrastructure.
//
// A Bus maintains a registry of named subscriber channels. Broadcast fans a
// message out to every registered channel. Delivery is non-blocking: if a
// subscriber's channel is full the message is dropped and a warning is logged to
// stderr. A panicking subscriber is recovered and the fan-out continues to
// remaining subscribers.
package bus

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// Bus is a thread-safe, in-process message broadcaster.
// It implements domain.Broadcaster.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[string]chan domain.Message
}

// New constructs an empty Bus with no subscribers.
func New() *Bus {
	return &Bus{
		subscribers: make(map[string]chan domain.Message),
	}
}

// Register creates a buffered channel for botName with the given buffer size
// and returns the read-only end of it. If bufferSize is 0, a buffer of 1 is
// used. Panics if botName is already registered.
func (b *Bus) Register(botName string, bufferSize int) <-chan domain.Message {
	if bufferSize <= 0 {
		bufferSize = 1
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.subscribers[botName]; exists {
		panic(fmt.Sprintf("local/bus: bot %q already registered", botName))
	}
	ch := make(chan domain.Message, bufferSize)
	b.subscribers[botName] = ch
	return ch
}

// Broadcast fans msg out to every registered subscriber channel.
// Per-subscriber delivery is non-blocking: if the channel is full the message
// is dropped and a warning is logged. A panicking subscriber is recovered and
// the broadcast continues to remaining subscribers.
// Broadcast always returns nil — delivery failures are handled by dropping.
func (b *Bus) Broadcast(_ context.Context, msg domain.Message) error {
	b.mu.RLock()
	// Snapshot the subscriber list while holding the read lock.
	subs := make([]chan domain.Message, 0, len(b.subscribers))
	names := make([]string, 0, len(b.subscribers))
	for name, ch := range b.subscribers {
		subs = append(subs, ch)
		names = append(names, name)
	}
	b.mu.RUnlock()

	for i, ch := range subs {
		deliverSafe(names[i], ch, msg)
	}
	return nil
}

// deliverSafe attempts a non-blocking send to ch. It recovers from panics
// (e.g. send on closed channel) and logs a warning if the channel is full.
func deliverSafe(name string, ch chan domain.Message, msg domain.Message) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("local/bus: recovered panic delivering to %q: %v", name, r)
		}
	}()
	select {
	case ch <- msg:
	default:
		log.Printf("local/bus: channel full for %q, dropping message %s", name, msg.ID)
	}
}
