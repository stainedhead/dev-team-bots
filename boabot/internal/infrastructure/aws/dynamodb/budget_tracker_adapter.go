package dynamodb

import (
	"context"

	domaincost "github.com/stainedhead/dev-team-bots/boabot/internal/domain/cost"
)

// BudgetTrackerAdapter wraps BudgetTracker to satisfy domain.BudgetTracker.
// The domain interface is write-through by design (no in-memory buffer), so
// Flush is a no-op — every CheckAndRecord call writes directly to DynamoDB.
type BudgetTrackerAdapter struct {
	tracker      *BudgetTracker
	botID        string
	perBotCap    domaincost.SystemBudget
	systemBudget domaincost.SystemBudget
}

// NewBudgetTrackerAdapter creates an adapter that routes domain.BudgetTracker
// calls to tracker using botID and the provided budget caps.
func NewBudgetTrackerAdapter(
	tracker *BudgetTracker,
	botID string,
	perBotCap, systemBudget domaincost.SystemBudget,
) *BudgetTrackerAdapter {
	return &BudgetTrackerAdapter{
		tracker:      tracker,
		botID:        botID,
		perBotCap:    perBotCap,
		systemBudget: systemBudget,
	}
}

// CheckAndRecordToolCall checks the budget for one tool call and records it on
// success.
func (a *BudgetTrackerAdapter) CheckAndRecordToolCall(ctx context.Context) error {
	if err := a.tracker.CheckBudget(ctx, a.botID, 0, 1, 0, a.perBotCap, a.systemBudget); err != nil {
		return err
	}
	return a.tracker.RecordSpend(ctx, a.botID, 0, 1, 0)
}

// CheckAndRecordTokens checks the budget for count tokens and records them on
// success.
func (a *BudgetTrackerAdapter) CheckAndRecordTokens(ctx context.Context, count int64) error {
	if err := a.tracker.CheckBudget(ctx, a.botID, count, 0, 0, a.perBotCap, a.systemBudget); err != nil {
		return err
	}
	return a.tracker.RecordSpend(ctx, a.botID, count, 0, 0)
}

// Flush is a no-op: BudgetTracker writes directly to DynamoDB on every call.
func (a *BudgetTrackerAdapter) Flush(_ context.Context) error { return nil }
