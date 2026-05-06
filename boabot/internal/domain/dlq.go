package domain

import (
	"context"
	"time"
)

// DLQItem is a message that has been moved to the dead-letter queue after
// exhausting delivery retries.
type DLQItem struct {
	ID            string    `json:"id"`
	ReceiptHandle string    `json:"-"`
	QueueName     string    `json:"queue_name"`
	Body          string    `json:"body"`
	ReceivedCount int       `json:"received_count"`
	FirstReceived time.Time `json:"first_received"`
	LastReceived  time.Time `json:"last_received"`
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
