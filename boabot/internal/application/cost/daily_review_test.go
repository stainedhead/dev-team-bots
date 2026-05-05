package cost_test

import (
	"errors"
	"testing"

	appcost "github.com/stainedhead/dev-team-bots/boabot/internal/application/cost"
	domaincost "github.com/stainedhead/dev-team-bots/boabot/internal/domain/cost"
	costmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/cost/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification"
	notifmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification/mocks"
)

func TestDailyReview_NoAlerts(t *testing.T) {
	enforcer := &costmocks.CostEnforcer{
		DailySpendFn: func(_ domaincost.BotID) float64 { return 1.0 }, // 10% — below any threshold
	}
	notifier := &notifmocks.NotificationSender{}
	budget := defaultBudget()
	uc := appcost.NewDailyCostReviewUseCase(enforcer, notifier, budget, []domaincost.BotID{"bot-1", "bot-2"})

	uc.Review()

	if len(notifier.SendCalls) != 0 {
		t.Fatalf("expected no notifications got %d", len(notifier.SendCalls))
	}
}

func TestDailyReview_SpikeAlert(t *testing.T) {
	spends := map[domaincost.BotID]float64{
		"bot-1": 1.0, // 10% — fine
		"bot-2": 4.0, // 40% — spike
	}
	enforcer := &costmocks.CostEnforcer{
		DailySpendFn: func(id domaincost.BotID) float64 { return spends[id] },
	}
	notifier := &notifmocks.NotificationSender{}
	budget := defaultBudget()
	uc := appcost.NewDailyCostReviewUseCase(enforcer, notifier, budget, []domaincost.BotID{"bot-1", "bot-2"})

	uc.Review()

	if len(notifier.SendCalls) != 1 {
		t.Fatalf("expected 1 notification got %d", len(notifier.SendCalls))
	}
	if notifier.SendCalls[0].Notification.Type != notification.NotifCostSpike {
		t.Fatalf("expected NotifCostSpike got %s", notifier.SendCalls[0].Notification.Type)
	}
}

func TestDailyReview_FlatCapAlert(t *testing.T) {
	enforcer := &costmocks.CostEnforcer{
		DailySpendFn: func(_ domaincost.BotID) float64 { return 8.5 }, // 85%
	}
	notifier := &notifmocks.NotificationSender{}
	budget := defaultBudget()
	uc := appcost.NewDailyCostReviewUseCase(enforcer, notifier, budget, []domaincost.BotID{"bot-1"})

	uc.Review()

	if len(notifier.SendCalls) != 1 {
		t.Fatalf("expected 1 notification got %d", len(notifier.SendCalls))
	}
	if notifier.SendCalls[0].Notification.Type != notification.NotifCostFlatCap {
		t.Fatalf("expected NotifCostFlatCap got %s", notifier.SendCalls[0].Notification.Type)
	}
}

func TestDailyReview_ZeroCap_Skips(t *testing.T) {
	enforcer := &costmocks.CostEnforcer{
		DailySpendFn: func(_ domaincost.BotID) float64 { return 9999.0 },
	}
	notifier := &notifmocks.NotificationSender{}
	b := domaincost.DefaultSystemBudget() // SystemDailyCapUSD == 0
	uc := appcost.NewDailyCostReviewUseCase(enforcer, notifier, b, []domaincost.BotID{"bot-1"})

	uc.Review() // must not panic and must fire no notifications

	if len(notifier.SendCalls) != 0 {
		t.Fatalf("expected no notifications got %d", len(notifier.SendCalls))
	}
}

func TestDailyReview_SendError_DoesNotPropagate(t *testing.T) {
	enforcer := &costmocks.CostEnforcer{
		DailySpendFn: func(_ domaincost.BotID) float64 { return 5.0 }, // 50% — spike
	}
	notifier := &notifmocks.NotificationSender{
		SendFn: func(_ notification.Notification) error {
			return errors.New("SNS down")
		},
	}
	budget := defaultBudget()
	uc := appcost.NewDailyCostReviewUseCase(enforcer, notifier, budget, []domaincost.BotID{"bot-1"})

	// Must complete without panicking.
	uc.Review()
}

func TestDailyReview_MultipleBots_MultipleAlerts(t *testing.T) {
	enforcer := &costmocks.CostEnforcer{
		DailySpendFn: func(_ domaincost.BotID) float64 { return 9.0 }, // 90% — flat cap for all
	}
	notifier := &notifmocks.NotificationSender{}
	budget := defaultBudget()
	uc := appcost.NewDailyCostReviewUseCase(enforcer, notifier, budget,
		[]domaincost.BotID{"bot-1", "bot-2", "bot-3"})

	uc.Review()

	if len(notifier.SendCalls) != 3 {
		t.Fatalf("expected 3 notifications got %d", len(notifier.SendCalls))
	}
}
