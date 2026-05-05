// Package notification defines the domain types and interfaces for sending
// operational alerts and event notifications to human operators.
package notification

// NotifType is the category of notification being sent.
type NotifType string

const (
	// NotifSuccess indicates a task or item was completed successfully.
	NotifSuccess NotifType = "success"

	// NotifFailure indicates a task or item failed.
	NotifFailure NotifType = "failure"

	// NotifBlocked indicates a bot or item is blocked and cannot make progress.
	NotifBlocked NotifType = "blocked"

	// NotifCostSpike indicates that a bot's spend has crossed the spike-alert
	// threshold.
	NotifCostSpike NotifType = "cost.spike"

	// NotifCostFlatCap indicates that a bot's spend has crossed the flat-cap
	// alert threshold.
	NotifCostFlatCap NotifType = "cost.flat_cap"

	// NotifRateLimited indicates that a bot has been throttled by an upstream
	// provider.
	NotifRateLimited NotifType = "rate_limited"

	// NotifRebalanced indicates that one or more work items have been reassigned
	// by the rebalancing engine.
	NotifRebalanced NotifType = "rebalanced"
)

// Notification is the envelope for a single outbound notification.
type Notification struct {
	// Type categorises the notification for routing and display.
	Type NotifType

	// RecipientARN is the Amazon SNS ARN (or other address) to deliver the
	// notification to.
	RecipientARN string

	// Subject is the short summary displayed in the notification header.
	Subject string

	// Body is the full human-readable message content.
	Body string

	// Metadata is an arbitrary map of key-value pairs attached to the
	// notification for downstream filtering or logging.
	Metadata map[string]string
}

// NotificationSender delivers notifications to human operators.
type NotificationSender interface {
	// Send delivers notification n to the configured recipient. It returns an
	// error when delivery fails; the caller is responsible for retry or
	// dead-letter handling.
	Send(n Notification) error
}
