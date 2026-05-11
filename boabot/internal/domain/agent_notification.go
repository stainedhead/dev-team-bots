package domain

import (
	"context"
	"time"
)

// AgentNotificationStatus represents the read/action state of a notification.
type AgentNotificationStatus string

const (
	AgentNotificationStatusUnread   AgentNotificationStatus = "unread"
	AgentNotificationStatusRead     AgentNotificationStatus = "read"
	AgentNotificationStatusActioned AgentNotificationStatus = "actioned"
)

// DiscussEntry is a single message in the discussion thread attached to a notification.
type DiscussEntry struct {
	Author    string    `json:"author"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// AgentNotification is an in-app notification raised by a bot to the operator.
type AgentNotification struct {
	ID             string                  `json:"id"`
	BotName        string                  `json:"bot_name"`
	TaskID         string                  `json:"task_id,omitempty"`
	WorkItemID     string                  `json:"work_item_id,omitempty"`
	WorkDir        string                  `json:"work_dir,omitempty"`
	Message        string                  `json:"message"`
	ContextSummary string                  `json:"context_summary,omitempty"`
	Status         AgentNotificationStatus `json:"status"`
	DiscussThread  []DiscussEntry          `json:"discuss_thread,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
	ActionedAt     *time.Time              `json:"actioned_at,omitempty"`
}

// AgentNotificationFilter constrains a List query.
// Empty fields are treated as "no constraint".
type AgentNotificationFilter struct {
	BotName string
	Status  AgentNotificationStatus
	Search  string
	// WorkDir filters by the leaf directory component of AgentNotification.WorkDir.
	// An exact match against path.Base(n.WorkDir) is performed.
	WorkDir string
}

// AgentNotificationStore persists and retrieves agent notifications.
type AgentNotificationStore interface {
	Save(ctx context.Context, n AgentNotification) error
	Get(ctx context.Context, id string) (AgentNotification, error)
	List(ctx context.Context, filter AgentNotificationFilter) ([]AgentNotification, error)
	UnreadCount(ctx context.Context) (int, error)
	AppendDiscuss(ctx context.Context, id string, entry DiscussEntry) error
	MarkActioned(ctx context.Context, id string) error
	Delete(ctx context.Context, ids []string) error
}
