package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
)

// CreateWorkItemUseCase creates new workflow work items.
type CreateWorkItemUseCase struct {
	store   WorkItemStore
	metrics metrics.MetricsStore
	now     func() time.Time
	newID   func() string
}

// NewCreateWorkItemUseCase constructs a CreateWorkItemUseCase.
func NewCreateWorkItemUseCase(store WorkItemStore, m metrics.MetricsStore) *CreateWorkItemUseCase {
	return &CreateWorkItemUseCase{
		store:   store,
		metrics: m,
		now:     time.Now,
		newID:   generateID,
	}
}

// Execute creates a new work item, persists it, and records a metric event.
// Returns ErrInvalidInput if title is blank.
func (uc *CreateWorkItemUseCase) Execute(
	ctx context.Context,
	title, description, workflowName string,
	itemType WorkItemType,
	priority int,
	futureStartAt *time.Time,
) (WorkItem, error) {
	if strings.TrimSpace(title) == "" {
		return WorkItem{}, fmt.Errorf("%w: title is required", ErrInvalidInput)
	}

	now := uc.now()
	item := WorkItem{
		ID:            uc.newID(),
		Title:         title,
		Description:   description,
		WorkflowName:  workflowName,
		WorkflowStep:  "backlog",
		Status:        WorkItemStatusBacklog,
		Type:          itemType,
		Priority:      priority,
		Version:       1,
		CreatedAt:     now,
		UpdatedAt:     now,
		FutureStartAt: futureStartAt,
	}

	if err := uc.store.Create(ctx, item); err != nil {
		return WorkItem{}, fmt.Errorf("create work item: %w", err)
	}

	uc.metrics.Record(metrics.MetricEvent{
		EventType: "item_created",
		ItemID:    metrics.WorkItemID(item.ID),
		Timestamp: now,
	})

	return item, nil
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
