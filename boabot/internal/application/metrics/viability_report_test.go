package metrics_test

import (
	"testing"

	appmetrics "github.com/stainedhead/dev-team-bots/boabot/internal/application/metrics"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
	metricsmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics/mocks"
)

func TestGenerateViabilityReport_AllBots(t *testing.T) {
	store := &metricsmocks.MetricsStore{
		BotThroughputFn: func(id metrics.BotID, _ metrics.DateRange) int {
			if id == "bot-1" {
				return 10
			}
			return 5
		},
		DeliveryAccuracyFn: func(_ metrics.BotID, _ metrics.DateRange) float64 { return 0.9 },
		CostPerTaskFn:      func(_ metrics.BotID, _ metrics.DateRange) float64 { return 0.15 },
	}
	uc := appmetrics.NewGenerateViabilityReportUseCase(store,
		[]metrics.BotID{"bot-1", "bot-2"})

	period := metrics.DateRange{Start: "2026-01-01", End: "2026-01-31"}
	report := uc.Generate(period)

	if report.Period.Start != "2026-01-01" {
		t.Fatalf("unexpected period start %s", report.Period.Start)
	}
	if len(report.BotReports) != 2 {
		t.Fatalf("expected 2 bot reports got %d", len(report.BotReports))
	}
	if report.BotReports[0].BotID != "bot-1" {
		t.Fatalf("unexpected BotID %s", report.BotReports[0].BotID)
	}
	if report.BotReports[0].Throughput != 10 {
		t.Fatalf("expected throughput=10 got %d", report.BotReports[0].Throughput)
	}
	if report.BotReports[1].Throughput != 5 {
		t.Fatalf("expected throughput=5 got %d", report.BotReports[1].Throughput)
	}
	if report.GeneratedAt.IsZero() {
		t.Fatal("expected non-zero GeneratedAt")
	}
}

func TestGenerateViabilityReport_NoBots(t *testing.T) {
	store := &metricsmocks.MetricsStore{}
	uc := appmetrics.NewGenerateViabilityReportUseCase(store, nil)

	report := uc.Generate(metrics.DateRange{Start: "2026-01-01", End: "2026-01-31"})
	if len(report.BotReports) != 0 {
		t.Fatalf("expected 0 bot reports got %d", len(report.BotReports))
	}
}

func TestGenerateViabilityReport_DeliveryAccuracy(t *testing.T) {
	store := &metricsmocks.MetricsStore{
		DeliveryAccuracyFn: func(_ metrics.BotID, _ metrics.DateRange) float64 { return 0.75 },
	}
	uc := appmetrics.NewGenerateViabilityReportUseCase(store, []metrics.BotID{"bot-1"})

	report := uc.Generate(metrics.DateRange{})
	if report.BotReports[0].DeliveryAccuracy != 0.75 {
		t.Fatalf("expected 0.75 got %f", report.BotReports[0].DeliveryAccuracy)
	}
}

func TestGenerateViabilityReport_CostPerTask(t *testing.T) {
	store := &metricsmocks.MetricsStore{
		CostPerTaskFn: func(_ metrics.BotID, _ metrics.DateRange) float64 { return 0.42 },
	}
	uc := appmetrics.NewGenerateViabilityReportUseCase(store, []metrics.BotID{"bot-1"})

	report := uc.Generate(metrics.DateRange{})
	if report.BotReports[0].CostPerTask != 0.42 {
		t.Fatalf("expected 0.42 got %f", report.BotReports[0].CostPerTask)
	}
}
