package domain

import "context"

// BudgetTracker enforces token spend and tool call caps.
// Counters are held in memory and flushed to DynamoDB every 30 seconds.
// On startup, counters are seeded from DynamoDB so caps survive restarts.
type BudgetTracker interface {
	// CheckAndRecordTokens returns an error if recording count tokens would
	// exceed the daily token spend cap. On success the spend is recorded.
	CheckAndRecordTokens(ctx context.Context, count int64) error

	// CheckAndRecordToolCall returns an error if recording one tool call would
	// exceed the hourly tool call cap. On success the call is recorded.
	CheckAndRecordToolCall(ctx context.Context) error

	// Flush persists the current in-memory counters to DynamoDB.
	Flush(ctx context.Context) error
}
