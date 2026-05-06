// Package rebalancing contains the application-layer use case for detecting and
// resolving pipeline bottlenecks by reassigning work items.
package rebalancing

import (
	"fmt"
	"log/slog"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/rebalancing"
)

// RebalanceUseCase detects bottlenecks and produces work-item reassignments,
// logging each assignment as a metric event and notifying the operator.
type RebalanceUseCase struct {
	engine   rebalancing.RebalancingEngine
	metrics  metrics.MetricsStore
	notifier notification.NotificationSender
}

// NewRebalanceUseCase constructs a RebalanceUseCase.
func NewRebalanceUseCase(
	engine rebalancing.RebalancingEngine,
	store metrics.MetricsStore,
	notifier notification.NotificationSender,
) *RebalanceUseCase {
	return &RebalanceUseCase{
		engine:   engine,
		metrics:  store,
		notifier: notifier,
	}
}

// Run analyses bots for bottlenecks and, if one is found, computes
// assignments, records them as metric events, and sends a notification. It
// returns the assignments produced (empty slice when no bottleneck exists).
func (u *RebalanceUseCase) Run(bots []rebalancing.BotStatus) ([]rebalancing.Assignment, error) {
	bn := u.engine.DetectBottleneck(bots)
	if bn == nil {
		return nil, nil
	}

	assignments, err := u.engine.Rebalance(*bn)
	if err != nil {
		return nil, fmt.Errorf("rebalance: %w", err)
	}

	for _, a := range assignments {
		u.metrics.Record(metrics.MetricEvent{
			EventType: "rebalancing.assignment",
			BotID:     metrics.BotID(a.ToBotID),
			ItemID:    metrics.WorkItemID(a.ItemID),
			StepName:  "rebalancing",
		})
	}

	if len(assignments) > 0 {
		n := notification.Notification{
			Type:    notification.NotifRebalanced,
			Subject: fmt.Sprintf("Rebalanced: %d item(s) moved from %s", len(assignments), bn.BlockedBotID),
			Body:    fmt.Sprintf("Bottleneck reason: %s. Items reassigned: %d", bn.Reason, len(assignments)),
			Metadata: map[string]string{
				"blocked_bot_id":   bn.BlockedBotID,
				"assignment_count": fmt.Sprintf("%d", len(assignments)),
			},
		}
		if err := u.notifier.Send(n); err != nil {
			slog.Error("failed to send rebalancing notification", "err", err)
		}
	}

	return assignments, nil
}
