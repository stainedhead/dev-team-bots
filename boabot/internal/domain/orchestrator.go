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
}

// Attachment holds a file uploaded to a WorkItem.
type Attachment struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Content     string    `json:"content"`  // base64-encoded file bytes
	Size        int       `json:"size"`     // original byte count
	UploadedAt  time.Time `json:"uploaded_at"`
}

type WorkItem struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotency_key"` // client-supplied UUID; mutations with a seen key are no-ops
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Status         WorkItemStatus `json:"status"`
	AssignedTo     string         `json:"assigned_to"`
	ActiveTaskID   string         `json:"active_task_id,omitempty"`
	LastResult     string         `json:"last_result,omitempty"`
	LastResultAt   *time.Time     `json:"last_result_at,omitempty"`
	Attachments    []Attachment   `json:"attachments,omitempty"`
	CreatedBy      string         `json:"created_by"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type WorkItemStatus string

const (
	WorkItemStatusBacklog    WorkItemStatus = "backlog"
	WorkItemStatusInProgress WorkItemStatus = "in-progress"
	WorkItemStatusBlocked    WorkItemStatus = "blocked"
	WorkItemStatusDone       WorkItemStatus = "done"
)

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
