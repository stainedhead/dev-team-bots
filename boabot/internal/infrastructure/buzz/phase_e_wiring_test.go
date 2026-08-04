package buzz

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
)

// This file holds Phase E's wiring tests -- E3 (NIP-OA auth tag on the
// AUTH event), E4 (created_at freshness / clock skew), and E5 (auth
// failure class distinction) -- kept separate from Phase D's own
// relay_client_test.go so it's unambiguous which task each test proves.
// nipoa_test.go covers E1/E2 (preimage construction and owner sign/verify)
// in isolation from the RelayClient plumbing exercised here.

// --- E3: NIP-OA auth tag included on the kind:22242 AUTH event -------------

// TestE3_NIPOAAuthTagIncludedOnAuthEvent proves the full path end to end at
// the unit level: a real owner-signed NIP-OA tag (via nipoa.go's
// SignAuthTag), wrapped in StaticAuthTagFunc and wired into a RelayClient
// via WithAuthTagFunc (D5's extension point), ends up on the signed AUTH
// event -- and that tag independently validates via ValidateAuthTag against
// the RelayClient's own agent pubkey. This is the FR-005 unit-testable
// construction; live relay confirmation of the resulting virtual
// membership is on the manual-verification list (Phase I).
func TestE3_NIPOAAuthTagIncludedOnAuthEvent(t *testing.T) {
	conn := newFakeConn()
	agentSK := testAgentSK
	conditions := "kind=9&created_at<2000000000"

	wantTag, err := SignAuthTag(testOwnerSK, agentSK.Public().Hex(), conditions)
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}

	authTagFn, err := StaticAuthTagFunc(wantTag, agentSK.Public().Hex())
	if err != nil {
		t.Fatalf("StaticAuthTagFunc: %v", err)
	}

	d := &fakeDialer{conns: []*fakeConn{conn}}
	rc := NewRelayClient("wss://buzz.example/relay", agentSK,
		WithDial(d.dial),
		WithAuthRetryInterval(time.Millisecond),
		WithSleep(func(time.Duration) {}),
		WithAuthTagFunc(authTagFn),
	)

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	got := conn.lastAuthEvent.Tags.Find("auth")
	if got == nil {
		t.Fatal("expected an auth tag on the signed AUTH event")
	}
	gotTag := []string(got)
	if len(gotTag) != 4 {
		t.Fatalf("auth tag has %d elements, want 4: %v", len(gotTag), gotTag)
	}
	for i, v := range wantTag {
		if gotTag[i] != v {
			t.Errorf("auth tag[%d] = %q, want %q", i, gotTag[i], v)
		}
	}

	// The tag on the wire must itself independently validate -- proving
	// this isn't just byte-copying a fixture but a real, checkable NIP-OA
	// capability riding on the AUTH event.
	if err := ValidateAuthTag(gotTag, agentSK.Public().Hex()); err != nil {
		t.Fatalf("ValidateAuthTag on the wired tag: %v", err)
	}

	if !conn.lastAuthEvent.VerifySignature() {
		t.Error("expected the AUTH event itself to carry a valid agent signature")
	}
	if conn.lastAuthEvent.PubKey != agentSK.Public() {
		t.Error("expected the AUTH event to be authored by the agent key, not the owner key (NIP-OA is not NIP-26 delegation)")
	}
}

// TestE3_NoAuthTagFuncOmitsAuthTag is the negative control: when NIP-OA is
// not configured (no AuthTagFunc set), the AUTH event carries no "auth"
// tag at all -- confirming E3's wiring is additive, not always-on.
func TestE3_NoAuthTagFuncOmitsAuthTag(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if tag := conn.lastAuthEvent.Tags.Find("auth"); tag != nil {
		t.Fatalf("expected no auth tag when NIP-OA is not configured, got %v", tag)
	}
}

// --- E4: created_at freshness / clock skew ----------------------------------

// TestE4_DefaultClockStaysWithinFreshnessWindow is the positive control:
// with the default clock (time.Now, the same source the library uses for
// its own initial CreatedAt), authentication succeeds and the signed
// event's created_at is current -- proving E4's check doesn't get in the
// way of the ordinary case Phase D's own tests already rely on.
func TestE4_DefaultClockStaysWithinFreshnessWindow(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)

	before := time.Now().Add(-time.Second)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	after := time.Now().Add(time.Second)

	got := time.Unix(int64(conn.lastAuthEvent.CreatedAt), 0)
	if got.Before(before) || got.After(after) {
		t.Fatalf("AUTH event created_at = %v, want between %v and %v", got, before, after)
	}
}

// TestE4_ClockWithinToleranceSucceeds confirms a small, legitimate offset
// (well under the ±120s window) is not treated as skew.
func TestE4_ClockWithinToleranceSucceeds(t *testing.T) {
	conn := newFakeConn()
	rc, d := clientWithClock(t, conn, 30*time.Second)
	_ = d

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := rc.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: expected a 30s offset (within the ±120s window) to succeed, got %v", err)
	}
}

// TestE4_ClockAheadBeyondWindowFailsDistinguishably: the PRD's named
// acceptance criterion -- "AUTH event built with a clock offset beyond the
// relay's ±120s freshness window produces a distinguishable clock-skew
// error" -- for the ahead-of-real-time direction.
func TestE4_ClockAheadBeyondWindowFailsDistinguishably(t *testing.T) {
	conn := newFakeConn()
	rc, _ := clientWithClock(t, conn, 200*time.Second)

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	err := rc.Authenticate(context.Background())
	if !errors.Is(err, ErrAuthClockSkew) {
		t.Fatalf("Authenticate with +200s clock skew: got %v, want ErrAuthClockSkew", err)
	}
	// Distinguishable, not a generic auth failure: must not also be
	// misclassified as a relay-side invalid/restricted rejection, since
	// this never reached the relay at all.
	if errors.Is(err, ErrAuthInvalid) || errors.Is(err, ErrAuthRestricted) {
		t.Fatalf("clock-skew error must not be classified as a relay auth-failure class: %v", err)
	}
	if conn.authCallCount() == 0 {
		t.Fatal("expected conn.Auth to have been attempted (skew is caught inside the sign callback)")
	}
	if conn.lastAuthEvent.Sig != ([64]byte{}) {
		t.Fatal("expected the clock-skewed event to never be signed/sent")
	}
}

// TestE4_ClockBehindBeyondWindowFailsDistinguishably is the symmetric
// behind-real-time direction.
func TestE4_ClockBehindBeyondWindowFailsDistinguishably(t *testing.T) {
	conn := newFakeConn()
	rc, _ := clientWithClock(t, conn, -200*time.Second)

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	err := rc.Authenticate(context.Background())
	if !errors.Is(err, ErrAuthClockSkew) {
		t.Fatalf("Authenticate with -200s clock skew: got %v, want ErrAuthClockSkew", err)
	}
}

// clientWithClock builds a RelayClient whose clock reads time.Now()+offset
// at call time, so the measured skew against the library's own
// nostr.Now()-stamped CreatedAt is deterministically close to offset.
func clientWithClock(t *testing.T, conn *fakeConn, offset time.Duration) (*RelayClient, *fakeDialer) {
	t.Helper()
	d := &fakeDialer{conns: []*fakeConn{conn}}
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithAuthRetryInterval(time.Millisecond),
		WithSleep(func(time.Duration) {}),
		WithClock(func() time.Time { return time.Now().Add(offset) }),
	)
	return rc, d
}

// --- E5: auth failure class distinction -------------------------------------

// TestE5_ClassifiesInvalid: "invalid: ..." (step-1 failures) must
// distinguishably classify as AuthFailureInvalid / ErrAuthInvalid, never
// collapsed with restricted.
func TestE5_ClassifiesInvalid(t *testing.T) {
	var buf bytes.Buffer
	conn := newFakeConn()
	conn.authErr = errors.New("msg: invalid: bad signature")
	rc, _ := newTestClient(t, conn, WithLogger(testLogger(&buf)))

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	err := rc.Authenticate(context.Background())

	if !errors.Is(err, ErrAuthInvalid) {
		t.Fatalf("got %v, want ErrAuthInvalid", err)
	}
	if errors.Is(err, ErrAuthRestricted) {
		t.Fatalf("invalid: failure must not also classify as restricted: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid:") {
		t.Fatalf("expected the relay's invalid: reason to propagate, got %v", err)
	}

	logged := buf.String()
	if !strings.Contains(logged, "class=invalid") {
		t.Fatalf("expected a log entry with class=invalid, got: %s", logged)
	}
}

// TestE5_ClassifiesRestricted: "restricted: ..." (credential/membership
// failures) must distinguishably classify as AuthFailureRestricted /
// ErrAuthRestricted, never collapsed with invalid.
func TestE5_ClassifiesRestricted(t *testing.T) {
	var buf bytes.Buffer
	conn := newFakeConn()
	conn.authErr = errors.New("msg: restricted: owner is not an active member")
	rc, _ := newTestClient(t, conn, WithLogger(testLogger(&buf)))

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	err := rc.Authenticate(context.Background())

	if !errors.Is(err, ErrAuthRestricted) {
		t.Fatalf("got %v, want ErrAuthRestricted", err)
	}
	if errors.Is(err, ErrAuthInvalid) {
		t.Fatalf("restricted: failure must not also classify as invalid: %v", err)
	}
	if !strings.Contains(err.Error(), "restricted:") {
		t.Fatalf("expected the relay's restricted: reason to propagate, got %v", err)
	}

	logged := buf.String()
	if !strings.Contains(logged, "class=restricted") {
		t.Fatalf("expected a log entry with class=restricted, got: %s", logged)
	}
}

// TestE5_UnclassifiedFailureIsNeitherInvalidNorRestricted: a rejection
// reason matching neither prefix (e.g. a transport-level error) must not
// be misclassified as either named class.
func TestE5_UnclassifiedFailureIsNeitherInvalidNorRestricted(t *testing.T) {
	conn := newFakeConn()
	conn.authErr = errors.New("connection reset by peer")
	rc, _ := newTestClient(t, conn)

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	err := rc.Authenticate(context.Background())

	if errors.Is(err, ErrAuthInvalid) || errors.Is(err, ErrAuthRestricted) {
		t.Fatalf("unclassifiable failure must not match either named class: %v", err)
	}
	if err == nil {
		t.Fatal("expected an error")
	}
}

// TestE5_ClassifyAuthFailure_TableDriven exercises classifyAuthFailure
// directly against the two named prefixes and the nil/unmatched cases.
func TestE5_ClassifyAuthFailure_TableDriven(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want AuthFailureClass
	}{
		{"nil", nil, AuthFailureUnclassified},
		{"invalid prefix", errors.New("msg: invalid: bad sig"), AuthFailureInvalid},
		{"restricted prefix", errors.New("msg: restricted: not a member"), AuthFailureRestricted},
		{"unmatched", errors.New("timeout"), AuthFailureUnclassified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyAuthFailure(tc.err); got != tc.want {
				t.Errorf("classifyAuthFailure(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestAuthFailureClass_String pins the class names used as log/metric
// attribute values.
func TestAuthFailureClass_String(t *testing.T) {
	cases := map[AuthFailureClass]string{
		AuthFailureUnclassified: "unclassified",
		AuthFailureInvalid:      "invalid",
		AuthFailureRestricted:   "restricted",
	}
	for class, want := range cases {
		if got := class.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", int(class), got, want)
		}
	}
}
