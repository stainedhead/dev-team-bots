package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification"
	domainwf "github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"
)

// WorkflowAdvancer advances steps in a named workflow definition.
type WorkflowAdvancer interface {
	Advance(workflowName, currentStep string) (domainwf.WorkflowStep, error)
}

// AdvanceWorkflowUseCase advances a work item to the next workflow step.
type AdvanceWorkflowUseCase struct {
	store    WorkItemStore
	router   WorkflowAdvancer
	notifier notification.NotificationSender
	metrics  metrics.MetricsStore
	now      func() time.Time
}

// NewAdvanceWorkflowUseCase constructs an AdvanceWorkflowUseCase.
func NewAdvanceWorkflowUseCase(
	store WorkItemStore,
	router WorkflowAdvancer,
	notifier notification.NotificationSender,
	m metrics.MetricsStore,
) *AdvanceWorkflowUseCase {
	return &AdvanceWorkflowUseCase{
		store:    store,
		router:   router,
		notifier: notifier,
		metrics:  m,
		now:      time.Now,
	}
}

// Execute advances the work item identified by itemID to the next step.
// It retries once on ErrConflict from the store.
func (uc *AdvanceWorkflowUseCase) Execute(ctx context.Context, itemID string) (WorkItem, error) {
	item, err := uc.store.Get(ctx, itemID)
	if err != nil {
		return WorkItem{}, fmt.Errorf("get item %s: %w", itemID, err)
	}

	now := uc.now()
	nextStep, err := uc.router.Advance(item.WorkflowName, item.WorkflowStep)

	var eventType string
	if errors.Is(err, domainwf.ErrNoNextStep) {
		// Terminal: mark complete.
		var durationMinutes float64
		if item.StartedAt != nil {
			durationMinutes = now.Sub(*item.StartedAt).Minutes()
		}
		item.Status = WorkItemStatusComplete
		item.UpdatedAt = now
		item.Version++

		if updateErr := uc.updateWithRetry(ctx, item); updateErr != nil {
			return WorkItem{}, updateErr
		}

		uc.metrics.Record(metrics.MetricEvent{
			EventType:       "item_completed",
			ItemID:          metrics.WorkItemID(item.ID),
			DurationMinutes: durationMinutes,
			Timestamp:       now,
		})
		return item, nil
	}
	if err != nil {
		return WorkItem{}, fmt.Errorf("advance workflow: %w", err)
	}

	// Non-terminal: advance to next step.
	item.WorkflowStep = nextStep.Name
	item.Status = WorkItemStatusInProgress
	if item.StartedAt == nil {
		item.StartedAt = &now
	}
	item.UpdatedAt = now
	item.Version++

	if updateErr := uc.updateWithRetry(ctx, item); updateErr != nil {
		return WorkItem{}, updateErr
	}

	if nextStep.NotifyOnEntry {
		n := notification.Notification{
			Type:    notification.NotifSuccess,
			Subject: fmt.Sprintf("Work item %s entered step %s", item.ID, nextStep.Name),
			Body:    fmt.Sprintf("Item %q advanced to step %q.", item.Title, nextStep.Name),
		}
		_ = uc.notifier.Send(n) // best-effort
	}

	eventType = "step_advanced"
	uc.metrics.Record(metrics.MetricEvent{
		EventType: eventType,
		ItemID:    metrics.WorkItemID(item.ID),
		StepName:  nextStep.Name,
		Timestamp: now,
	})

	return item, nil
}

func (uc *AdvanceWorkflowUseCase) updateWithRetry(ctx context.Context, item WorkItem) error {
	err := uc.store.Update(ctx, item)
	if errors.Is(err, ErrConflict) {
		// Retry once after re-fetching.
		fresh, fetchErr := uc.store.Get(ctx, item.ID)
		if fetchErr != nil {
			return fmt.Errorf("re-fetch on conflict: %w", fetchErr)
		}
		fresh.WorkflowStep = item.WorkflowStep
		fresh.Status = item.Status
		fresh.UpdatedAt = item.UpdatedAt
		fresh.StartedAt = item.StartedAt
		fresh.Version = fresh.Version + 1
		if retryErr := uc.store.Update(ctx, fresh); retryErr != nil {
			return fmt.Errorf("retry update: %w", retryErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("update item: %w", err)
	}
	return nil
}
