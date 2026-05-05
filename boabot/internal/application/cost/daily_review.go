package cost

import (
	"fmt"
	"log/slog"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/cost"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification"
)

// DailyCostReviewUseCase checks all bots' spend against the pro-rated daily
// budget and fires spike or flat-cap notifications as appropriate.
type DailyCostReviewUseCase struct {
	enforcer cost.CostEnforcer
	notifier notification.NotificationSender
	budget   cost.SystemBudget
	botIDs   []cost.BotID
}

// NewDailyCostReviewUseCase constructs a DailyCostReviewUseCase.
func NewDailyCostReviewUseCase(
	enforcer cost.CostEnforcer,
	notifier notification.NotificationSender,
	budget cost.SystemBudget,
	botIDs []cost.BotID,
) *DailyCostReviewUseCase {
	return &DailyCostReviewUseCase{
		enforcer: enforcer,
		notifier: notifier,
		budget:   budget,
		botIDs:   botIDs,
	}
}

// Review checks each known bot's daily spend against the system budget and
// fires alerts for bots that have crossed threshold boundaries.
func (u *DailyCostReviewUseCase) Review() {
	if u.budget.SystemDailyCapUSD <= 0 {
		return
	}

	for _, botID := range u.botIDs {
		daily := u.enforcer.DailySpend(botID)
		pct := daily / u.budget.SystemDailyCapUSD

		var notifType notification.NotifType
		var threshold float64

		switch {
		case pct >= u.budget.FlatCapAlertThresholdPct:
			notifType = notification.NotifCostFlatCap
			threshold = u.budget.FlatCapAlertThresholdPct
		case pct >= u.budget.SpikeAlertThresholdPct:
			notifType = notification.NotifCostSpike
			threshold = u.budget.SpikeAlertThresholdPct
		default:
			continue
		}

		n := notification.Notification{
			Type:    notifType,
			Subject: fmt.Sprintf("Daily cost review: bot %s", botID),
			Body: fmt.Sprintf(
				"Daily spend %.4f USD (%.0f%% of cap %.4f USD, threshold %.0f%%)",
				daily, pct*100, u.budget.SystemDailyCapUSD, threshold*100,
			),
			Metadata: map[string]string{"bot_id": string(botID)},
		}
		if err := u.notifier.Send(n); err != nil {
			slog.Error("failed to send daily review alert",
				"bot_id", botID, "type", notifType, "err", err)
		}
	}
}
