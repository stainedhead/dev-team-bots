package domain

import (
	"context"
	"time"
)

// DLQItem is a message that has been moved to the dead-letter queue after
// exhausting delivery retries.
type DLQItem struct {
	// ID is the provider-level message ID (e.g. SQS MessageId).
	ID string

	// ReceiptHandle is an opaque handle used to delete or change visibility on
	// the message.  It is not transmitted to API clients.
	ReceiptHandle string

	// QueueName is the human-readable name of the DLQ this item belongs to.
	QueueName string

	// Body is the raw message payload.
	Body string

	// ReceivedCount is how many times this message has been received (and
	// failed to process) before landing in the DLQ.
	ReceivedCount int

	// FirstReceived is when the message was first received.
	FirstReceived time.Time

	// LastReceived is when the message most recently became visible.
	LastReceived time.Time
}

// DLQStore provides read and disposition operations on the dead-letter queue.
type DLQStore interface {
	// List returns all items currently in the DLQ (up to a provider-imposed
	// limit). Implementations should not alter message visibility.
	List(ctx context.Context) ([]DLQItem, error)

	// Retry moves the identified item back onto the main processing queue for
	// redelivery, then removes it from the DLQ.
	Retry(ctx context.Context, id string) error

	// Discard permanently deletes the identified item from the DLQ.
	Discard(ctx context.Context, id string) error
}
