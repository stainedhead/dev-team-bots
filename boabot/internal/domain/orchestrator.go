package domain

import (
	"context"
	"time"
)

// ControlPlane manages the team registry.
type ControlPlane interface {
	Register(ctx context.Context, entry BotEntry) error
	Deregister(ctx context.Context, name string) error
	UpdateHeartbeat(ctx context.Context, name string) error
	Get(ctx context.Context, name string) (BotEntry, error)
	List(ctx context.Context) ([]BotEntry, error)
	IsTypeActive(ctx context.Context, botType string) (bool, error)
}

type BotEntry struct {
	Name          string    `json:"name"`
	BotType       string    `json:"bot_type"`
	Description   string    `json:"description,omitempty"` // short human-readable summary of the bot's role/skills
	QueueURL      string    `json:"queue_url,omitempty"`
	AgentCardURL  string    `json:"agent_card_url,omitempty"`
	Status        BotStatus `json:"status"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	RegisteredAt  time.Time `json:"registered_at"`
}

type BotStatus string

const (
	BotStatusActive   BotStatus = "active"
	BotStatusInactive BotStatus = "inactive"
)

// BoardStore manages Kanban work items.
type BoardStore interface {
	Create(ctx context.Context, item WorkItem) (WorkItem, error)
	Update(ctx context.Context, item WorkItem) (WorkItem, error)
	Get(ctx context.Context, id string) (WorkItem, error)
	List(ctx context.Context, filter WorkItemFilter) ([]WorkItem, error)
	Delete(ctx context.Context, id string) error
	// Reorder sets the SortPosition of each item to its index (1-based) in ids.
	// Items not in ids are unchanged.
	Reorder(ctx context.Context, ids []string) error
}

// Attachment holds a file uploaded to a WorkItem.
// If StoragePath is non-empty the file lives on disk and Content is empty.
// If StoragePath is empty the file is stored inline in Content (base64).
type Attachment struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Content     string    `json:"content,omitempty"`      // base64-encoded, only when not on disk
	StoragePath string    `json:"storage_path,omitempty"` // absolute path when stored on disk
	Size        int       `json:"size"`
	UploadedAt  time.Time `json:"uploaded_at"`
}

type WorkItem struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotency_key"` // client-supplied UUID; mutations with a seen key are no-ops
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	WorkDir        string         `json:"work_dir,omitempty"`
	Status         WorkItemStatus `json:"status"`
	AssignedTo     string         `json:"assigned_to"`
	ActiveTaskID   string         `json:"active_task_id,omitempty"`
	LastResult     string         `json:"last_result,omitempty"`
	LastResultAt   *time.Time     `json:"last_result_at,omitempty"`
	Attachments    []Attachment   `json:"attachments,omitempty"`
	SortPosition   int            `json:"sort_position"`
	// Queue configuration — populated when Status == WorkItemStatusQueued.
	QueueMode           string     `json:"queue_mode,omitempty"`            // "asap" | "run_at" | "run_after"
	QueueRunAt          *time.Time `json:"queue_run_at,omitempty"`          // used when QueueMode == "run_at"
	QueueAfterItemID    string     `json:"queue_after_item_id,omitempty"`   // predecessor item ID; used when QueueMode == "run_after"
	QueueRequireSuccess bool       `json:"queue_require_success,omitempty"` // predecessor must reach Done (not Errored)
	QueuedAt            *time.Time `json:"queued_at,omitempty"`             // when item entered Queued state
	CreatedBy           string     `json:"created_by"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type WorkItemStatus string

const (
	WorkItemStatusBacklog    WorkItemStatus = "backlog"
	WorkItemStatusQueued     WorkItemStatus = "queued"
	WorkItemStatusInProgress WorkItemStatus = "in-progress"
	WorkItemStatusBlocked    WorkItemStatus = "blocked"
	WorkItemStatusDone       WorkItemStatus = "done"
	WorkItemStatusErrored    WorkItemStatus = "errored"
)

// BoardItemDispatcher dispatches a board item to its assigned bot, building the
// instruction from the item's title, description, and attachments.
type BoardItemDispatcher interface {
	DispatchBoardItem(ctx context.Context, item WorkItem) (WorkItem, error)
}

type WorkItemFilter struct {
	AssignedTo   string
	Status       WorkItemStatus
	ActiveTaskID string // if non-empty, only items where ActiveTaskID matches
}

// UserStore manages human operator accounts.
type UserStore interface {
	Create(ctx context.Context, user User) (User, error)
	Update(ctx context.Context, user User) (User, error)
	Delete(ctx context.Context, username string) error
	Get(ctx context.Context, username string) (User, error)
	List(ctx context.Context) ([]User, error)
}

type User struct {
	Username           string    `json:"username"`
	DisplayName        string    `json:"display_name"`
	PasswordHash       string    `json:"-"`
	Role               UserRole  `json:"role"`
	Enabled            bool      `json:"enabled"`
	MustChangePassword bool      `json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
}

type UserRole string

const (
	UserRoleAdmin UserRole = "admin"
	UserRoleUser  UserRole = "user"
)

// AskRequest is a mid-task question directed at a bot that is actively running
// a board item. The bot answers between tool-call iterations and calls ReplyFn
// exactly once with its reply.
type AskRequest struct {
	Question string
	ReplyFn  func(reply string)
}

// AskRouter routes mid-task user questions to the bot handling a board item.
type AskRouter interface {
	// Enqueue sends an ask to the named bot. Returns false if the bot is
	// unavailable or its buffer is full.
	Enqueue(botName string, req AskRequest) bool
}
