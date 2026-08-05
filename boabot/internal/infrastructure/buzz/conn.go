package buzz

import (
	"context"
	"math"
	"time"

	"fiatjaf.com/nostr"
)

// relayConn is the seam between RelayClient and a single dialed connection
// to a relay. It exists so RelayClient's connect/auth/publish/subscribe/
// reconnect logic can be unit-tested against a fake, in-memory
// implementation instead of a live relay or a general-purpose WebSocket
// test server.
//
// Two library quirks shape this seam deliberately (see research.md):
//
//  1. A *nostr.Relay cannot be reconnected in place -- once its connection
//     context is canceled, ConnectWithClient hard-fails
//     ("relay context canceled") on any further attempt, and its `authed`
//     flag is set once and never reset. So relayConn instances are
//     single-use: reconnecting means dialing a brand new one, never
//     reusing/resetting an old one.
//  2. Subscribe returns a bare event channel, not the library's
//     *nostr.Subscription. Handing the library's own Subscription.Events
//     channel to a caller would mean it dies (closes) at the first
//     disconnect and never recovers, since PrepareSubscription cancels the
//     subscription when the relay's connection context is done. RelayClient
//     owns the long-lived, caller-facing channel and re-attaches it to a
//     fresh library subscription after every reconnect (see reconnect.go).
type relayConn interface {
	// Auth performs the NIP-42 handshake, calling sign to fill in the
	// event's tags/signature. It blocks until the relay's OK/CLOSED
	// response is known.
	Auth(ctx context.Context, sign func(context.Context, *nostr.Event) error) error

	// Publish sends evt and waits for the relay's OK response.
	Publish(ctx context.Context, evt nostr.Event) error

	// Subscribe opens a subscription for f, returning the underlying
	// library's raw event channel. It is closed when ctx is canceled or
	// the connection is lost.
	Subscribe(ctx context.Context, f nostr.Filter) (<-chan nostr.Event, error)

	// Done is closed when this connection is lost, for any reason.
	Done() <-chan struct{}

	// Close tears the connection down.
	Close() error
}

// dialFunc dials a new relayConn. It is a seam so tests can inject a fake
// implementation instead of dialing a real WebSocket.
type dialFunc func(ctx context.Context, url string, opts nostr.RelayOptions) (relayConn, error)

// dialLibRelay is the production dialFunc, wrapping fiatjaf.com/nostr.
func dialLibRelay(ctx context.Context, url string, opts nostr.RelayOptions) (relayConn, error) {
	r, err := nostr.RelayConnect(ctx, url, opts)
	if err != nil {
		return nil, err
	}
	return &libRelayConn{r: r}, nil
}

// noEOSETimeout disables PrepareSubscription's synthetic end-of-stored-
// events timer -- BaoBot's subscriptions are long-lived and don't consume
// EOSE, so there is no reason to pay for the extra per-subscription timer
// goroutine the library spawns when this is left at its 7s default.
const noEOSETimeout = time.Duration(math.MaxInt64)

type libRelayConn struct {
	r *nostr.Relay
}

func (c *libRelayConn) Auth(ctx context.Context, sign func(context.Context, *nostr.Event) error) error {
	return c.r.Auth(ctx, sign)
}

func (c *libRelayConn) Publish(ctx context.Context, evt nostr.Event) error {
	return c.r.Publish(ctx, evt)
}

func (c *libRelayConn) Subscribe(ctx context.Context, f nostr.Filter) (<-chan nostr.Event, error) {
	sub, err := c.r.Subscribe(ctx, f, nostr.SubscriptionOptions{MaxWaitForEOSE: noEOSETimeout})
	if err != nil {
		return nil, err
	}
	return sub.Events, nil
}

func (c *libRelayConn) Done() <-chan struct{} {
	return c.r.Context().Done()
}

func (c *libRelayConn) Close() error {
	return c.r.Close()
}

var _ relayConn = (*libRelayConn)(nil)
