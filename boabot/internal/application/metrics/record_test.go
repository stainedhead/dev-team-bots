package metrics_test

import (
	"testing"
	"time"

	appmetrics "github.com/stainedhead/dev-team-bots/boabot/internal/application/metrics"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
	metricsmocks "github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics/mocks"
)

func TestRecordMetric_Delegates(t *testing.T) {
	store := &metricsmocks.MetricsStore{}
	uc := appmetrics.NewRecordMetricUseCase(store)

	event := metrics.MetricEvent{
		EventType: "task.completed",
		BotID:     "bot-1",
		Timestamp: time.Now(),
	}
	uc.Record(event)

	if len(store.RecordCalls) != 1 {
		t.Fatalf("expected 1 record call got %d", len(store.RecordCalls))
	}
	if store.RecordCalls[0].Event.EventType != "task.completed" {
		t.Fatalf("unexpected EventType %s", store.RecordCalls[0].Event.EventType)
	}
}

func TestRecordMetric_StampsTimestampWhenZero(t *testing.T) {
	store := &metricsmocks.MetricsStore{}
	uc := appmetrics.NewRecordMetricUseCase(store)

	event := metrics.MetricEvent{
		EventType: "step.entered",
		BotID:     "bot-2",
		// Timestamp intentionally left at zero value.
	}
	before := time.Now().UTC()
	uc.Record(event)
	after := time.Now().UTC()

	if len(store.RecordCalls) != 1 {
		t.Fatalf("expected 1 record call got %d", len(store.RecordCalls))
	}
	recorded := store.RecordCalls[0].Event.Timestamp
	if recorded.IsZero() {
		t.Fatal("expected Timestamp to be stamped, got zero")
	}
	if recorded.Before(before) || recorded.After(after) {
		t.Fatalf("expected timestamp between %v and %v, got %v", before, after, recorded)
	}
}

func TestRecordMetric_PreservesProvidedTimestamp(t *testing.T) {
	store := &metricsmocks.MetricsStore{}
	uc := appmetrics.NewRecordMetricUseCase(store)

	ts := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	event := metrics.MetricEvent{EventType: "cost.recorded", Timestamp: ts}
	uc.Record(event)

	recorded := store.RecordCalls[0].Event.Timestamp
	if !recorded.Equal(ts) {
		t.Fatalf("expected preserved timestamp %v got %v", ts, recorded)
	}
}
