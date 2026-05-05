// Package workflow contains the application-layer use cases for managing
// work items through the BaoBot development workflow.
package workflow

import (
	"context"
	"errors"
	"time"
)

// ErrInvalidInput is returned when required fields are missing or invalid.
var ErrInvalidInput = errors.New("workflow: invalid input")

// ErrConflict is returned when an optimistic-lock conflict occurs on update.
var ErrConflict = errors.New("workflow: optimistic lock conflict")

// ErrNotFound is returned when a work item is not found.
var ErrNotFound = errors.New("workflow: item not found")

// WorkItemStatus is the lifecycle status of a work item.
type WorkItemStatus string

const (
	WorkItemStatusBacklog    WorkItemStatus = "backlog"
	WorkItemStatusInProgress WorkItemStatus = "in_progress"
	WorkItemStatusComplete   WorkItemStatus = "complete"
)

// WorkItemType classifies the work item.
type WorkItemType string

const (
	WorkItemTypeFeature WorkItemType = "feature"
	WorkItemTypeBug     WorkItemType = "bug"
	WorkItemTypeChore   WorkItemType = "chore"
)

// WorkItem is the M3 enriched work item with workflow tracking fields.
type WorkItem struct {
	ID            string
	Title         string
	Description   string
	WorkflowName  string
	WorkflowStep  string
	Status        WorkItemStatus
	AssignedBotID string
	Type          WorkItemType
	Priority      int
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	FutureStartAt *time.Time
	StartedAt     *time.Time // set when first moved to in_progress
}

// WorkItemStore persists and retrieves workflow work items.
type WorkItemStore interface {
	Create(ctx context.Context, item WorkItem) error
	Get(ctx context.Context, id string) (WorkItem, error)
	Update(ctx context.Context, item WorkItem) error
	ListByStatus(ctx context.Context, status WorkItemStatus) ([]WorkItem, error)
	ListByBot(ctx context.Context, botID string) ([]WorkItem, error)
	ListStalled(ctx context.Context, cutoff time.Time) ([]WorkItem, error)
	UpdateHeartbeat(ctx context.Context, id string) error
}
