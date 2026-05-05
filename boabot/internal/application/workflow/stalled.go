package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
)

const stalledCutoffDuration = 5 * time.Minute

// StalledItemRecoveryUseCase recovers stalled work items by returning them to
// the backlog and clearing their bot assignment.
type StalledItemRecoveryUseCase struct {
	store   WorkItemStore
	metrics metrics.MetricsStore
	now     func() time.Time
}

// NewStalledItemRecoveryUseCase constructs a StalledItemRecoveryUseCase.
func NewStalledItemRecoveryUseCase(store WorkItemStore, m metrics.MetricsStore) *StalledItemRecoveryUseCase {
	return &StalledItemRecoveryUseCase{
		store:   store,
		metrics: m,
		now:     time.Now,
	}
}

// Execute finds stalled items (no heartbeat for >5 minutes), clears their
// assignment, resets their status to backlog, and returns the recovered list.
func (uc *StalledItemRecoveryUseCase) Execute(ctx context.Context) ([]WorkItem, error) {
	cutoff := uc.now().Add(-stalledCutoffDuration)
	stalled, err := uc.store.ListStalled(ctx, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list stalled: %w", err)
	}

	now := uc.now()
	recovered := make([]WorkItem, 0, len(stalled))

	for _, item := range stalled {
		item.AssignedBotID = ""
		item.Status = WorkItemStatusBacklog
		item.UpdatedAt = now
		item.Version++

		if err := uc.store.Update(ctx, item); err != nil {
			return recovered, fmt.Errorf("update stalled item %s: %w", item.ID, err)
		}

		uc.metrics.Record(metrics.MetricEvent{
			EventType: "item_requeued",
			ItemID:    metrics.WorkItemID(item.ID),
			Timestamp: now,
		})

		recovered = append(recovered, item)
	}

	return recovered, nil
}
