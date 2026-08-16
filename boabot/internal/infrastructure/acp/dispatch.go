package acp

import (
	"context"
	"errors"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// errImmediateDispatchUnsupported is returned by NoImmediateDispatchQueue's
// Send, and surfaced to the human as a clear reply (turn.go) rather than a
// hard turn failure.
var errImmediateDispatchUnsupported = errors.New("acp mode does not support immediate task delegation to another bot")

// NoImmediateDispatchQueue implements domain.MessageQueue as a hard,
// intentional boundary for FR-504's ChatTaskManager wiring
// (specs/260816-acp-native-shared-state/spec.md). ACP mode is single-persona
// with no bot-to-bot message routing (architecture.md AD-1/AD-2, this
// feature's Non-Goals) -- unlike native mode's LocalTaskDispatcher, which
// legitimately routes an ASAP-scheduled DirectTask to another bot's queue
// for a different persona's goroutine to pick up. A confirmed ChatTaskManager
// intent that resolves to an ASAP schedule (no time/recurrence signal in the
// message -- e.g. "create a task for the architect") has nowhere to go in
// ACP mode's process; Send fails immediately and predictably instead of
// silently enqueuing a message nothing will ever consume, so turn.go can
// detect it and reply with a clear explanation instead of a raw error.
// Receive/Delete are never reached by LocalTaskDispatcher's synchronous
// DispatchWithSchedule path (only Send is), so they return empty/no-op
// results should anything ever call them. Exported so cmd/boabot/acp.go can
// construct the orchestratorlocal.LocalTaskDispatcher this package's
// ChatTaskManager option needs.
type NoImmediateDispatchQueue struct{}

func (NoImmediateDispatchQueue) Send(_ context.Context, _ string, _ domain.Message) error {
	return errImmediateDispatchUnsupported
}

func (NoImmediateDispatchQueue) Receive(_ context.Context) ([]domain.ReceivedMessage, error) {
	return nil, nil
}

func (NoImmediateDispatchQueue) Delete(_ context.Context, _ string) error {
	return nil
}
