package cost_test

import (
	"errors"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/cost"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/cost/mocks"
)

func TestDefaultSystemBudget(t *testing.T) {
	b := cost.DefaultSystemBudget()
	if b.SpikeAlertThresholdPct != 0.30 {
		t.Fatalf("expected SpikeAlertThresholdPct=0.30, got %f", b.SpikeAlertThresholdPct)
	}
	if b.FlatCapAlertThresholdPct != 0.80 {
		t.Fatalf("expected FlatCapAlertThresholdPct=0.80, got %f", b.FlatCapAlertThresholdPct)
	}
}

func TestBudgetExceededError(t *testing.T) {
	err := &cost.BudgetExceededError{
		BotID:   "bot-1",
		Reason:  "daily token limit",
		Current: 1500.0,
		Cap:     1000.0,
	}
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
	if !errors.Is(err, cost.ErrBudgetExceeded) {
		t.Fatal("expected errors.Is match for ErrBudgetExceeded")
	}
}

func TestSpikeAlertError(t *testing.T) {
	err := &cost.SpikeAlertError{
		BotID:        "bot-2",
		DailySpend:   3.50,
		DailyCap:     10.0,
		ThresholdPct: 0.30,
	}
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
	if !errors.Is(err, cost.ErrSpikeAlert) {
		t.Fatal("expected errors.Is match for ErrSpikeAlert")
	}
}

func TestFlatCapAlertError(t *testing.T) {
	err := &cost.FlatCapAlertError{
		BotID:        "bot-3",
		DailySpend:   8.50,
		DailyCap:     10.0,
		ThresholdPct: 0.80,
	}
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
	if !errors.Is(err, cost.ErrFlatCapAlert) {
		t.Fatal("expected errors.Is match for ErrFlatCapAlert")
	}
}

func TestCostEnforcerMock_CheckBudget_OK(t *testing.T) {
	m := &mocks.CostEnforcer{}
	err := m.CheckBudget("bot-1", 100, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.CheckBudgetCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.CheckBudgetCalls))
	}
}

func TestCostEnforcerMock_CheckBudget_Exceeded(t *testing.T) {
	exceed := &cost.BudgetExceededError{BotID: "bot-1", Reason: "token cap"}
	m := &mocks.CostEnforcer{
		CheckBudgetFn: func(_ cost.BotID, _ int64, _ int) error { return exceed },
	}
	err := m.CheckBudget("bot-1", 99999, 0)
	if !errors.Is(err, cost.ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded got %v", err)
	}
}

func TestCostEnforcerMock_RecordSpend(t *testing.T) {
	m := &mocks.CostEnforcer{}
	m.RecordSpend("bot-1", 200, 3, 0.05)
	if len(m.RecordSpendCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.RecordSpendCalls))
	}
	c := m.RecordSpendCalls[0]
	if c.USDSpend != 0.05 {
		t.Fatalf("expected USDSpend=0.05 got %f", c.USDSpend)
	}
}

func TestCostEnforcerMock_DailySpend(t *testing.T) {
	m := &mocks.CostEnforcer{
		DailySpendFn: func(_ cost.BotID) float64 { return 4.20 },
	}
	got := m.DailySpend("bot-1")
	if got != 4.20 {
		t.Fatalf("expected 4.20 got %f", got)
	}
}

func TestCostEnforcerMock_MonthlySpend(t *testing.T) {
	m := &mocks.CostEnforcer{
		MonthlySpendFn: func() float64 { return 99.99 },
	}
	got := m.MonthlySpend()
	if got != 99.99 {
		t.Fatalf("expected 99.99 got %f", got)
	}
}

func TestDateRange_Fields(t *testing.T) {
	dr := cost.DateRange{Start: "2026-01-01", End: "2026-01-31"}
	if dr.Start != "2026-01-01" {
		t.Fatalf("unexpected Start %s", dr.Start)
	}
	if dr.End != "2026-01-31" {
		t.Fatalf("unexpected End %s", dr.End)
	}
}
