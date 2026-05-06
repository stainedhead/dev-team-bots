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

// ChatMessage represents one turn in an operator↔bot conversation.
type ChatMessage struct {
	ID        string        `json:"id"`
	BotName   string        `json:"bot_name"`
	Direction ChatDirection `json:"direction"` // "outbound" | "inbound"
	Content   string        `json:"content"`
	TaskID    string        `json:"task_id,omitempty"` // links to the DirectTask
	CreatedAt time.Time     `json:"created_at"`
}

// ChatDirection indicates which side sent the message.
type ChatDirection string

const (
	// ChatDirectionOutbound is a message from the operator to the bot.
	ChatDirectionOutbound ChatDirection = "outbound"
	// ChatDirectionInbound is a message from the bot to the operator.
	ChatDirectionInbound ChatDirection = "inbound"
)

// ChatStore persists and retrieves chat messages.
type ChatStore interface {
	Append(ctx context.Context, msg ChatMessage) error
	List(ctx context.Context, botName string) ([]ChatMessage, error)
	ListAll(ctx context.Context) ([]ChatMessage, error)
}
