package buzz

import (
	"errors"
	"fmt"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// pGatedKinds are the Nostr kinds the PRD's "Two protocol traps" section
// names as p-gated: a global subscription for any of these MUST include a
// #p filter whose values all equal the authenticated pubkey, or the relay
// rejects it as "restricted: p-gated events require #p matching your
// pubkey" -- silently, from a naive client's point of view. 1059 (gift-
// wrapped DM) is covered from the outset (FR-016) even though DM handling
// itself is out of scope this run, so P1's DM work cannot reintroduce the
// violation.
var pGatedKinds = map[int]bool{44100: true, 44101: true, 1059: true}

// reactionKind is Nostr's reaction kind (NIP-25). Per the PRD's second
// protocol trap, reactions derive their channel from the target event's #e
// tag, not from any client-supplied #h tag -- live fan-out segregates
// channel and global subscriptions, so a kinds-only reaction subscription
// receives nothing at all, with no error. FR-027/F18 require every
// reaction subscription to carry #h regardless.
const reactionKind = 7

// ErrPGateFilterMissing is returned by RelayClient.Subscribe when a filter
// requests a p-gated kind (44100, 44101, 1059) without a #p filter whose
// values all equal the client's own authenticated pubkey (FR-016).
var ErrPGateFilterMissing = errors.New("buzz: subscription for a p-gated kind requires a #p filter matching the authenticated pubkey")

// ErrReactionHGateMissing is returned by RelayClient.Subscribe when a
// filter requests reactions (kind:7) without a #h filter (FR-027).
var ErrReactionHGateMissing = errors.New("buzz: reaction subscription (kind 7) requires a #h filter naming the channel; a kinds-only subscription silently receives nothing")

// validateSubscriptionFilter guards against both protocol traps documented
// in the PRD's "Two protocol traps we must design around" section, in our
// own code, before f is ever translated and sent to the relay. It is
// called from RelayClient.Subscribe itself (not only from a caller-side
// helper) so no future call site -- including the P1 DM work FR-016
// explicitly anticipates -- can bypass it.
func validateSubscriptionFilter(f domain.Filter, selfPubKeyHex string) error {
	for _, k := range f.Kinds {
		if pGatedKinds[k] {
			if err := requirePTag(f, selfPubKeyHex); err != nil {
				return fmt.Errorf("buzz: subscribe: kind %d: %w", k, err)
			}
		}
		if k == reactionKind {
			if err := requireHTag(f); err != nil {
				return fmt.Errorf("buzz: subscribe: kind %d: %w", k, err)
			}
		}
	}
	return nil
}

func requirePTag(f domain.Filter, selfPubKeyHex string) error {
	ps := f.Tags["p"]
	if len(ps) == 0 {
		return ErrPGateFilterMissing
	}
	for _, p := range ps {
		if p != selfPubKeyHex {
			return ErrPGateFilterMissing
		}
	}
	return nil
}

func requireHTag(f domain.Filter) error {
	if len(f.Tags["h"]) == 0 {
		return ErrReactionHGateMissing
	}
	return nil
}
