// Package queue provides a local in-process implementation of domain.MessageQueue.
// It is intended for single-binary operation without any cloud infrastructure.
//
// A Router is a shared registry that maps bot names to buffered channels.
// Each bot calls Register to get its own Queue. Messages are routed by bot name
// — the queueURL parameter passed to Send is treated as the destination bot name.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

const defaultBufferSize = 100

// Router is a thread-safe registry of named message channels.
// Create a single Router and share it across all bots in the process.
type Router struct {
	mu       sync.RWMutex
	channels map[string]chan domain.ReceivedMessage
}

// NewRouter constructs an empty Router.
func NewRouter() *Router {
	return &Router{
		channels: make(map[string]chan domain.ReceivedMessage),
	}
}

// Register creates a buffered channel for botName and returns a Queue backed by
// this Router. If bufferSize is 0, the default (100) is used.
// Register panics if botName is already registered.
func (r *Router) Register(botName string, bufferSize int) *Queue {
	if bufferSize <= 0 {
		bufferSize = defaultBufferSize
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.channels[botName]; exists {
		panic(fmt.Sprintf("local/queue: bot %q already registered", botName))
	}
	ch := make(chan domain.ReceivedMessage, bufferSize)
	r.channels[botName] = ch
	return &Queue{router: r, name: botName, ch: ch}
}

// send routes a ReceivedMessage to the channel of the named bot.
// Returns an error if the bot is unknown or its channel is full.
func (r *Router) send(botName string, rm domain.ReceivedMessage) error {
	r.mu.RLock()
	ch, ok := r.channels[botName]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("local/queue: unknown bot %q", botName)
	}
	select {
	case ch <- rm:
		return nil
	default:
		return fmt.Errorf("local/queue: channel full for bot %q", botName)
	}
}

// Queue is a named message queue backed by a Router channel.
// It implements domain.MessageQueue.
type Queue struct {
	router *Router
	name   string
	ch     chan domain.ReceivedMessage
}

// Send routes msg to the bot identified by queueURL (the bot name in local mode).
// Returns an error immediately if the destination channel is full or unknown.
func (q *Queue) Send(_ context.Context, queueURL string, msg domain.Message) error {
	if msg.ID == "" {
		msg.ID = generateID()
	}
	rm := domain.ReceivedMessage{
		Message:       msg,
		ReceiptHandle: msg.ID,
	}
	return q.router.send(queueURL, rm)
}

// Receive drains all available messages from this bot's channel in a
// non-blocking pass. If the channel is empty, it blocks until at least one
// message arrives or ctx is cancelled, returning ctx.Err() on cancellation.
func (q *Queue) Receive(ctx context.Context) ([]domain.ReceivedMessage, error) {
	// Non-blocking drain first.
	var msgs []domain.ReceivedMessage
	for {
		select {
		case rm := <-q.ch:
			msgs = append(msgs, rm)
		default:
			if len(msgs) > 0 {
				return msgs, nil
			}
			// Channel was empty — block until a message or cancellation.
			select {
			case rm := <-q.ch:
				msgs = append(msgs, rm)
				// Do one more non-blocking drain to collect any that arrived.
				for {
					select {
					case extra := <-q.ch:
						msgs = append(msgs, extra)
					default:
						return msgs, nil
					}
				}
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
}

// Delete is a no-op for local queues. Receipt handles are never re-delivered.
func (q *Queue) Delete(_ context.Context, _ string) error { return nil }

// generateID returns a unique string combining the current nanosecond timestamp
// and a random 32-bit integer read from crypto/rand.
func generateID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback: use nanotime only — still unique enough for local use.
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	n := binary.BigEndian.Uint32(b[:])
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), n)
}
