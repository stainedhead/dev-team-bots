package buzz

import (
	"context"
	"errors"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

var errFakeDial = errors.New("fake dial failure")

// TestBackoffConfig_Delay exercises D8's bounded-exponential-with-jitter
// calculation directly, independent of any connection machinery.
func TestBackoffConfig_Delay(t *testing.T) {
	b := BackoffConfig{Base: time.Second, Max: 10 * time.Second}

	if got := b.delay(0, 1.0); got != time.Second {
		t.Errorf("attempt 0, jitter 1.0: got %v, want %v", got, time.Second)
	}
	if got := b.delay(1, 1.0); got != 2*time.Second {
		t.Errorf("attempt 1, jitter 1.0: got %v, want %v", got, 2*time.Second)
	}
	if got := b.delay(2, 1.0); got != 4*time.Second {
		t.Errorf("attempt 2, jitter 1.0: got %v, want %v", got, 4*time.Second)
	}
	// Bounded: large attempt counts must clamp to Max, not overflow or
	// grow unbounded.
	if got := b.delay(100, 1.0); got != 10*time.Second {
		t.Errorf("attempt 100, jitter 1.0: got %v, want Max=%v", got, 10*time.Second)
	}
	// Jitter scales the delay down, never up or negative.
	if got := b.delay(0, 0.5); got != 500*time.Millisecond {
		t.Errorf("attempt 0, jitter 0.5: got %v, want %v", got, 500*time.Millisecond)
	}
	if got := b.delay(0, -1); got < 0 {
		t.Errorf("negative jitter must clamp to non-negative delay, got %v", got)
	}
}

func TestBackoffConfig_ZeroValueUsesDefaults(t *testing.T) {
	var b BackoffConfig
	if got := b.delay(0, 1.0); got != DefaultBackoffConfig.Base {
		t.Errorf("zero-value BackoffConfig should fall back to defaults, got %v", got)
	}
}

func TestDefaultJitter_InRange(t *testing.T) {
	for i := 0; i < 100; i++ {
		v := defaultJitter()
		if v < 0.5 || v >= 1.0 {
			t.Fatalf("defaultJitter() = %v, want [0.5, 1.0)", v)
		}
	}
}

// --- Reconnect integration (via RelayClient, fake conns) -------------------

func TestReconnect_ReauthenticatesAndResubscribesEverything(t *testing.T) {
	conn1 := newFakeConn()
	conn2 := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn1, conn2}}
	sk := nostr.Generate()

	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithAuthRetryInterval(time.Millisecond),
		WithSleep(func(time.Duration) {}),
		WithBackoff(BackoffConfig{Base: time.Millisecond, Max: 5 * time.Millisecond}),
	)

	ctx := context.Background()
	if err := rc.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Authenticate(ctx); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	ch, err := rc.Subscribe(ctx, domain.Filter{Kinds: []int{9}, Tags: map[string][]string{"h": {"chan-1"}}})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got := conn1.subscribeCount(); got != 1 {
		t.Fatalf("expected the initial subscription to reach conn1, got %d", got)
	}

	// Simulate the relay dropping the connection.
	conn1.disconnect()

	// Wait for the background reconnect loop to dial, auth, and
	// resubscribe on conn2.
	deadline := time.After(2 * time.Second)
	for conn2.subscribeCount() < 1 || conn2.authCallCount() < 1 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for reconnect: conn2.subscribes=%d conn2.authCalls=%d",
				conn2.subscribeCount(), conn2.authCallCount())
		case <-time.After(5 * time.Millisecond):
		}
	}

	if got := conn2.subscribeFilterAt(0); got.Kinds[0] != 9 {
		t.Errorf("expected the same filter to be re-subscribed on conn2, got %+v", got)
	}

	// The channel returned by the ORIGINAL Subscribe call must still be
	// the one delivering events -- no lost correlation across reconnect.
	sk2 := nostr.Generate()
	evt := nostr.Event{Kind: 9, Tags: nostr.Tags{{"h", "chan-1"}}, Content: "after reconnect"}
	if err := evt.Sign(sk2); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	conn2.deliver(0, evt)

	select {
	case got := <-ch:
		if got.Content != "after reconnect" {
			t.Errorf("unexpected event after reconnect: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for post-reconnect delivery on the original channel")
	}

	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestReconnect_RetriesDialFailures(t *testing.T) {
	// The first dial (Connect) succeeds via conn0; the next two dial
	// attempts (reconnect retries) fail before the fourth succeeds via
	// conn2.
	conn0 := newFakeConn()
	conn2 := newFakeConn()
	d := &fakeDialer{
		conns: []*fakeConn{conn0, nil, nil, conn2},
		errs:  []error{nil, errFakeDial, errFakeDial, nil},
	}
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithAuthRetryInterval(time.Millisecond),
		WithSleep(func(time.Duration) {}),
		WithBackoff(BackoffConfig{Base: time.Millisecond, Max: 2 * time.Millisecond}),
	)

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	conn0.disconnect()

	deadline := time.After(2 * time.Second)
	for d.callCount() < 4 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for retried reconnect dials, calls=%d", d.callCount())
		case <-time.After(5 * time.Millisecond):
		}
	}

	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestReconnect_StopsOnClose(t *testing.T) {
	conn := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn}}
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithSleep(func(time.Duration) {}),
		WithBackoff(BackoffConfig{Base: time.Millisecond, Max: time.Millisecond}),
	)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	done := make(chan struct{})
	go func() {
		if err := rc.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return promptly")
	}
}

// TestReconnect_ClosedDuringBackoffSleep covers the "closed mid-backoff"
// branch: the sleep seam itself closes the client (simulating Close()
// racing the backoff timer), and reconnect must notice and give up
// without dialing again.
func TestReconnect_ClosedDuringBackoffSleep(t *testing.T) {
	conn := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn}}
	sk := nostr.Generate()

	var rc *RelayClient
	rc = NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithSleep(func(time.Duration) {
			// Only close once, on the first backoff wait triggered by
			// the simulated disconnect below (not on Connect's own
			// path, which doesn't sleep).
			_ = rc.Close()
		}),
		WithBackoff(BackoffConfig{Base: time.Millisecond, Max: time.Millisecond}),
	)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	conn.disconnect()

	// watchLoop's reconnect() should observe rc.closed after the sleep
	// seam closes it, and return nil without a second dial.
	deadline := time.After(2 * time.Second)
	for d.callCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out")
		case <-time.After(5 * time.Millisecond):
		}
	}
	// Give watchLoop a moment to observe the closed state and exit; no
	// second dial should ever occur.
	time.Sleep(20 * time.Millisecond)
	if got := d.callCount(); got != 1 {
		t.Errorf("expected no reconnect dial after Close, got %d dial calls", got)
	}
}

// TestResubscribeAll_SkipsCanceledSubscription covers resubscribeAll's
// guard against re-attaching a subscription whose caller-owned ctx was
// already canceled by the time a reconnect happens.
func TestResubscribeAll_SkipsCanceledSubscription(t *testing.T) {
	conn1 := newFakeConn()
	conn2 := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn1, conn2}}
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithSleep(func(time.Duration) {}),
		WithBackoff(BackoffConfig{Base: time.Millisecond, Max: time.Millisecond}),
	)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if _, err := rc.Subscribe(ctx, domain.Filter{Kinds: []int{9}}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	cancel() // subscription's own ctx is done before the reconnect happens

	conn1.disconnect()
	deadline := time.After(2 * time.Second)
	for conn2.authCallCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for reconnect")
		case <-time.After(5 * time.Millisecond):
		}
	}
	time.Sleep(20 * time.Millisecond) // let resubscribeAll run to completion
	if got := conn2.subscribeCount(); got != 0 {
		t.Errorf("expected the canceled subscription NOT to be re-attached, got %d subscribes on conn2", got)
	}
	_ = rc.Close()
}

// TestWithJitter_CustomSourceUsed proves the jitter seam is actually
// consulted by the reconnect path.
func TestWithJitter_CustomSourceUsed(t *testing.T) {
	conn1 := newFakeConn()
	conn2 := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn1, conn2}}
	sk := nostr.Generate()

	jitterCalled := false
	var gotDelays []time.Duration
	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithJitter(func() float64 {
			jitterCalled = true
			return 1.0
		}),
		WithSleep(func(d time.Duration) { gotDelays = append(gotDelays, d) }),
		WithBackoff(BackoffConfig{Base: time.Millisecond, Max: time.Millisecond}),
	)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	conn1.disconnect()

	deadline := time.After(2 * time.Second)
	for conn2.authCallCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for reconnect")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if !jitterCalled {
		t.Error("expected the custom jitter source to be consulted")
	}
	if len(gotDelays) == 0 {
		t.Error("expected at least one backoff delay to be recorded")
	}
	_ = rc.Close()
}

// TestAttachSub_EntryNoLongerRegistered covers the branch where a
// subscription entry was removed from the registry (e.g. its ctx was
// canceled and removeAndClose already ran) between resubscribeAll
// snapshotting it and attachSub actually running.
func TestAttachSub_EntryNoLongerRegistered(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	entry := &subEntry{
		id:     999, // never registered in rc.subs
		ctx:    context.Background(),
		filter: nostr.Filter{Kinds: []nostr.Kind{9}},
		out:    make(chan domain.Event, 1),
	}
	if err := rc.attachSub(context.Background(), conn, entry); err == nil {
		t.Fatal("expected attachSub to refuse an entry that is not registered")
	}
}
