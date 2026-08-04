package domain

import "context"

type Agent interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// ChannelMonitor watches an external channel (e.g. Slack, Buzz) and emits
// messages onto the inbound queue for unified processing by the main loop.
type ChannelMonitor interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// HandleResult delivers a completed task's result back to the monitor so
	// it can post a reply on the originating channel. Implementations that
	// did not originate the task (e.g. the task ID is unknown to them) MUST
	// treat the call as a no-op.
	HandleResult(ctx context.Context, p TaskResultPayload)
}

type BotIdentity struct {
	Name     string
	BotType  string
	QueueURL string
}
