package domain

import "context"

// BuzzDispatchResult communicates back to the Buzz ChannelMonitor what
// happened for one inbound qualifying mention, so the monitor knows whether
// there is an immediate text reply to publish (a scheduling confirmation
// prompt, a cancellation acknowledgement, or a "task scheduled" ack) versus
// waiting on the dispatched task's own async result (as with today's plain
// dispatch).
type BuzzDispatchResult struct {
	// TaskID is set whenever a DirectTask was actually created (immediate
	// dispatch, or a confirmed scheduled/recurring dispatch). Empty when the
	// call only produced a text reply (a confirmation prompt, a
	// cancellation) or was a duplicate no-op.
	TaskID string

	// Reply, when non-empty, MUST be published immediately as the Buzz
	// response instead of (or in addition to) waiting on a task result --
	// e.g. a schedule confirmation prompt, a cancellation acknowledgement,
	// or a "task scheduled" ack for a future/recurring dispatch that will
	// not itself produce an immediate task result.
	Reply string

	// AwaitResult is true when the caller should register to receive this
	// TaskID's eventual TaskResultPayload and publish it as a threaded
	// reply (mirrors today's plain-dispatch behaviour). False for
	// future/recurring dispatches, whose actual execution happens later,
	// outside of this call.
	AwaitResult bool

	// Duplicate is true when this event had already been dispatched (relay
	// redelivery/reconnect replay) and no new work was created.
	Duplicate bool
}

// BuzzTaskDispatcher is the seam the Buzz ChannelMonitor uses to hand off an
// inbound qualifying mention to the orchestrator's Dispatcher/
// DirectTaskStore/BoardStore pipeline (and the NL scheduling/confirm-cancel
// flow), instead of calling Worker.Execute directly. Implemented in
// internal/application/orchestrator (BuzzTaskBridge); consumed by
// internal/infrastructure/buzz.Monitor via the WithTaskDispatcher option.
type BuzzTaskDispatcher interface {
	// Dispatch handles one inbound Buzz mention. eventID is the triggering
	// Nostr event ID (used for relay-replay dedup); threadID is a stable
	// per-conversation key used for the NL-scheduling confirm/cancel flow
	// (e.g. the Buzz channel UUID).
	Dispatch(ctx context.Context, botName, eventID, threadID, instruction string) (BuzzDispatchResult, error)
}
