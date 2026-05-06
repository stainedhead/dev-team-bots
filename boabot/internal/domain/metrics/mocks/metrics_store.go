// Package mocks provides hand-written test doubles for the metrics domain
// interfaces.
package mocks

import "github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"

// RecordCall records a single call to Record.
type RecordCall struct {
	Event metrics.MetricEvent
}

// MetricsStore is a hand-written mock of metrics.MetricsStore.
type MetricsStore struct {
	BotThroughputFn    func(botID metrics.BotID, period metrics.DateRange) int
	DeliveryAccuracyFn func(botID metrics.BotID, period metrics.DateRange) float64
	CostPerTaskFn      func(botID metrics.BotID, period metrics.DateRange) float64
	StepCycleTimesFn   func(period metrics.DateRange) map[string]float64

	RecordCalls []RecordCall
}

func (m *MetricsStore) Record(event metrics.MetricEvent) {
	m.RecordCalls = append(m.RecordCalls, RecordCall{Event: event})
}

func (m *MetricsStore) BotThroughput(botID metrics.BotID, period metrics.DateRange) int {
	if m.BotThroughputFn != nil {
		return m.BotThroughputFn(botID, period)
	}
	return 0
}

func (m *MetricsStore) DeliveryAccuracy(botID metrics.BotID, period metrics.DateRange) float64 {
	if m.DeliveryAccuracyFn != nil {
		return m.DeliveryAccuracyFn(botID, period)
	}
	return 0
}

func (m *MetricsStore) CostPerTask(botID metrics.BotID, period metrics.DateRange) float64 {
	if m.CostPerTaskFn != nil {
		return m.CostPerTaskFn(botID, period)
	}
	return 0
}

func (m *MetricsStore) StepCycleTimes(period metrics.DateRange) map[string]float64 {
	if m.StepCycleTimesFn != nil {
		return m.StepCycleTimesFn(period)
	}
	return nil
}
