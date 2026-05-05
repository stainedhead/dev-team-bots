package eta_test

import (
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/eta"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/eta/mocks"
)

func TestDefaultETACalibration(t *testing.T) {
	c := eta.DefaultETACalibration("implement")
	if c.TaskType != "implement" {
		t.Fatalf("unexpected TaskType %s", c.TaskType)
	}
	if c.SeedMultiplier != 0.015 {
		t.Fatalf("expected SeedMultiplier=0.015 got %f", c.SeedMultiplier)
	}
	if c.CalibrationThreshold != 10 {
		t.Fatalf("expected CalibrationThreshold=10 got %d", c.CalibrationThreshold)
	}
	if c.ObservedRatio != nil {
		t.Fatal("expected ObservedRatio=nil before calibration")
	}
	if c.CompletedSampleCount != 0 {
		t.Fatalf("expected 0 samples got %d", c.CompletedSampleCount)
	}
}

func TestETAResult_Fields(t *testing.T) {
	now := time.Now()
	r := eta.ETAResult{
		EstimatedMinutes: 42.5,
		ETAStartAt:       now,
		ETACompleteAt:    now.Add(42*time.Minute + 30*time.Second),
		IsSeeded:         true,
	}
	if r.EstimatedMinutes != 42.5 {
		t.Fatalf("unexpected EstimatedMinutes %f", r.EstimatedMinutes)
	}
	if !r.IsSeeded {
		t.Fatal("expected IsSeeded=true")
	}
}

func TestETAEstimatorMock_Estimate_Default(t *testing.T) {
	m := &mocks.ETAEstimator{}
	result := m.Estimate("item-1", 5.0)
	if !result.IsSeeded {
		t.Fatal("expected default result to be seeded")
	}
	if len(m.EstimateCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.EstimateCalls))
	}
	if m.EstimateCalls[0].HumanManDays != 5.0 {
		t.Fatalf("unexpected HumanManDays %f", m.EstimateCalls[0].HumanManDays)
	}
}

func TestETAEstimatorMock_Estimate_Custom(t *testing.T) {
	m := &mocks.ETAEstimator{
		EstimateFn: func(_ eta.WorkItemID, hmd float64) eta.ETAResult {
			return eta.ETAResult{EstimatedMinutes: hmd * 0.015, IsSeeded: true}
		},
	}
	result := m.Estimate("item-2", 100.0)
	if result.EstimatedMinutes != 1.5 {
		t.Fatalf("expected 1.5 got %f", result.EstimatedMinutes)
	}
}

func TestETAEstimatorMock_Calibrate(t *testing.T) {
	var recorded float64
	m := &mocks.ETAEstimator{
		CalibrateFn: func(_ eta.WorkItemID, actual float64) { recorded = actual },
	}
	m.Calibrate("item-3", 37.0)
	if recorded != 37.0 {
		t.Fatalf("expected 37.0 got %f", recorded)
	}
	if len(m.CalibrateCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.CalibrateCalls))
	}
}

func TestETAEstimatorMock_Calibrate_NoFn(t *testing.T) {
	m := &mocks.ETAEstimator{}
	// Must not panic when CalibrateFn is nil.
	m.Calibrate("item-4", 10.0)
	if len(m.CalibrateCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.CalibrateCalls))
	}
}
