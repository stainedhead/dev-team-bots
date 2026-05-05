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

func defaultBudget() domaincost.SystemBudget {
	b := domaincost.DefaultSystemBudget()
	b.SystemDailyCapUSD = 10.0
	return b
}

func TestEnforceBudget_OK_NoAlert(t *testing.T) {
	enforcer := &costmocks.CostEnforcer{
		DailySpendFn: func(_ domaincost.BotID) float64 { return 1.0 }, // 10% — below spike threshold
	}
	notifier := &notifmocks.NotificationSender{}
	uc := appcost.NewEnforceBudgetUseCase(enforcer, notifier, defaultBudget())

	err := uc.CheckAndRecord("bot-1", 100, 2, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(enforcer.RecordSpendCalls) != 1 {
		t.Fatalf("expected 1 RecordSpend call got %d", len(enforcer.RecordSpendCalls))
	}
	if len(notifier.SendCalls) != 0 {
		t.Fatalf("expected no notifications got %d", len(notifier.SendCalls))
	}
}

func TestEnforceBudget_SpikeAlert(t *testing.T) {
	enforcer := &costmocks.CostEnforcer{
		DailySpendFn: func(_ domaincost.BotID) float64 { return 4.0 }, // 40% — above spike (30%) but below flat-cap (80%)
	}
	notifier := &notifmocks.NotificationSender{}
	uc := appcost.NewEnforceBudgetUseCase(enforcer, notifier, defaultBudget())

	err := uc.CheckAndRecord("bot-1", 100, 2, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notifier.SendCalls) != 1 {
		t.Fatalf("expected 1 notification got %d", len(notifier.SendCalls))
	}
	if notifier.SendCalls[0].Notification.Type != notification.NotifCostSpike {
		t.Fatalf("expected NotifCostSpike got %s", notifier.SendCalls[0].Notification.Type)
	}
}

func TestEnforceBudget_FlatCapAlert(t *testing.T) {
	enforcer := &costmocks.CostEnforcer{
		DailySpendFn: func(_ domaincost.BotID) float64 { return 8.5 }, // 85% — above flat-cap (80%)
	}
	notifier := &notifmocks.NotificationSender{}
	uc := appcost.NewEnforceBudgetUseCase(enforcer, notifier, defaultBudget())

	err := uc.CheckAndRecord("bot-1", 100, 2, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notifier.SendCalls) != 1 {
		t.Fatalf("expected 1 notification got %d", len(notifier.SendCalls))
	}
	if notifier.SendCalls[0].Notification.Type != notification.NotifCostFlatCap {
		t.Fatalf("expected NotifCostFlatCap got %s", notifier.SendCalls[0].Notification.Type)
	}
}

func TestEnforceBudget_BudgetExceeded_ReturnsError(t *testing.T) {
	exceed := &domaincost.BudgetExceededError{BotID: "bot-1", Reason: "token cap"}
	enforcer := &costmocks.CostEnforcer{
		CheckBudgetFn: func(_ domaincost.BotID, _ int64, _ int) error { return exceed },
	}
	notifier := &notifmocks.NotificationSender{}
	uc := appcost.NewEnforceBudgetUseCase(enforcer, notifier, defaultBudget())

	err := uc.CheckAndRecord("bot-1", 99999, 0, 0.0)
	if !errors.Is(err, domaincost.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded got %v", err)
	}
	// Spend must NOT have been recorded.
	if len(enforcer.RecordSpendCalls) != 0 {
		t.Fatalf("expected no RecordSpend calls got %d", len(enforcer.RecordSpendCalls))
	}
}

func TestEnforceBudget_NotificationSendError_DoesNotPropagate(t *testing.T) {
	enforcer := &costmocks.CostEnforcer{
		DailySpendFn: func(_ domaincost.BotID) float64 { return 4.0 }, // spike
	}
	notifier := &notifmocks.NotificationSender{
		SendFn: func(_ notification.Notification) error {
			return errors.New("SNS down")
		},
	}
	uc := appcost.NewEnforceBudgetUseCase(enforcer, notifier, defaultBudget())

	// The use case must still return nil even if notification fails.
	err := uc.CheckAndRecord("bot-1", 100, 2, 1.0)
	if err != nil {
		t.Fatalf("expected nil error despite SNS failure, got: %v", err)
	}
}

func TestEnforceBudget_NoDailyCapConfigured_NoAlert(t *testing.T) {
	b := domaincost.DefaultSystemBudget()
	// SystemDailyCapUSD left at zero — threshold checks must be skipped.
	enforcer := &costmocks.CostEnforcer{
		DailySpendFn: func(_ domaincost.BotID) float64 { return 9999.0 },
	}
	notifier := &notifmocks.NotificationSender{}
	uc := appcost.NewEnforceBudgetUseCase(enforcer, notifier, b)

	err := uc.CheckAndRecord("bot-1", 100, 2, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(notifier.SendCalls) != 0 {
		t.Fatalf("expected no notifications when cap=0 got %d", len(notifier.SendCalls))
	}
}
