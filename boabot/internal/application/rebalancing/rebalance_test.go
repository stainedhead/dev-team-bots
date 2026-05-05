package rebalancing_test

import (
	"errors"
	"testing"

	apprebalancing "github.com/stainedhead/dev-team-bots/boabot/internal/application/rebalancing"
	metricsmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification"
	notifmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/rebalancing"
	rbmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/rebalancing/mocks"
)

func makeEngine(bottleneck *rebalancing.Bottleneck, assignments []rebalancing.Assignment, err error) *rbmocks.RebalancingEngine {
	return &rbmocks.RebalancingEngine{
		DetectBottleneckFn: func(_ []rebalancing.BotStatus) *rebalancing.Bottleneck { return bottleneck },
		RebalanceFn:        func(_ rebalancing.Bottleneck) ([]rebalancing.Assignment, error) { return assignments, err },
	}
}

func TestRebalance_NoBottleneck_NoAction(t *testing.T) {
	engine := makeEngine(nil, nil, nil)
	store := &metricsmocks.MetricsStore{}
	notifier := &notifmocks.NotificationSender{}
	uc := apprebalancing.NewRebalanceUseCase(engine, store, notifier)

	got, err := uc.Run([]rebalancing.BotStatus{{BotID: "bot-1", QueueDepth: 0}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty assignments got %d", len(got))
	}
	if len(store.RecordCalls) != 0 {
		t.Fatalf("expected no metric records got %d", len(store.RecordCalls))
	}
	if len(notifier.SendCalls) != 0 {
		t.Fatalf("expected no notifications got %d", len(notifier.SendCalls))
	}
}

func TestRebalance_BottleneckFound_AssignmentsReturned(t *testing.T) {
	bn := &rebalancing.Bottleneck{
		BlockedBotID:  "bot-heavy",
		Reason:        "cap exceeded",
		AffectedItems: []rebalancing.WorkItemID{"item-1"},
	}
	assignments := []rebalancing.Assignment{
		{ItemID: "item-1", FromBotID: "bot-heavy", ToBotID: "bot-light", Reason: "load balancing"},
	}
	engine := makeEngine(bn, assignments, nil)
	store := &metricsmocks.MetricsStore{}
	notifier := &notifmocks.NotificationSender{}
	uc := apprebalancing.NewRebalanceUseCase(engine, store, notifier)

	got, err := uc.Run([]rebalancing.BotStatus{{BotID: "bot-heavy", IsCapExceeded: true}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 assignment got %d", len(got))
	}
	// Metric event recorded.
	if len(store.RecordCalls) != 1 {
		t.Fatalf("expected 1 metric record got %d", len(store.RecordCalls))
	}
	// Notification sent.
	if len(notifier.SendCalls) != 1 {
		t.Fatalf("expected 1 notification got %d", len(notifier.SendCalls))
	}
	if notifier.SendCalls[0].Notification.Type != notification.NotifRebalanced {
		t.Fatalf("expected NotifRebalanced got %s", notifier.SendCalls[0].Notification.Type)
	}
}

func TestRebalance_RebalanceError_Propagated(t *testing.T) {
	bn := &rebalancing.Bottleneck{BlockedBotID: "bot-bad"}
	sentinel := errors.New("no available bots")
	engine := makeEngine(bn, nil, sentinel)
	store := &metricsmocks.MetricsStore{}
	notifier := &notifmocks.NotificationSender{}
	uc := apprebalancing.NewRebalanceUseCase(engine, store, notifier)

	_, err := uc.Run([]rebalancing.BotStatus{{BotID: "bot-bad", IsBlocked: true}})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error got %v", err)
	}
	if len(store.RecordCalls) != 0 {
		t.Fatalf("expected no metric records on error got %d", len(store.RecordCalls))
	}
}

func TestRebalance_MultipleAssignments_MultipleMetrics(t *testing.T) {
	bn := &rebalancing.Bottleneck{BlockedBotID: "bot-heavy", Reason: "blocked"}
	assignments := []rebalancing.Assignment{
		{ItemID: "item-1", FromBotID: "bot-heavy", ToBotID: "bot-a"},
		{ItemID: "item-2", FromBotID: "bot-heavy", ToBotID: "bot-b"},
		{ItemID: "item-3", FromBotID: "bot-heavy", ToBotID: "bot-c"},
	}
	engine := makeEngine(bn, assignments, nil)
	store := &metricsmocks.MetricsStore{}
	notifier := &notifmocks.NotificationSender{}
	uc := apprebalancing.NewRebalanceUseCase(engine, store, notifier)

	got, err := uc.Run([]rebalancing.BotStatus{{BotID: "bot-heavy", IsBlocked: true}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 assignments got %d", len(got))
	}
	if len(store.RecordCalls) != 3 {
		t.Fatalf("expected 3 metric records got %d", len(store.RecordCalls))
	}
	// Only one notification should be sent (summary).
	if len(notifier.SendCalls) != 1 {
		t.Fatalf("expected 1 notification got %d", len(notifier.SendCalls))
	}
}

func TestRebalance_NotificationError_DoesNotPropagate(t *testing.T) {
	bn := &rebalancing.Bottleneck{BlockedBotID: "bot-bad"}
	assignments := []rebalancing.Assignment{
		{ItemID: "item-1", FromBotID: "bot-bad", ToBotID: "bot-good"},
	}
	engine := makeEngine(bn, assignments, nil)
	store := &metricsmocks.MetricsStore{}
	notifier := &notifmocks.NotificationSender{
		SendFn: func(_ notification.Notification) error {
			return errors.New("SNS down")
		},
	}
	uc := apprebalancing.NewRebalanceUseCase(engine, store, notifier)

	got, err := uc.Run([]rebalancing.BotStatus{{BotID: "bot-bad", IsBlocked: true}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 assignment got %d", len(got))
	}
}
