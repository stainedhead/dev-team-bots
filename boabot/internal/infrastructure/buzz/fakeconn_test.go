package buzz

import (
	"context"
	"errors"
	"sync"

	"fiatjaf.com/nostr"
)

// fakeConn is a test double for relayConn. It mimics just enough of
// *nostr.Relay's observable behaviour (documented in conn.go) for
// RelayClient's connect/auth/publish/subscribe/reconnect logic to be
// unit-tested without a live relay or a general-purpose WebSocket server.
type fakeConn struct {
	mu sync.Mutex

	relayURL  string
	challenge string

	authErr          error
	noChallengeUntil int // Auth returns "no challenge" for calls <= this
	authCalls        int
	lastAuthEvent    nostr.Event

	publishErr error
	published  []nostr.Event

	subscribeErr error
	subscribes   []nostr.Filter
	subChans     []chan nostr.Event

	doneCh   chan struct{}
	closed   bool
	closeErr error
	closes   int
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		relayURL:  "wss://buzz.example/relay",
		challenge: "test-challenge",
		doneCh:    make(chan struct{}),
	}
}

func (f *fakeConn) Auth(ctx context.Context, sign func(context.Context, *nostr.Event) error) error {
	f.mu.Lock()
	f.authCalls++
	calls := f.authCalls
	noChallenge := f.noChallengeUntil
	f.mu.Unlock()

	if calls <= noChallenge {
		return errNoChallenge
	}

	evt := nostr.Event{
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindClientAuthentication,
		Tags:      nostr.Tags{{"relay", f.relayURL}, {"challenge", f.challenge}},
	}
	if err := sign(ctx, &evt); err != nil {
		return err
	}

	f.mu.Lock()
	f.lastAuthEvent = evt
	authErr := f.authErr
	f.mu.Unlock()

	return authErr
}

func (f *fakeConn) Publish(_ context.Context, evt nostr.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, evt)
	return f.publishErr
}

func (f *fakeConn) Subscribe(ctx context.Context, filt nostr.Filter) (<-chan nostr.Event, error) {
	f.mu.Lock()
	if f.subscribeErr != nil {
		f.mu.Unlock()
		return nil, f.subscribeErr
	}
	ch := make(chan nostr.Event, 16)
	f.subscribes = append(f.subscribes, filt)
	f.subChans = append(f.subChans, ch)
	f.mu.Unlock()

	// Mirror *nostr.Relay.Subscribe's real, documented behaviour: the
	// returned channel closes when ctx is canceled OR the connection is
	// lost (PrepareSubscription watches both the subscription's own ctx
	// and the relay's connection context). RelayClient's correctness
	// (Subscribe's ctx-cancel-closes-the-channel contract, and pump
	// cleanup on disconnect) depends on this, so the fake must honor it
	// too, not just Close()'s explicit path.
	go func() {
		select {
		case <-ctx.Done():
		case <-f.doneCh:
		}
		close(ch)
	}()

	return ch, nil
}

func (f *fakeConn) Done() <-chan struct{} { return f.doneCh }

func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	if !f.closed {
		f.closed = true
		close(f.doneCh)
	}
	return f.closeErr
}

// disconnect simulates the connection dropping for a reason other than an
// explicit Close() call (e.g. the relay restarting).
func (f *fakeConn) disconnect() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.doneCh)
	}
}

func (f *fakeConn) deliver(subIndex int, evt nostr.Event) {
	f.mu.Lock()
	ch := f.subChans[subIndex]
	f.mu.Unlock()
	ch <- evt
}

// The accessors below take f.mu, so tests that poll fakeConn state from a
// goroutine other than the one driving RelayClient (e.g. waiting for a
// background reconnect to complete) don't race with fakeConn's own
// internal writes under -race.

func (f *fakeConn) subscribeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subscribes)
}

func (f *fakeConn) subscribeFilterAt(i int) nostr.Filter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subscribes[i]
}

func (f *fakeConn) authCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authCalls
}

// fakeDialer hands out pre-built fakeConns in the order Connect/reconnect
// dial them, recording every dial's URL and options for assertions (e.g.
// D6's Authorization header).
type fakeDialer struct {
	mu    sync.Mutex
	conns []*fakeConn
	errs  []error
	calls []dialCall
}

type dialCall struct {
	url  string
	opts nostr.RelayOptions
}

func (d *fakeDialer) dial(_ context.Context, url string, opts nostr.RelayOptions) (relayConn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, dialCall{url: url, opts: opts})

	idx := len(d.calls) - 1
	if idx < len(d.errs) && d.errs[idx] != nil {
		return nil, d.errs[idx]
	}
	if idx < len(d.conns) {
		return d.conns[idx], nil
	}
	if len(d.conns) == 0 {
		return nil, errors.New("fakeDialer: no more connections configured")
	}
	// Beyond the configured list, keep returning the last one.
	return d.conns[len(d.conns)-1], nil
}

func (d *fakeDialer) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

func (d *fakeDialer) lastOpts() nostr.RelayOptions {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls[len(d.calls)-1].opts
}

func bearerHeader(opts nostr.RelayOptions) string {
	if opts.RequestHeader == nil {
		return ""
	}
	return opts.RequestHeader.Get("Authorization")
}
