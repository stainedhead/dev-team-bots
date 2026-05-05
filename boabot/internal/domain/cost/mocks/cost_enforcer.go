// Package mocks provides hand-written test doubles for the cost domain
// interfaces.
package mocks

import "github.com/stainedhead/dev-team-bots/boabot/internal/domain/cost"

// CheckBudgetCall records a single call to CheckBudget.
type CheckBudgetCall struct {
	BotID     cost.BotID
	Tokens    int64
	ToolCalls int
}

// RecordSpendCall records a single call to RecordSpend.
type RecordSpendCall struct {
	BotID     cost.BotID
	Tokens    int64
	ToolCalls int
	USDSpend  float64
}

// CostEnforcer is a hand-written mock of cost.CostEnforcer.
type CostEnforcer struct {
	CheckBudgetFn  func(botID cost.BotID, tokens int64, toolCalls int) error
	DailySpendFn   func(botID cost.BotID) float64
	MonthlySpendFn func() float64

	CheckBudgetCalls []CheckBudgetCall
	RecordSpendCalls []RecordSpendCall
}

func (m *CostEnforcer) CheckBudget(botID cost.BotID, tokens int64, toolCalls int) error {
	m.CheckBudgetCalls = append(m.CheckBudgetCalls, CheckBudgetCall{
		BotID:     botID,
		Tokens:    tokens,
		ToolCalls: toolCalls,
	})
	if m.CheckBudgetFn != nil {
		return m.CheckBudgetFn(botID, tokens, toolCalls)
	}
	return nil
}

func (m *CostEnforcer) RecordSpend(botID cost.BotID, tokens int64, toolCalls int, usdSpend float64) {
	m.RecordSpendCalls = append(m.RecordSpendCalls, RecordSpendCall{
		BotID:     botID,
		Tokens:    tokens,
		ToolCalls: toolCalls,
		USDSpend:  usdSpend,
	})
}

func (m *CostEnforcer) DailySpend(botID cost.BotID) float64 {
	if m.DailySpendFn != nil {
		return m.DailySpendFn(botID)
	}
	return 0
}

func (m *CostEnforcer) MonthlySpend() float64 {
	if m.MonthlySpendFn != nil {
		return m.MonthlySpendFn()
	}
	return 0
}
