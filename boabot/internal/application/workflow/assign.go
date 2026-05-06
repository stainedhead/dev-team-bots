package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
)

// AssignBotUseCase assigns a bot to a work item.
type AssignBotUseCase struct {
	store   WorkItemStore
	metrics metrics.MetricsStore
	now     func() time.Time
}

// NewAssignBotUseCase constructs an AssignBotUseCase.
func NewAssignBotUseCase(store WorkItemStore, m metrics.MetricsStore) *AssignBotUseCase {
	return &AssignBotUseCase{
		store:   store,
		metrics: m,
		now:     time.Now,
	}
}

// Execute assigns botID to the work item identified by itemID.
func (uc *AssignBotUseCase) Execute(ctx context.Context, itemID, botID string) (WorkItem, error) {
	item, err := uc.store.Get(ctx, itemID)
	if err != nil {
		return WorkItem{}, fmt.Errorf("get item %s: %w", itemID, err)
	}

	now := uc.now()
	item.AssignedBotID = botID
	item.UpdatedAt = now
	item.Version++

	if err := uc.store.Update(ctx, item); err != nil {
		return WorkItem{}, fmt.Errorf("update item: %w", err)
	}

	uc.metrics.Record(metrics.MetricEvent{
		EventType: "bot_assigned",
		BotID:     metrics.BotID(botID),
		ItemID:    metrics.WorkItemID(item.ID),
		Timestamp: now,
	})

	return item, nil
}
