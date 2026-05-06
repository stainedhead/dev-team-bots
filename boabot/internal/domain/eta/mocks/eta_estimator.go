// Package mocks provides hand-written test doubles for the eta domain
// interfaces.
package mocks

import "github.com/stainedhead/dev-team-bots/boabot/internal/domain/eta"

// EstimateCall records a single call to Estimate.
type EstimateCall struct {
	Item         eta.WorkItemID
	HumanManDays float64
}

// CalibrateCall records a single call to Calibrate.
type CalibrateCall struct {
	Item          eta.WorkItemID
	ActualMinutes float64
}

// ETAEstimator is a hand-written mock of eta.ETAEstimator.
type ETAEstimator struct {
	EstimateFn  func(item eta.WorkItemID, humanManDays float64) eta.ETAResult
	CalibrateFn func(item eta.WorkItemID, actualMinutes float64)

	EstimateCalls  []EstimateCall
	CalibrateCalls []CalibrateCall
}

func (m *ETAEstimator) Estimate(item eta.WorkItemID, humanManDays float64) eta.ETAResult {
	m.EstimateCalls = append(m.EstimateCalls, EstimateCall{Item: item, HumanManDays: humanManDays})
	if m.EstimateFn != nil {
		return m.EstimateFn(item, humanManDays)
	}
	return eta.ETAResult{IsSeeded: true}
}

func (m *ETAEstimator) Calibrate(item eta.WorkItemID, actualMinutes float64) {
	m.CalibrateCalls = append(m.CalibrateCalls, CalibrateCall{Item: item, ActualMinutes: actualMinutes})
	if m.CalibrateFn != nil {
		m.CalibrateFn(item, actualMinutes)
	}
}
