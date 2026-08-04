package buzz

import (
	"context"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// defaultPresenceInterval is used when Config.PresenceInterval is zero.
// Well under FR-023's 180-second staleness bound.
const defaultPresenceInterval = 60 * time.Second

// offlinePublishTimeout bounds F15's offline-presence publish so graceful
// shutdown can't hang indefinitely on an unresponsive relay.
const offlinePublishTimeout = 5 * time.Second

// onConnStateChange is RelayClient's F14 connection-state hook (wired in
// NewMonitor via SetConnStateFunc). It only ever starts or stops the
// presence loop's goroutine -- it never publishes synchronously itself, so
// a slow/blocked relay Publish call can never delay the reconnect
// machinery that invokes this callback.
func (m *Monitor) onConnStateChange(connected bool) {
	m.presenceMu.Lock()
	defer m.presenceMu.Unlock()

	if connected {
		if m.presenceCancel != nil {
			return // already running
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.presenceCancel = cancel
		go m.presenceLoop(ctx)
		return
	}

	// architecture.md "Presence ticker during disconnect": suspended while
	// the connection is down -- there is no connection to publish on.
	if m.presenceCancel != nil {
		m.presenceCancel()
		m.presenceCancel = nil
	}
}

// presenceLoop implements F14: publishes kind:20001 "online" immediately
// (as part of re-establishing state on (re)connect), then on an interval
// under the 180s staleness bound, until ctx is canceled (by
// onConnStateChange, on disconnect or Monitor.Stop).
func (m *Monitor) presenceLoop(ctx context.Context) {
	m.publishPresence(ctx, "online")

	interval := m.cfg.PresenceInterval
	if interval <= 0 {
		interval = defaultPresenceInterval
	}

	t := m.newTicker(interval)
	defer t.stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.c():
			m.publishPresence(ctx, "online")
		}
	}
}

func (m *Monitor) publishPresence(ctx context.Context, status string) {
	evt := domain.Event{Kind: kindPresence, Content: status}
	if err := m.relay.Publish(ctx, evt); err != nil {
		m.logger.Warn("buzz monitor: publish presence failed", "status", status, "err", err)
	}
}

// Stop implements domain.ChannelMonitor / F15: publishes offline presence
// and closes the relay connection cleanly, synchronously, before
// returning -- which is what makes it happen "before the existing
// shutdown path completes" (application.RunAgentUseCase.Shutdown calls
// Stop on every monitor, in order, before it broadcasts the shutdown
// message).
//
// ctx may already be canceled by the time Stop runs (e.g. a SIGTERM
// handler that cancels the root context before calling Shutdown);
// context.WithoutCancel detaches the offline publish from that
// cancellation so it still gets a real (bounded) chance to reach the relay
// instead of failing immediately on an already-Done context.
func (m *Monitor) Stop(ctx context.Context) error {
	m.presenceMu.Lock()
	if m.presenceCancel != nil {
		m.presenceCancel()
		m.presenceCancel = nil
	}
	m.presenceMu.Unlock()

	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), offlinePublishTimeout)
	defer cancel()
	if err := m.relay.Publish(pubCtx, domain.Event{Kind: kindPresence, Content: "offline"}); err != nil {
		m.logger.Warn("buzz monitor: publish offline presence failed", "err", err)
	}

	return m.relay.Close()
}
