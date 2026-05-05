// Package cost defines the domain types and interfaces for per-bot and
// system-level cost enforcement and reporting.
package cost

import (
	"errors"
	"fmt"
)

// BotID is the unique identifier for a bot instance.
type BotID string

// DateRange is an inclusive calendar date range used for reporting periods.
type DateRange struct {
	// Start is the first day of the period (UTC, truncated to midnight).
	Start string // "YYYY-MM-DD"
	// End is the last day of the period (UTC, truncated to midnight).
	End string // "YYYY-MM-DD"
}

// SystemBudget holds the system-wide cost caps and alert thresholds.
type SystemBudget struct {
	// SystemDailyCapUSD is the maximum total USD spend allowed per day across
	// all bots.
	SystemDailyCapUSD float64

	// SystemMonthlyCapUSD is the maximum total USD spend allowed per calendar
	// month across all bots.
	SystemMonthlyCapUSD float64

	// SpikeAlertThresholdPct is the fraction of the daily cap at which a spike
	// alert is fired (default 0.30).
	SpikeAlertThresholdPct float64

	// FlatCapAlertThresholdPct is the fraction of the daily cap at which a
	// flat-cap alert is fired (default 0.80).
	FlatCapAlertThresholdPct float64
}

// DefaultSystemBudget returns a SystemBudget with the canonical default
// thresholds applied. Callers should override cap values for their deployment.
func DefaultSystemBudget() SystemBudget {
	return SystemBudget{
		SpikeAlertThresholdPct:   0.30,
		FlatCapAlertThresholdPct: 0.80,
	}
}

// BudgetExceededError is returned when a bot's spend would exceed the
// configured hard cap for tokens or tool calls.
type BudgetExceededError struct {
	BotID   BotID
	Reason  string
	Current float64
	Cap     float64
}

func (e *BudgetExceededError) Error() string {
	return fmt.Sprintf("cost: budget exceeded for bot %q: %s (current=%.4f cap=%.4f)",
		e.BotID, e.Reason, e.Current, e.Cap)
}

// ErrBudgetExceeded is a sentinel that callers can use with errors.Is after
// unwrapping a BudgetExceededError.
var ErrBudgetExceeded = errors.New("cost: budget exceeded")

func (e *BudgetExceededError) Is(target error) bool { return target == ErrBudgetExceeded }

// SpikeAlertError is returned (or wrapped alongside a nil error) when a bot's
// spend crosses the spike-alert threshold for the day.
type SpikeAlertError struct {
	BotID        BotID
	DailySpend   float64
	DailyCap     float64
	ThresholdPct float64
}

func (e *SpikeAlertError) Error() string {
	return fmt.Sprintf("cost: spike alert for bot %q: daily spend %.4f exceeds %.0f%% of cap %.4f",
		e.BotID, e.DailySpend, e.ThresholdPct*100, e.DailyCap)
}

// ErrSpikeAlert is the sentinel for SpikeAlertError.
var ErrSpikeAlert = errors.New("cost: spike alert")

func (e *SpikeAlertError) Is(target error) bool { return target == ErrSpikeAlert }

// FlatCapAlertError is returned when a bot's spend crosses the flat-cap alert
// threshold for the day.
type FlatCapAlertError struct {
	BotID        BotID
	DailySpend   float64
	DailyCap     float64
	ThresholdPct float64
}

func (e *FlatCapAlertError) Error() string {
	return fmt.Sprintf("cost: flat-cap alert for bot %q: daily spend %.4f exceeds %.0f%% of cap %.4f",
		e.BotID, e.DailySpend, e.ThresholdPct*100, e.DailyCap)
}

// ErrFlatCapAlert is the sentinel for FlatCapAlertError.
var ErrFlatCapAlert = errors.New("cost: flat-cap alert")

func (e *FlatCapAlertError) Is(target error) bool { return target == ErrFlatCapAlert }

// CostEnforcer checks and records per-bot cost spend and provides aggregate
// totals for review and alerting.
type CostEnforcer interface {
	// CheckBudget returns an error if recording tokens and toolCalls for botID
	// would exceed the configured budget caps. The spend is NOT recorded on
	// error — the caller must decide how to handle the failure.
	CheckBudget(botID BotID, tokens int64, toolCalls int) error

	// RecordSpend atomically adds the given token count, tool call count, and
	// USD spend to the running totals for botID. It never returns an error; if
	// the underlying store is unavailable the implementation must buffer or log.
	RecordSpend(botID BotID, tokens int64, toolCalls int, usdSpend float64)

	// DailySpend returns the total USD recorded for botID today (UTC).
	DailySpend(botID BotID) float64

	// MonthlySpend returns the total USD recorded for all bots in the current
	// calendar month (UTC).
	MonthlySpend() float64
}
