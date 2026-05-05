// Package eta defines the domain types and interfaces for estimating and
// calibrating delivery time predictions for work items.
package eta

import (
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"
)

// WorkItemID is the unique identifier of a board work item.
type WorkItemID = workflow.WorkItemID

// ETAResult is the output of an ETAEstimator.Estimate call.
type ETAResult struct {
	// EstimatedMinutes is the predicted duration in wall-clock minutes.
	EstimatedMinutes float64

	// ETAStartAt is the time at which work is estimated to begin.
	ETAStartAt time.Time

	// ETACompleteAt is the time at which the work item is estimated to be done.
	ETACompleteAt time.Time

	// IsSeeded indicates that the estimate was produced using the seed
	// multiplier rather than observed data (not yet enough samples for
	// calibration).
	IsSeeded bool
}

// ETACalibration holds the calibration state for a given task type.
type ETACalibration struct {
	// TaskType is the named category of task (matches the WorkflowStep.Name or
	// bot role).
	TaskType string

	// SeedMultiplier is the ratio of minutes per human man-day used before
	// enough observations have been collected (default 0.015 → 0.015 minutes
	// per human man-day minute, i.e. 1 HMD ≈ 0.015 AI minutes).
	SeedMultiplier float64

	// ObservedRatio is the empirically measured ratio once enough samples are
	// available. Nil when not yet calibrated.
	ObservedRatio *float64

	// CompletedSampleCount is the number of completed items used to compute
	// ObservedRatio.
	CompletedSampleCount int

	// CalibrationThreshold is the minimum number of samples required before
	// ObservedRatio supersedes SeedMultiplier (default 10).
	CalibrationThreshold int
}

// DefaultETACalibration returns an ETACalibration with canonical defaults
// applied.
func DefaultETACalibration(taskType string) ETACalibration {
	return ETACalibration{
		TaskType:             taskType,
		SeedMultiplier:       0.015,
		CalibrationThreshold: 10,
	}
}

// ETAEstimator produces delivery time estimates for work items and accepts
// feedback to calibrate future estimates.
type ETAEstimator interface {
	// Estimate returns an ETAResult for item based on humanManDays of effort.
	Estimate(item WorkItemID, humanManDays float64) ETAResult

	// Calibrate records the actual completion duration for item so that
	// subsequent estimates for the same task type become more accurate.
	Calibrate(item WorkItemID, actualMinutes float64)
}
