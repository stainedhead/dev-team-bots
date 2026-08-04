//go:build integration

package buzz

import (
	"context"
	"os"
	"testing"
	"time"

	"fiatjaf.com/nostr"
)

// TestDialLibRelay_LiveRelay exercises dialLibRelay/libRelayConn (the
// production relayConn implementation, untouched by the fake-based unit
// tests in the rest of this package) against a real relay. It requires
// BUZZ_TEST_RELAY_URL to point at a running relay (e.g. Buzz's own
// docker-compose.yml stack) and is excluded from normal `go test` runs by
// the integration build tag, per the PRD's NFR/Testing pre-flight
// decision -- this is a stub for Phase I to build on (real connect/auth/
// pub/sub/reconnect-restart scenarios), not the full manual-verification
// checklist itself.
func TestDialLibRelay_LiveRelay(t *testing.T) {
	url := os.Getenv("BUZZ_TEST_RELAY_URL")
	if url == "" {
		t.Skip("BUZZ_TEST_RELAY_URL not set; skipping live-relay integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := dialLibRelay(ctx, url, nostr.RelayOptions{})
	if err != nil {
		t.Fatalf("dialLibRelay: %v", err)
	}
	defer conn.Close()

	select {
	case <-conn.Done():
		t.Fatal("connection reported done immediately after a successful dial")
	default:
	}
}
