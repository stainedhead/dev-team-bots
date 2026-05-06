package domain

import (
	"context"
	"time"
)

// DirectTask represents an out-of-band task assigned directly to a bot by an operator.
type DirectTask struct {
	ID           string           `json:"id"`
	BotName      string           `json:"bot_name"`
	Instruction  string           `json:"instruction"`
	Status       DirectTaskStatus `json:"status"`
	ScheduledAt  *time.Time       `json:"scheduled_at,omitempty"`
	DispatchedAt *time.Time       `json:"dispatched_at,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

// DirectTaskStatus represents the lifecycle state of a direct task.
type DirectTaskStatus string

const (
	// DirectTaskStatusPending means the task has been created but not yet dispatched.
	DirectTaskStatusPending DirectTaskStatus = "pending"
	// DirectTaskStatusDispatched means the task message has been sent to the bot queue.
	DirectTaskStatusDispatched DirectTaskStatus = "dispatched"
	// DirectTaskStatusFailed means the task could not be dispatched.
	DirectTaskStatusFailed DirectTaskStatus = "failed"
)

// DirectTaskStore persists and retrieves direct tasks.
type DirectTaskStore interface {
	Create(ctx context.Context, task DirectTask) (DirectTask, error)
	Update(ctx context.Context, task DirectTask) (DirectTask, error)
	Get(ctx context.Context, id string) (DirectTask, error)
	List(ctx context.Context, botName string) ([]DirectTask, error)
	ListAll(ctx context.Context) ([]DirectTask, error)
}

// TaskDispatcher assigns immediate or scheduled tasks to bots.
type TaskDispatcher interface {
	Dispatch(ctx context.Context, botName, instruction string, scheduledAt *time.Time) (DirectTask, error)
}
