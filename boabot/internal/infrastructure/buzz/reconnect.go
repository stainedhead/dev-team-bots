package buzz

import (
	"context"
	"math/rand"
	"time"
)

// BackoffConfig bounds D8's reconnect backoff: delays grow exponentially
// from Base, capped at Max, and are randomized (full jitter within
// [0, cappedDelay)) so a relay restart doesn't cause every connected bot to
// hammer it in lockstep.
type BackoffConfig struct {
	Base time.Duration
	Max  time.Duration
}

// DefaultBackoffConfig is used when no BackoffConfig is supplied.
var DefaultBackoffConfig = BackoffConfig{Base: time.Second, Max: 30 * time.Second}

// maxBackoffShift bounds the exponent so 1<<shift never overflows
// time.Duration for any plausible attempt count.
const maxBackoffShift = 20

// delay computes the backoff duration for the given zero-based attempt
// number, scaled by jitter (expected range [0,1)).
func (b BackoffConfig) delay(attempt int, jitter float64) time.Duration {
	base := b.Base
	if base <= 0 {
		base = DefaultBackoffConfig.Base
	}
	max := b.Max
	if max <= 0 {
		max = DefaultBackoffConfig.Max
	}
	if jitter < 0 {
		jitter = 0
	}
	if jitter > 1 {
		jitter = 1
	}

	shift := attempt
	if shift > maxBackoffShift {
		shift = maxBackoffShift
	}
	if shift < 0 {
		shift = 0
	}

	d := base * time.Duration(int64(1)<<uint(shift))
	if d <= 0 || d > max {
		d = max
	}
	return time.Duration(float64(d) * jitter)
}

// defaultJitter returns a value in [0.5, 1.0): always some backoff on
// retry (never a busy loop), still randomized to avoid a reconnect
// thundering herd.
func defaultJitter() float64 {
	return 0.5 + rand.Float64()*0.5 //nolint:gosec // backoff jitter, not security-sensitive
}

// watchLoop watches the current connection for disconnection and drives
// reconnection (FR-012) for the lifetime of the RelayClient. It is started
// once, by the first successful Connect.
func (rc *RelayClient) watchLoop() {
	for {
		rc.mu.Lock()
		conn := rc.conn
		closed := rc.closed
		rc.mu.Unlock()
		if closed || conn == nil {
			return
		}

		select {
		case <-conn.Done():
		case <-rc.closedCh:
			return
		}

		rc.mu.Lock()
		closed = rc.closed
		rc.mu.Unlock()
		if closed {
			return
		}

		// F14 prerequisite: the connection is lost -- signal disconnected
		// before the (possibly lengthy, backed-off) reconnect loop starts,
		// so a presence loop hooked in via SetConnStateFunc suspends
		// immediately rather than continuing to try to publish on a dead
		// connection.
		rc.notifyConnState(false)

		if rc.reconnect() == nil {
			return
		}
	}
}

// reconnect retries dialing and re-authenticating, with bounded
// exponential backoff and jitter, until it succeeds or the RelayClient is
// closed (in which case it returns nil). On success it re-subscribes every
// registered subscription (FR-012) and stores the new connection before
// returning it.
func (rc *RelayClient) reconnect() relayConn {
	attempt := 0
	for {
		rc.mu.Lock()
		closed := rc.closed
		rc.mu.Unlock()
		if closed {
			return nil
		}

		rc.sleep(rc.backoff.delay(attempt, rc.jitter()))

		rc.mu.Lock()
		closed = rc.closed
		rc.mu.Unlock()
		if closed {
			return nil
		}

		conn, err := rc.dial(context.Background(), rc.url, rc.dialOpts())
		if err != nil {
			rc.logger.Warn("buzz: reconnect dial failed", "attempt", attempt, "url", rc.url, "err", err)
			attempt++
			continue
		}

		authCtx, cancel := context.WithTimeout(context.Background(), rc.authTimeout)
		authErr := rc.authenticateOn(authCtx, conn)
		cancel()
		if authErr != nil {
			rc.logger.Warn("buzz: reconnect authenticate failed", "attempt", attempt, "err", authErr)
			_ = conn.Close()
			attempt++
			continue
		}

		rc.mu.Lock()
		if rc.closed {
			rc.mu.Unlock()
			_ = conn.Close()
			return nil
		}
		rc.conn = conn
		rc.mu.Unlock()

		rc.resubscribeAll(conn)
		rc.logger.Info("buzz: reconnected", "url", rc.url, "attempts", attempt+1)
		// F14 prerequisite: the "reconnected" signal itself already fired
		// inside authenticateOn (above), right when auth succeeded -- not
		// here -- so it fires uniformly for both this reconnect path and the
		// initial Connect->Authenticate sequence. See authenticateOn's doc
		// comment for why authentication, not just re-dialing, is the right
		// trigger.
		return conn
	}
}

// resubscribeAll re-attaches every currently registered subscription to
// conn, in the caller-owned channel each was originally returned on, so
// no pending correlation is lost across a reconnect (architecture.md
// "Pending map across reconnect" edge case; the pending map itself is a
// later phase's concern, but the channel continuity this relies on is
// built here).
func (rc *RelayClient) resubscribeAll(conn relayConn) {
	rc.subMu.Lock()
	entries := make([]*subEntry, 0, len(rc.subs))
	for _, e := range rc.subs {
		entries = append(entries, e)
	}
	rc.subMu.Unlock()

	for _, e := range entries {
		select {
		case <-e.ctx.Done():
			continue
		default:
		}
		if err := rc.attachSub(e.ctx, conn, e); err != nil {
			rc.logger.Warn("buzz: resubscribe failed", "err", err)
		}
	}
}
