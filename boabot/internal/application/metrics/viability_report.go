package metrics

import (
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
)

// GenerateViabilityReportUseCase produces a ViabilityReport for a given
// period by querying the MetricsStore for each known bot.
type GenerateViabilityReportUseCase struct {
	store  metrics.MetricsStore
	botIDs []metrics.BotID
}

// NewGenerateViabilityReportUseCase constructs a GenerateViabilityReportUseCase.
func NewGenerateViabilityReportUseCase(store metrics.MetricsStore, botIDs []metrics.BotID) *GenerateViabilityReportUseCase {
	return &GenerateViabilityReportUseCase{store: store, botIDs: botIDs}
}

// Generate queries the store for each bot and assembles a ViabilityReport.
func (u *GenerateViabilityReportUseCase) Generate(period metrics.DateRange) metrics.ViabilityReport {
	botReports := make([]metrics.BotReport, 0, len(u.botIDs))
	for _, id := range u.botIDs {
		botReports = append(botReports, metrics.BotReport{
			BotID:            id,
			Throughput:       u.store.BotThroughput(id, period),
			DeliveryAccuracy: u.store.DeliveryAccuracy(id, period),
			CostPerTask:      u.store.CostPerTask(id, period),
		})
	}
	return metrics.ViabilityReport{
		Period:      period,
		BotReports:  botReports,
		GeneratedAt: time.Now().UTC(),
	}
}
