package buzz

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

func newTestClient(t *testing.T, conn *fakeConn, opts ...Option) (*RelayClient, *fakeDialer) {
	t.Helper()
	d := &fakeDialer{conns: []*fakeConn{conn}}
	sk := nostr.Generate()
	base := []Option{
		WithDial(d.dial),
		WithAuthRetryInterval(time.Millisecond),
		WithSleep(func(time.Duration) {}), // no real sleeping in tests
	}
	rc := NewRelayClient("wss://buzz.example/relay", sk, append(base, opts...)...)
	return rc, d
}

// --- Connect -------------------------------------------------------------

func TestConnect_Success(t *testing.T) {
	conn := newFakeConn()
	rc, d := newTestClient(t, conn)

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if d.callCount() != 1 {
		t.Fatalf("expected 1 dial call, got %d", d.callCount())
	}
}

func TestConnect_DialError(t *testing.T) {
	sk := nostr.Generate()
	d := &fakeDialer{errs: []error{errors.New("boom")}}
	rc := NewRelayClient("wss://buzz.example/relay", sk, WithDial(d.dial))

	if err := rc.Connect(context.Background()); err == nil {
		t.Fatal("expected dial error to propagate")
	}
}

// --- Authenticate ----------------------------------------------------------

func TestAuthenticate_Success(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := rc.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if conn.lastAuthEvent.Kind != nostr.KindClientAuthentication {
		t.Errorf("expected kind:22242 auth event, got kind %d", conn.lastAuthEvent.Kind)
	}
	if tag := conn.lastAuthEvent.Tags.Find("relay"); tag == nil || tag[1] != conn.relayURL {
		t.Errorf("expected relay tag, got %+v", conn.lastAuthEvent.Tags)
	}
	if tag := conn.lastAuthEvent.Tags.Find("challenge"); tag == nil || tag[1] != conn.challenge {
		t.Errorf("expected challenge tag, got %+v", conn.lastAuthEvent.Tags)
	}
	if !conn.lastAuthEvent.VerifySignature() {
		t.Error("expected auth event to carry a valid signature")
	}
}

func TestAuthenticate_NotConnected(t *testing.T) {
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk)
	if err := rc.Authenticate(context.Background()); err == nil {
		t.Fatal("expected error when Authenticate is called before Connect")
	}
}

// TestAuthenticate_RetriesUntilChallengeArrives exercises the race the
// library's own AuthHandler auto-fire path would otherwise expose: a
// caller who calls Authenticate immediately after Connect must not
// observe a hard failure just because the relay's AUTH challenge frame
// hasn't been processed by the read loop yet.
func TestAuthenticate_RetriesUntilChallengeArrives(t *testing.T) {
	conn := newFakeConn()
	conn.noChallengeUntil = 3 // first 3 calls simulate "no challenge yet"
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := rc.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if conn.authCalls < 4 {
		t.Errorf("expected at least 4 Auth attempts, got %d", conn.authCalls)
	}
}

func TestAuthenticate_GivesUpAtContextDeadline(t *testing.T) {
	conn := newFakeConn()
	conn.noChallengeUntil = 1_000_000 // never arrives
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := rc.Authenticate(ctx); err == nil {
		t.Fatal("expected Authenticate to give up once ctx deadline passes")
	}
}

func TestAuthenticate_RelayRejectsWithReason(t *testing.T) {
	conn := newFakeConn()
	conn.authErr = errors.New(`msg: restricted: owner is not an active member`)
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	err := rc.Authenticate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "restricted:") {
		t.Fatalf("expected the relay's restricted: reason to propagate, got %v", err)
	}
}

// TestAuthenticate_AuthTagHook proves D5's NIP-OA extension point: an
// optional AuthTagFunc, when configured, has its tag appended to the AUTH
// event before it's signed -- without requiring any change to
// Authenticate's own logic. Phase E fills this hook with real NIP-OA
// preimage/signature construction; here it's a stand-in raw tag.
func TestAuthenticate_AuthTagHook(t *testing.T) {
	conn := newFakeConn()
	wantTag := []string{"auth", "owner-pubkey-hex", "kind=9", "sig-hex"}
	hookCalled := false
	rc, _ := newTestClient(t, conn, WithAuthTagFunc(func(_ context.Context) ([]string, error) {
		hookCalled = true
		return wantTag, nil
	}))
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !hookCalled {
		t.Fatal("expected the AuthTagFunc hook to be called")
	}
	tag := conn.lastAuthEvent.Tags.Find("auth")
	if tag == nil {
		t.Fatal("expected an auth tag on the signed AUTH event")
	}
	for i, v := range wantTag {
		if tag[i] != v {
			t.Errorf("auth tag[%d] = %q, want %q", i, tag[i], v)
		}
	}
}

func TestAuthenticate_AuthTagHookError(t *testing.T) {
	conn := newFakeConn()
	sentinel := errors.New("owner signature invalid")
	rc, _ := newTestClient(t, conn, WithAuthTagFunc(func(_ context.Context) ([]string, error) {
		return nil, sentinel
	}))
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Authenticate(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("expected the hook's error to propagate, got %v", err)
	}
}

// --- Publish ---------------------------------------------------------------

func TestPublish_Success(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	evt := domain.Event{Kind: 9, Tags: [][]string{{"h", "chan-1"}}, Content: "hi"}
	if err := rc.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(conn.published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(conn.published))
	}
	got := conn.published[0]
	if got.Kind != 9 || got.Content != "hi" {
		t.Errorf("published event mismatch: %+v", got)
	}
	if got.PubKey != rc.PubKey() {
		t.Errorf("expected published event to carry the client's own pubkey")
	}
	if !got.VerifySignature() {
		t.Error("expected published event to be validly signed")
	}
	if got.CreatedAt == 0 {
		t.Error("expected CreatedAt to be auto-filled when zero")
	}
}

func TestPublish_NotConnected(t *testing.T) {
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk)
	err := rc.Publish(context.Background(), domain.Event{Kind: 1})
	if err == nil {
		t.Fatal("expected error when publishing before Connect")
	}
}

func TestPublish_TranslationError(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	err := rc.Publish(context.Background(), domain.Event{Kind: 99999})
	if err == nil {
		t.Fatal("expected translation error for an out-of-range kind")
	}
}

func TestPublish_RelayError(t *testing.T) {
	conn := newFakeConn()
	conn.publishErr = errors.New("msg: blocked: rate limited")
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Publish(context.Background(), domain.Event{Kind: 1}); err == nil {
		t.Fatal("expected relay publish error to propagate")
	}
}

// --- Subscribe / Close -----------------------------------------------------

func TestSubscribe_DeliversTranslatedEvents(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	ch, err := rc.Subscribe(context.Background(), domain.Filter{Kinds: []int{9}, Tags: map[string][]string{"h": {"chan-1"}}})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if len(conn.subscribes) != 1 || conn.subscribes[0].Kinds[0] != 9 {
		t.Fatalf("expected the filter to reach the connection: %+v", conn.subscribes)
	}

	sk := nostr.Generate()
	libEvt := nostr.Event{Kind: 9, Tags: nostr.Tags{{"h", "chan-1"}}, Content: "hello"}
	if err := libEvt.Sign(sk); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	conn.deliver(0, libEvt)

	select {
	case got := <-ch:
		if got.Content != "hello" || got.Kind != 9 {
			t.Errorf("unexpected translated event: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscribed event")
	}
}

func TestSubscribe_ContextCancelStopsDelivery(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := rc.Subscribe(ctx, domain.Filter{Kinds: []int{9}})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed, not to deliver a value")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel to close after ctx cancel")
	}
}

func TestClose_Idempotent(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("second Close should be a no-op, got: %v", err)
	}
	if conn.closes != 1 {
		t.Errorf("expected the underlying connection to be closed exactly once, got %d", conn.closes)
	}
}

func TestClose_ClosesSubscriptionChannels(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	ch, err := rc.Subscribe(context.Background(), domain.Filter{Kinds: []int{9}})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected subscription channel to be closed by Close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription channel to close")
	}
}

func TestSubscribe_AfterClose_Errors(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := rc.Subscribe(context.Background(), domain.Filter{Kinds: []int{9}}); err == nil {
		t.Fatal("expected Subscribe to error after Close")
	}
}

// TestNeverLogsPrivateKey exercises Connect+Authenticate+Publish with a
// captured slog buffer, asserting the hex-encoded secret key never appears
// anywhere in the log output -- the RelayClient half of D4's full
// keypair-load-through-relay-connect log-safety requirement (the
// LoadKeypair half lives in keypair_test.go).
func TestNeverLogsPrivateKey(t *testing.T) {
	var buf bytes.Buffer
	sk := nostr.Generate()
	skHex := sk.Hex()

	conn := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn}}
	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithLogger(testLogger(&buf)),
		WithAuthRetryInterval(time.Millisecond),
	)

	ctx := context.Background()
	if err := rc.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Authenticate(ctx); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if err := rc.Publish(ctx, domain.Event{Kind: 1, Content: "hi"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if strings.Contains(buf.String(), skHex) {
		t.Fatalf("log output contains the hex-encoded private key")
	}
}
