package domain

import "context"

type Agent interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// ChannelMonitor watches an external channel (Slack, Teams) and emits messages
// onto the inbound queue for unified processing by the main loop.
type ChannelMonitor interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type BotIdentity struct {
	Name     string
	BotType  string
	QueueURL string
}
