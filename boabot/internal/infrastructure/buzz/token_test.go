package buzz

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret"
)

// --- LoadAPIToken (D6, SecretStore chain resolution) ------------------------

func TestLoadAPIToken_Resolved(t *testing.T) {
	store := &fakeSecretStore{value: "  secret-token  \n"}
	token, found, err := LoadAPIToken(context.Background(), store, "buzzbot")
	if err != nil {
		t.Fatalf("LoadAPIToken: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if token != "secret-token" {
		t.Errorf("token = %q, want trimmed %q", token, "secret-token")
	}
	if store.gotRef.Name != APITokenSecretName || store.gotRef.Bot != "buzzbot" {
		t.Errorf("unexpected ref: %+v", store.gotRef)
	}
}

func TestLoadAPIToken_NotConfigured_IsNotAnError(t *testing.T) {
	// The token is optional unless the relay requires it -- an all-miss
	// resolution (every provider in the chain consulted, none had it,
	// exactly what internal/infrastructure/secret.Store.Get returns as
	// *secret.NotFoundError) is a normal, expected outcome, not a
	// failure.
	notFound := &secret.NotFoundError{
		Ref:       domain.SecretRef{Name: APITokenSecretName, Bot: "buzzbot"},
		Providers: []string{"env", "systemd", "keystore", "file"},
	}
	store := &fakeSecretStore{err: notFound}
	token, found, err := LoadAPIToken(context.Background(), store, "buzzbot")
	if err != nil {
		t.Fatalf("LoadAPIToken should not error when the token is simply absent: %v", err)
	}
	if found {
		t.Fatal("expected found=false")
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

func TestLoadAPIToken_GenuineProviderError_Propagates(t *testing.T) {
	// A real provider failure (as opposed to a clean "not found") is
	// FR-053's whole point: the operator needs to see it, not have it
	// silently treated the same as "not configured."
	sentinel := errors.New("keystore: D-Bus connection refused")
	store := &fakeSecretStore{err: sentinel}
	_, found, err := LoadAPIToken(context.Background(), store, "buzzbot")
	if err == nil {
		t.Fatal("expected a genuine provider error to propagate, not be swallowed")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected the wrapped sentinel to be reachable via errors.Is, got %v", err)
	}
	if found {
		t.Error("expected found=false on error")
	}
}

// --- Connect-time token gating (D6, FR-010) ---------------------------------

func TestConnect_RequiredTokenMissing_FailsClosedWithoutDialing(t *testing.T) {
	conn := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn}}
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithAPIToken("", true),
	)

	if err := rc.Connect(context.Background()); err == nil {
		t.Fatal("expected Connect to fail closed when a required token is missing")
	}
	if got := d.callCount(); got != 0 {
		t.Errorf("expected no dial attempt when failing closed, got %d dial calls", got)
	}
}

func TestConnect_RequiredTokenPresent_ConnectsWithBearerHeader(t *testing.T) {
	conn := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn}}
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithAPIToken("valid-token", true),
	)

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := bearerHeader(d.lastOpts()); got != "Bearer valid-token" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer valid-token")
	}
}

func TestConnect_OptionalTokenAbsent_ConnectsWithoutHeader(t *testing.T) {
	conn := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn}}
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk, WithDial(d.dial))

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := bearerHeader(d.lastOpts()); got != "" {
		t.Errorf("expected no Authorization header, got %q", got)
	}
}

func TestReconnect_CarriesTokenHeaderOnEveryDial(t *testing.T) {
	conn1 := newFakeConn()
	conn2 := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn1, conn2}}
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithAPIToken("valid-token", true),
		WithSleep(func(time.Duration) {}),
		WithBackoff(BackoffConfig{Base: time.Millisecond, Max: time.Millisecond}),
	)

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	conn1.disconnect()

	deadline := time.After(2 * time.Second)
	for d.callCount() < 2 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for reconnect dial, calls=%d", d.callCount())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if got := bearerHeader(d.lastOpts()); got != "Bearer valid-token" {
		t.Errorf("expected the reconnect dial to also carry the bearer token, got %q", got)
	}
	_ = rc.Close()
}

// --- Profile publish (D7, FR-011) -------------------------------------------

func TestConnect_PublishesProfileOnFirstConnect(t *testing.T) {
	conn := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn}}
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithProfile(Profile{Name: "buzzbot", BotType: "engineer", Description: "Ships code."}),
	)

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if len(conn.published) != 1 {
		t.Fatalf("expected exactly 1 published event (the profile), got %d", len(conn.published))
	}
	got := conn.published[0]
	if got.Kind != nostr.KindProfileMetadata {
		t.Errorf("expected kind:0, got kind:%d", got.Kind)
	}
	if got.PubKey != rc.PubKey() {
		t.Error("expected the profile event to be authored by the client's own pubkey")
	}
	if !got.VerifySignature() {
		t.Error("expected the profile event to be validly signed")
	}
	if !containsAll(got.Content, "buzzbot", "engineer", "Ships code.") {
		t.Errorf("profile content missing expected fields: %s", got.Content)
	}
}

func TestConnect_ProfilePublishedOnlyOnce(t *testing.T) {
	conn1 := newFakeConn()
	conn2 := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn1, conn2}}
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithProfile(Profile{Name: "buzzbot"}),
		WithSleep(func(time.Duration) {}),
		WithBackoff(BackoffConfig{Base: time.Millisecond, Max: time.Millisecond}),
	)

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if len(conn1.published) != 1 {
		t.Fatalf("expected 1 profile publish on first connect, got %d", len(conn1.published))
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

	// "First successful connection" means once per RelayClient lifetime,
	// not once per (re)connection.
	if len(conn2.published) != 0 {
		t.Errorf("expected no profile publish on reconnect, got %d", len(conn2.published))
	}
	_ = rc.Close()
}

func TestConnect_NoProfileConfigured_PublishesNothing(t *testing.T) {
	conn := newFakeConn()
	d := &fakeDialer{conns: []*fakeConn{conn}}
	sk := nostr.Generate()
	rc := NewRelayClient("wss://buzz.example/relay", sk, WithDial(d.dial))

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if len(conn.published) != 0 {
		t.Errorf("expected no publish when no Profile is configured, got %d", len(conn.published))
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// TestConnect_ProfilePublishFailure_LoggedNotFatal covers the branch where
// the relay rejects the kind:0 publish: Connect must still succeed (the
// profile is a best-effort courtesy, not a precondition for the
// connection being usable) and the failure must only be logged.
func TestConnect_ProfilePublishFailure_LoggedNotFatal(t *testing.T) {
	conn := newFakeConn()
	conn.publishErr = errors.New("msg: blocked: rate limited")
	d := &fakeDialer{conns: []*fakeConn{conn}}
	sk := nostr.Generate()

	var buf bytes.Buffer
	rc := NewRelayClient("wss://buzz.example/relay", sk,
		WithDial(d.dial),
		WithProfile(Profile{Name: "buzzbot"}),
		WithLogger(testLogger(&buf)),
	)

	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect should still succeed when only the profile publish fails: %v", err)
	}
	if !strings.Contains(buf.String(), "profile publish failed") {
		t.Errorf("expected the profile publish failure to be logged, got: %s", buf.String())
	}
}

func TestProfileContent_Variants(t *testing.T) {
	cases := []struct {
		name    string
		profile Profile
		want    []string
	}{
		{"name only", Profile{Name: "buzzbot"}, []string{`"name":"buzzbot"`, `"about":""`}},
		{"description only", Profile{Name: "buzzbot", Description: "Ships code."}, []string{`"about":"Ships code."`}},
		{"bot type only", Profile{Name: "buzzbot", BotType: "engineer"}, []string{`"about":"engineer"`}},
		{"both", Profile{Name: "buzzbot", BotType: "engineer", Description: "Ships code."}, []string{`"about":"Ships code. (engineer)"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.profile.content()
			if err != nil {
				t.Fatalf("content: %v", err)
			}
			if !containsAll(got, tc.want...) {
				t.Errorf("content() = %q, want to contain %v", got, tc.want)
			}
		})
	}
}
