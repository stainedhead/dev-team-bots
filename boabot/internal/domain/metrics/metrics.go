// Package metrics defines the domain types and interfaces for recording
// operational events and generating viability reports for the bot team.
package metrics

import (
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/cost"
)

// BotID is the unique identifier for a bot instance.
type BotID = cost.BotID

// WorkItemID is the unique identifier of a board work item.
type WorkItemID string

// DateRange is an inclusive calendar date range used for reporting periods.
type DateRange = cost.DateRange

// MetricEvent is a single recorded operational event.
type MetricEvent struct {
	// EventType is a namespaced string identifying what happened
	// (e.g. "task.completed", "step.entered", "cost.recorded").
	EventType string

	// BotID is the bot that produced the event.
	BotID BotID

	// ItemID is the work item associated with the event, if applicable.
	ItemID WorkItemID

	// StepName is the workflow step during which the event occurred.
	StepName string

	// DurationMinutes is the elapsed time relevant to the event.
	DurationMinutes float64

	// CostUSD is the USD spend recorded for this event.
	CostUSD float64

	// Timestamp is the UTC time at which the event occurred.
	Timestamp time.Time
}

// BotReport summarises a single bot's performance over a reporting period.
type BotReport struct {
	// BotID identifies the bot this report covers.
	BotID BotID

	// Throughput is the number of work items completed during the period.
	Throughput int

	// DeliveryAccuracy is the fraction of items completed on time (0–1).
	DeliveryAccuracy float64

	// CostPerTask is the average USD cost per completed work item.
	CostPerTask float64

	// RateLimitedMinutes is the total minutes the bot was throttled due to
	// rate-limiting.
	RateLimitedMinutes float64
}

// ViabilityReport is the aggregated team performance report for a period.
type ViabilityReport struct {
	// Period is the date range this report covers.
	Period DateRange

	// BotReports contains one entry per active bot.
	BotReports []BotReport

	// GeneratedAt is the UTC time at which the report was produced.
	GeneratedAt time.Time
}

// MetricsStore records events and provides aggregate query methods used by
// reporting and alerting.
type MetricsStore interface {
	// Record persists a MetricEvent.
	Record(event MetricEvent)

	// BotThroughput returns the number of completed work items for botID in
	// the supplied period.
	BotThroughput(botID BotID, period DateRange) int

	// DeliveryAccuracy returns the fraction of items completed on time (0–1)
	// for botID in the supplied period.
	DeliveryAccuracy(botID BotID, period DateRange) float64

	// CostPerTask returns the average USD cost per completed task for botID in
	// the supplied period.
	CostPerTask(botID BotID, period DateRange) float64

	// StepCycleTimes returns a map of step name → average duration in minutes
	// for all steps completed within the supplied period.
	StepCycleTimes(period DateRange) map[string]float64
}
