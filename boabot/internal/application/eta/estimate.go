// Package eta contains the application-layer use case for ETA estimation and
// calibration.
package eta

import (
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/eta"
)

// EstimateETAUseCase delegates ETA queries and calibration feedback to the
// domain ETAEstimator.
type EstimateETAUseCase struct {
	estimator eta.ETAEstimator
}

// NewEstimateETAUseCase constructs an EstimateETAUseCase.
func NewEstimateETAUseCase(estimator eta.ETAEstimator) *EstimateETAUseCase {
	return &EstimateETAUseCase{estimator: estimator}
}

// Estimate returns an ETAResult for itemID based on the supplied humanManDays
// of effort.
func (u *EstimateETAUseCase) Estimate(itemID eta.WorkItemID, humanManDays float64) eta.ETAResult {
	return u.estimator.Estimate(itemID, humanManDays)
}

// Calibrate records the actual completion time for itemID so that the
// estimator can improve future predictions.
func (u *EstimateETAUseCase) Calibrate(itemID eta.WorkItemID, actualMinutes float64) {
	u.estimator.Calibrate(itemID, actualMinutes)
}
