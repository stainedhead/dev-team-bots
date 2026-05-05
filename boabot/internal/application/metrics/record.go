// Package metrics contains application-layer use cases for recording metric
// events and generating viability reports.
package metrics

import (
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
)

// RecordMetricUseCase forwards a MetricEvent to the MetricsStore, stamping a
// UTC timestamp when the caller leaves Timestamp at its zero value.
type RecordMetricUseCase struct {
	store metrics.MetricsStore
}

// NewRecordMetricUseCase constructs a RecordMetricUseCase.
func NewRecordMetricUseCase(store metrics.MetricsStore) *RecordMetricUseCase {
	return &RecordMetricUseCase{store: store}
}

// Record persists the event. If the event's Timestamp is zero it is set to
// time.Now() in UTC before recording.
func (u *RecordMetricUseCase) Record(event metrics.MetricEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	u.store.Record(event)
}
