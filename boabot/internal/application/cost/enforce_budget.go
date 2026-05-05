// Package cost contains application-layer use cases for cost enforcement and
// daily budget review.
package cost

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/cost"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification"
)

// EnforceBudgetUseCase checks and records a bot's spend, firing alert
// notifications when spike or flat-cap thresholds are crossed.
type EnforceBudgetUseCase struct {
	enforcer cost.CostEnforcer
	notifier notification.NotificationSender
	budget   cost.SystemBudget
}

// NewEnforceBudgetUseCase constructs an EnforceBudgetUseCase.
func NewEnforceBudgetUseCase(
	enforcer cost.CostEnforcer,
	notifier notification.NotificationSender,
	budget cost.SystemBudget,
) *EnforceBudgetUseCase {
	return &EnforceBudgetUseCase{
		enforcer: enforcer,
		notifier: notifier,
		budget:   budget,
	}
}

// CheckAndRecord checks the budget for botID before recording spend. If the
// hard cap would be exceeded it returns the error without recording spend. If
// spike or flat-cap thresholds are crossed it fires a notification and
// continues. A nil error means spend was recorded successfully.
func (u *EnforceBudgetUseCase) CheckAndRecord(
	botID cost.BotID,
	tokens int64,
	toolCalls int,
	usdSpend float64,
) error {
	if err := u.enforcer.CheckBudget(botID, tokens, toolCalls); err != nil {
		if errors.Is(err, cost.ErrBudgetExceeded) {
			return fmt.Errorf("enforce budget: %w", err)
		}
	}

	u.enforcer.RecordSpend(botID, tokens, toolCalls, usdSpend)

	daily := u.enforcer.DailySpend(botID)
	if u.budget.SystemDailyCapUSD > 0 {
		pct := daily / u.budget.SystemDailyCapUSD
		if pct >= u.budget.FlatCapAlertThresholdPct {
			u.sendAlert(notification.Notification{
				Type:    notification.NotifCostFlatCap,
				Subject: fmt.Sprintf("Cost flat-cap alert: bot %s", botID),
				Body: fmt.Sprintf("Daily spend %.4f USD (%.0f%% of cap %.4f USD)",
					daily, pct*100, u.budget.SystemDailyCapUSD),
				Metadata: map[string]string{"bot_id": string(botID)},
			})
		} else if pct >= u.budget.SpikeAlertThresholdPct {
			u.sendAlert(notification.Notification{
				Type:    notification.NotifCostSpike,
				Subject: fmt.Sprintf("Cost spike alert: bot %s", botID),
				Body: fmt.Sprintf("Daily spend %.4f USD (%.0f%% of cap %.4f USD)",
					daily, pct*100, u.budget.SystemDailyCapUSD),
				Metadata: map[string]string{"bot_id": string(botID)},
			})
		}
	}

	return nil
}

func (u *EnforceBudgetUseCase) sendAlert(n notification.Notification) {
	if err := u.notifier.Send(n); err != nil {
		slog.Error("failed to send cost alert", "type", n.Type, "err", err)
	}
}
