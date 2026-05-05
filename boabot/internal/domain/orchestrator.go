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
	Name          string
	BotType       string
	QueueURL      string
	AgentCardURL  string // S3 URL of the bot's published Agent Card
	Status        BotStatus
	LastHeartbeat time.Time
	RegisteredAt  time.Time
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

type WorkItem struct {
	ID             string
	IdempotencyKey string // client-supplied UUID; mutations with a seen key are no-ops
	Title          string
	Description    string
	Status         WorkItemStatus
	AssignedTo     string
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type WorkItemStatus string

const (
	WorkItemStatusBacklog    WorkItemStatus = "backlog"
	WorkItemStatusInProgress WorkItemStatus = "in-progress"
	WorkItemStatusBlocked    WorkItemStatus = "blocked"
	WorkItemStatusDone       WorkItemStatus = "done"
)

type WorkItemFilter struct {
	AssignedTo string
	Status     WorkItemStatus
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
	Username           string
	DisplayName        string
	PasswordHash       string
	Role               UserRole
	Enabled            bool
	MustChangePassword bool
	CreatedAt          time.Time
}

type UserRole string

const (
	UserRoleAdmin UserRole = "admin"
	UserRoleUser  UserRole = "user"
)
