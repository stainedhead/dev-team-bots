package eta_test

import (
	"testing"
	"time"

	appeta "github.com/stainedhead/dev-team-bots/boabot/internal/application/eta"
	domaineta "github.com/stainedhead/dev-team-bots/boabot/internal/domain/eta"
	etamocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/eta/mocks"
)

func TestEstimateETA_Delegates(t *testing.T) {
	now := time.Now()
	expected := domaineta.ETAResult{
		EstimatedMinutes: 15.0,
		ETAStartAt:       now,
		ETACompleteAt:    now.Add(15 * time.Minute),
		IsSeeded:         true,
	}
	m := &etamocks.ETAEstimator{
		EstimateFn: func(_ domaineta.WorkItemID, _ float64) domaineta.ETAResult {
			return expected
		},
	}
	uc := appeta.NewEstimateETAUseCase(m)

	got := uc.Estimate("item-1", 10.0)
	if got.EstimatedMinutes != 15.0 {
		t.Fatalf("expected 15.0 got %f", got.EstimatedMinutes)
	}
	if len(m.EstimateCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.EstimateCalls))
	}
	if m.EstimateCalls[0].Item != "item-1" {
		t.Fatalf("unexpected item %s", m.EstimateCalls[0].Item)
	}
}

func TestEstimateETA_Calibrate_Delegates(t *testing.T) {
	m := &etamocks.ETAEstimator{}
	uc := appeta.NewEstimateETAUseCase(m)

	uc.Calibrate("item-2", 22.5)
	if len(m.CalibrateCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.CalibrateCalls))
	}
	if m.CalibrateCalls[0].ActualMinutes != 22.5 {
		t.Fatalf("expected 22.5 got %f", m.CalibrateCalls[0].ActualMinutes)
	}
}

func TestEstimateETA_IsSeeded_Flag(t *testing.T) {
	m := &etamocks.ETAEstimator{} // default returns IsSeeded=true
	uc := appeta.NewEstimateETAUseCase(m)
	got := uc.Estimate("item-3", 1.0)
	if !got.IsSeeded {
		t.Fatal("expected IsSeeded=true for default mock result")
	}
}
