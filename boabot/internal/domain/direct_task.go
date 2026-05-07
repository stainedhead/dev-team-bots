package domain

import (
	"context"
	"time"
)

// DirectTaskSource identifies where a DirectTask originated.
type DirectTaskSource string

const (
	// DirectTaskSourceChat means the task was created by the chat interface.
	DirectTaskSourceChat DirectTaskSource = "chat"
	// DirectTaskSourceOperator means the task was created directly by an operator.
	DirectTaskSourceOperator DirectTaskSource = "operator"
	// DirectTaskSourceBoard means the task was triggered by a board item.
	DirectTaskSourceBoard DirectTaskSource = "board"
)

// DirectTask represents an out-of-band task assigned directly to a bot by an operator.
type DirectTask struct {
	ID           string           `json:"id"`
	BotName      string           `json:"bot_name"`
	Source       DirectTaskSource `json:"source,omitempty"`
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
	Dispatch(ctx context.Context, botName, instruction string, scheduledAt *time.Time, source DirectTaskSource) (DirectTask, error)
}

// ChatThread represents a named conversation session.
type ChatThread struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Participants []string  `json:"participants"` // bot names
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ChatMessage represents one turn in an operator↔bot conversation.
type ChatMessage struct {
	ID        string        `json:"id"`
	ThreadID  string        `json:"thread_id,omitempty"`
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

// ChatStore persists and retrieves chat threads and messages.
type ChatStore interface {
	// Thread lifecycle
	CreateThread(ctx context.Context, title string, participants []string) (ChatThread, error)
	ListThreads(ctx context.Context) ([]ChatThread, error)
	DeleteThread(ctx context.Context, threadID string) error

	// Messages
	Append(ctx context.Context, msg ChatMessage) error
	// List returns messages for a specific thread (newest-first).
	List(ctx context.Context, threadID string) ([]ChatMessage, error)
	// ListAll returns all messages across all threads (newest-first).
	ListAll(ctx context.Context) ([]ChatMessage, error)
	// ListByBot returns all messages for a bot across all threads.
	ListByBot(ctx context.Context, botName string) ([]ChatMessage, error)
}
