package metrics_test

import (
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics/mocks"
)

func TestMetricEvent_Fields(t *testing.T) {
	e := metrics.MetricEvent{
		EventType:       "task.completed",
		BotID:           "bot-1",
		ItemID:          "item-42",
		StepName:        "implement",
		DurationMinutes: 12.5,
		CostUSD:         0.03,
		Timestamp:       time.Now(),
	}
	if e.EventType != "task.completed" {
		t.Fatalf("unexpected EventType %s", e.EventType)
	}
	if e.DurationMinutes != 12.5 {
		t.Fatalf("unexpected DurationMinutes %f", e.DurationMinutes)
	}
}

func TestBotReport_Fields(t *testing.T) {
	r := metrics.BotReport{
		BotID:              "bot-1",
		Throughput:         42,
		DeliveryAccuracy:   0.95,
		CostPerTask:        0.12,
		RateLimitedMinutes: 3.5,
	}
	if r.Throughput != 42 {
		t.Fatalf("unexpected Throughput %d", r.Throughput)
	}
	if r.DeliveryAccuracy != 0.95 {
		t.Fatalf("unexpected DeliveryAccuracy %f", r.DeliveryAccuracy)
	}
}

func TestViabilityReport_Fields(t *testing.T) {
	now := time.Now()
	vr := metrics.ViabilityReport{
		Period:      metrics.DateRange{Start: "2026-01-01", End: "2026-01-31"},
		BotReports:  []metrics.BotReport{{BotID: "bot-1", Throughput: 10}},
		GeneratedAt: now,
	}
	if len(vr.BotReports) != 1 {
		t.Fatalf("expected 1 bot report got %d", len(vr.BotReports))
	}
}

func TestMetricsStoreMock_Record(t *testing.T) {
	m := &mocks.MetricsStore{}
	event := metrics.MetricEvent{EventType: "step.entered", BotID: "bot-1"}
	m.Record(event)
	if len(m.RecordCalls) != 1 {
		t.Fatalf("expected 1 call got %d", len(m.RecordCalls))
	}
	if m.RecordCalls[0].Event.EventType != "step.entered" {
		t.Fatalf("unexpected EventType %s", m.RecordCalls[0].Event.EventType)
	}
}

func TestMetricsStoreMock_BotThroughput(t *testing.T) {
	m := &mocks.MetricsStore{
		BotThroughputFn: func(_ metrics.BotID, _ metrics.DateRange) int { return 7 },
	}
	got := m.BotThroughput("bot-1", metrics.DateRange{Start: "2026-01-01", End: "2026-01-31"})
	if got != 7 {
		t.Fatalf("expected 7 got %d", got)
	}
}

func TestMetricsStoreMock_DeliveryAccuracy(t *testing.T) {
	m := &mocks.MetricsStore{
		DeliveryAccuracyFn: func(_ metrics.BotID, _ metrics.DateRange) float64 { return 0.88 },
	}
	got := m.DeliveryAccuracy("bot-1", metrics.DateRange{})
	if got != 0.88 {
		t.Fatalf("expected 0.88 got %f", got)
	}
}

func TestMetricsStoreMock_CostPerTask(t *testing.T) {
	m := &mocks.MetricsStore{
		CostPerTaskFn: func(_ metrics.BotID, _ metrics.DateRange) float64 { return 0.25 },
	}
	got := m.CostPerTask("bot-1", metrics.DateRange{})
	if got != 0.25 {
		t.Fatalf("expected 0.25 got %f", got)
	}
}

func TestMetricsStoreMock_StepCycleTimes(t *testing.T) {
	m := &mocks.MetricsStore{
		StepCycleTimesFn: func(_ metrics.DateRange) map[string]float64 {
			return map[string]float64{"implement": 30.0, "review": 10.0}
		},
	}
	got := m.StepCycleTimes(metrics.DateRange{})
	if got["implement"] != 30.0 {
		t.Fatalf("expected 30.0 got %f", got["implement"])
	}
}

func TestMetricsStoreMock_Defaults(t *testing.T) {
	m := &mocks.MetricsStore{}
	if m.BotThroughput("bot", metrics.DateRange{}) != 0 {
		t.Fatal("expected 0 default throughput")
	}
	if m.DeliveryAccuracy("bot", metrics.DateRange{}) != 0 {
		t.Fatal("expected 0 default delivery accuracy")
	}
	if m.CostPerTask("bot", metrics.DateRange{}) != 0 {
		t.Fatal("expected 0 default cost per task")
	}
	if m.StepCycleTimes(metrics.DateRange{}) != nil {
		t.Fatal("expected nil default step cycle times")
	}
}
