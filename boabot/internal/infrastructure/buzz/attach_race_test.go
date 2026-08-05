package buzz

import (
	"context"
	"sync"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// WS-B1 (FR-002): a concurrent double attach of the same subEntry (the
// exact interleaving is Subscribe's own initial attachSub racing a
// concurrent reconnect's resubscribeAll attaching the same not-yet-attached
// entry -- see attachSub's doc comment) must be safe: at most one
// generation's pump ever forwards an event, and removeAndClose must wait
// for every generation's pump before closing entry.out, so an orphaned
// earlier-generation pump can never send on an already-closed channel.
//
// This test drives attachSub directly (rather than orchestrating the full
// Subscribe/reconnect timing via subscribeAfterRegisterHook) because that
// is the actual mechanism both real interleavings funnel through, and a
// direct concurrent double-attach is the sharpest, most deterministic way
// to prove the fix -- no sleep-and-hope timing is needed.
func TestAttachSub_ConcurrentDoubleAttach_OnlyOneGenerationDelivers(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	ctx := context.Background()
	if err := rc.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Authenticate(ctx); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	entryCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	entry := &subEntry{
		id:     999,
		ctx:    entryCtx,
		filter: nostr.Filter{Kinds: []nostr.Kind{9}},
		out:    make(chan domain.Event, subscribeChannelBuffer),
	}
	rc.subMu.Lock()
	rc.subs[entry.id] = entry
	rc.subMu.Unlock()

	var attachWG sync.WaitGroup
	attachWG.Add(2)
	errs := make([]error, 2)
	go func() { defer attachWG.Done(); errs[0] = rc.attachSub(entryCtx, conn, entry) }()
	go func() { defer attachWG.Done(); errs[1] = rc.attachSub(entryCtx, conn, entry) }()
	attachWG.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("attachSub errors: %v / %v", errs[0], errs[1])
	}
	if got := conn.subscribeCount(); got != 2 {
		t.Fatalf("expected 2 underlying conn.Subscribe calls (one per concurrent attach), got %d", got)
	}

	sk := nostr.Generate()
	evt := nostr.Event{Kind: 9, Tags: nostr.Tags{{"h", "chan-1"}}, Content: "double-attach payload"}
	if err := evt.Sign(sk); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	// Deliver the same event on BOTH underlying subscriptions -- one per
	// concurrent attachSub call. Only the current generation's pump must
	// forward it.
	conn.deliver(0, evt)
	conn.deliver(1, evt)

	select {
	case got := <-entry.out:
		if got.Content != evt.Content {
			t.Errorf("unexpected content: %q", got.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the current generation's delivery")
	}
	select {
	case extra := <-entry.out:
		t.Fatalf("stale generation also delivered -- expected exactly one delivery, got a second: %+v", extra)
	case <-time.After(200 * time.Millisecond):
		// Expected: nothing else arrives.
	}

	// Tear down: cancel the entry's ctx, which closes both underlying
	// library subscriptions (fakeConn mirrors *nostr.Relay's documented
	// ctx-or-connection-loss channel-close behaviour), letting both pumps
	// exit and call entry.wg.Done(). removeAndClose then closes
	// entry.out. Before the FR-002 fix (a single pumpDone slot instead of
	// entry.wg covering every generation), removeAndClose would have
	// returned after waiting on only the LATEST attach's completion
	// signal, closing entry.out while the OTHER (orphaned) pump could
	// still be alive -- an unrecovered "send on closed channel" panic the
	// instant it next tried to forward. Waiting for BOTH here proves that
	// can no longer happen.
	cancel()
	done := make(chan struct{})
	go func() {
		rc.removeAndClose(entry.id)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("removeAndClose did not return -- a pump may be stuck or entry.wg miscounted")
	}

	select {
	case _, ok := <-entry.out:
		if ok {
			t.Fatal("expected entry.out to be closed and empty, got a value")
		}
	default:
		t.Fatal("expected entry.out to be closed by now")
	}
}

// WS-B2 (FR-003): a reconnect's re-attach (resubscribeAll -> attachSub)
// racing a concurrent Close() must never panic and must never leak a pump
// goroutine. reconnectAfterConnSwapHook pauses reconnect() immediately
// after it publishes the new connection to rc.conn, right before
// resubscribeAll runs -- exactly the window FR-003's finding describes.
func TestReconnect_RacingClose_NoPanicNoLeak(t *testing.T) {
	conn1 := newFakeConn()
	conn2 := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn1, conn2}}
	sk := nostr.Generate()

	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithAuthRetryInterval(time.Millisecond),
		WithSleep(func(time.Duration) {}),
		WithBackoff(BackoffConfig{Base: time.Millisecond, Max: 2 * time.Millisecond}),
	)

	ctx := context.Background()
	if err := rc.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Authenticate(ctx); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if _, err := rc.Subscribe(ctx, domain.Filter{Kinds: []int{9}, Tags: map[string][]string{"h": {"chan-1"}}}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	proceed := make(chan struct{})
	// hookDone is closed by the hook itself, strictly after it resumes
	// from <-proceed, and is waited on below before the test returns.
	// This is not redundant with waiting on closeDone: closeDone only
	// synchronizes with the Close() goroutine, and establishes no
	// happens-before relationship with the *reconnect* goroutine this
	// hook runs in. Without hookDone, resetting the global hook var in
	// t.Cleanup below races (per the Go memory model, regardless of
	// wall-clock ordering) against reconnect.go's read of that same var,
	// since nothing otherwise orders "the read, which happens once,
	// before the closure blocks on <-proceed" relative to "Cleanup's
	// write, after the test function returns."
	hookDone := make(chan struct{})
	reconnectAfterConnSwapHook = func() {
		<-proceed
		close(hookDone)
	}
	t.Cleanup(func() { reconnectAfterConnSwapHook = nil })

	// Force a reconnect: conn1 drops, the client dials conn2, authenticates,
	// swaps rc.conn -- then reconnect() blocks in the hook, right before
	// resubscribeAll would run.
	conn1.disconnect()

	deadline := time.After(2 * time.Second)
	for conn2.authCallCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for reconnect to authenticate on conn2")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// Now race Close() against the paused reconnect finishing its
	// resubscribeAll pass.
	closeDone := make(chan error, 1)
	go func() { closeDone <- rc.Close() }()

	// Give Close() a moment to reach its own critical section before we
	// release the reconnect goroutine, to bias toward (not guarantee,
	// which is fine -- both orderings must be safe) the interleaving
	// FR-003 is about.
	time.Sleep(20 * time.Millisecond)
	close(proceed)

	select {
	case <-hookDone:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect's hook did not resume after being released")
	}

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close returned an error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not return -- possible deadlock racing the paused reconnect")
	}

	// If this line is reached, no goroutine panicked (a panic in any
	// goroutine, including pumpSub, crashes the whole test binary rather
	// than being reported as a normal test failure).
}
