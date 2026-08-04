package buzz

import (
	"context"
	"errors"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// --- F4: p-gate guard ------------------------------------------------------
//
// PRD "Two protocol traps we must design around" #1: global subscriptions
// matching kinds 44100/44101/1059 MUST include a #p filter whose values all
// equal the authenticated pubkey, or the relay silently returns nothing (or
// rejects with "restricted: p-gated events require #p matching your
// pubkey"). FR-016 requires this to be rejected in OUR OWN code before the
// filter ever reaches the relay -- so the guard lives inside
// RelayClient.Subscribe itself (the last hop before conn.Subscribe), not
// only in a caller-side helper that a future call site could bypass.

func TestSubscribe_PGatedKind_MissingPTag_Rejected(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	for _, kind := range []int{44100, 44101, 1059} {
		_, err := rc.Subscribe(context.Background(), domain.Filter{Kinds: []int{kind}})
		if err == nil {
			t.Fatalf("kind %d: expected rejection for missing #p filter, got nil", kind)
		}
		if !errors.Is(err, ErrPGateFilterMissing) {
			t.Fatalf("kind %d: expected ErrPGateFilterMissing, got %v", kind, err)
		}
	}
	if len(conn.subscribes) != 0 {
		t.Fatalf("expected no filter to reach the relay, got %d", len(conn.subscribes))
	}
}

func TestSubscribe_PGatedKind_WrongPubkey_Rejected(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	_, err := rc.Subscribe(context.Background(), domain.Filter{
		Kinds: []int{44100},
		Tags:  map[string][]string{"p": {"not-our-pubkey"}},
	})
	if !errors.Is(err, ErrPGateFilterMissing) {
		t.Fatalf("expected ErrPGateFilterMissing for mismatched #p value, got %v", err)
	}
}

func TestSubscribe_PGatedKind_CorrectPTag_Allowed(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	_, err := rc.Subscribe(context.Background(), domain.Filter{
		Kinds: []int{44100},
		Tags:  map[string][]string{"p": {rc.PubKey().Hex()}},
	})
	if err != nil {
		t.Fatalf("expected subscription to be allowed, got %v", err)
	}
	if len(conn.subscribes) != 1 {
		t.Fatalf("expected 1 filter sent to relay, got %d", len(conn.subscribes))
	}
}

func TestSubscribe_PGatedKind_MixedWithUngatedKinds_StillGuarded(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// A filter combining an ungated kind (9) with a p-gated kind (44100) must
	// still be rejected if the #p filter is absent -- the guard checks every
	// kind in the filter, not just the first.
	_, err := rc.Subscribe(context.Background(), domain.Filter{Kinds: []int{9, 44100}})
	if !errors.Is(err, ErrPGateFilterMissing) {
		t.Fatalf("expected ErrPGateFilterMissing, got %v", err)
	}
}

// --- F18: reaction #h guard -------------------------------------------------
//
// PRD trap #2: reactions (kind:7) derive their channel from the target
// event's #e tag; client-supplied #h tags on reactions are ignored by the
// relay's own fan-out, and a kinds-only reaction subscription silently
// receives nothing. Guard: any subscription containing kind 7 MUST carry a
// #h filter (mirrors F4's guard, same failure category, different trap).

func TestSubscribe_ReactionKind_MissingHTag_Rejected(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	_, err := rc.Subscribe(context.Background(), domain.Filter{Kinds: []int{7}})
	if !errors.Is(err, ErrReactionHGateMissing) {
		t.Fatalf("expected ErrReactionHGateMissing, got %v", err)
	}
	if len(conn.subscribes) != 0 {
		t.Fatalf("expected no filter to reach the relay, got %d", len(conn.subscribes))
	}
}

func TestSubscribe_ReactionKind_WithHTag_Allowed(t *testing.T) {
	conn := newFakeConn()
	rc, _ := newTestClient(t, conn)
	if err := rc.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	_, err := rc.Subscribe(context.Background(), domain.Filter{
		Kinds: []int{7},
		Tags:  map[string][]string{"h": {"chan-uuid-1"}},
	})
	if err != nil {
		t.Fatalf("expected subscription to be allowed, got %v", err)
	}
}

// --- validateSubscriptionFilter (unit-level, no RelayClient needed) --------

func TestValidateSubscriptionFilter_UngatedKind_NoTags_Allowed(t *testing.T) {
	if err := validateSubscriptionFilter(domain.Filter{Kinds: []int{9}}, "self-pk"); err != nil {
		t.Fatalf("expected no error for an ungated kind, got %v", err)
	}
}

func TestValidateSubscriptionFilter_EmptyKinds_Allowed(t *testing.T) {
	// A filter with no Kinds restriction at all is not, by definition,
	// a subscription "for" a p-gated or reaction kind.
	if err := validateSubscriptionFilter(domain.Filter{}, "self-pk"); err != nil {
		t.Fatalf("expected no error for an empty filter, got %v", err)
	}
}
