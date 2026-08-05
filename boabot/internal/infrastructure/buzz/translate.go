// Package buzz implements domain.RelayClient over fiatjaf.com/nostr,
// confining that library (and every other Nostr-protocol dependency) to
// this package per FR-033/FR-038. See internal/domain/buzz.go for the
// domain-owned Event/Filter types this package translates to and from.
package buzz

import (
	"encoding/hex"
	"fmt"

	"fiatjaf.com/nostr"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ToLibraryEvent translates a domain.Event into a fiatjaf.com/nostr Event.
// Empty ID/PubKey/Sig strings (as on an unsigned event awaiting Publish to
// sign it) translate to the library's zero values, not an error -- only a
// non-empty value that fails to parse as hex of the expected length is
// rejected.
func ToLibraryEvent(evt domain.Event) (nostr.Event, error) {
	var out nostr.Event

	id, err := hexID(evt.ID)
	if err != nil {
		return out, fmt.Errorf("buzz: translate event id: %w", err)
	}
	pk, err := hexPubKey(evt.PubKey)
	if err != nil {
		return out, fmt.Errorf("buzz: translate event pubkey: %w", err)
	}
	sig, err := hexSig(evt.Sig)
	if err != nil {
		return out, fmt.Errorf("buzz: translate event sig: %w", err)
	}
	if evt.Kind < 0 || evt.Kind > 0xFFFF {
		return out, fmt.Errorf("buzz: translate event kind: %d is out of the valid 0-65535 range", evt.Kind)
	}

	out = nostr.Event{
		ID:        id,
		PubKey:    pk,
		CreatedAt: nostr.Timestamp(evt.CreatedAt),
		Kind:      nostr.Kind(evt.Kind),
		Tags:      toLibraryTags(evt.Tags),
		Content:   evt.Content,
		Sig:       sig,
	}
	return out, nil
}

// FromLibraryEvent translates a fiatjaf.com/nostr Event into a
// domain.Event. Zero-valued ID/PubKey/Sig translate to empty strings, not
// their 64/128-zero-hex-character encoding.
func FromLibraryEvent(evt nostr.Event) domain.Event {
	d := domain.Event{
		CreatedAt: int64(evt.CreatedAt),
		Kind:      int(evt.Kind),
		Tags:      fromLibraryTags(evt.Tags),
		Content:   evt.Content,
	}
	if evt.ID != nostr.ZeroID {
		d.ID = evt.ID.Hex()
	}
	if evt.PubKey != nostr.ZeroPK {
		d.PubKey = evt.PubKey.Hex()
	}
	if evt.Sig != ([64]byte{}) {
		d.Sig = hex.EncodeToString(evt.Sig[:])
	}
	return d
}

// ToLibraryFilter translates a domain.Filter into a fiatjaf.com/nostr
// Filter. Nil Since/Until translate to the library's absent-bound zero
// value; note that (per NIP-01 as implemented by this library) an
// explicit *int64 pointing at 0 is therefore indistinguishable from an
// absent bound once translated -- this is a documented, accepted
// limitation of the library's own Timestamp representation, not a bug in
// this translation.
func ToLibraryFilter(f domain.Filter) (nostr.Filter, error) {
	var out nostr.Filter

	if f.Kinds != nil {
		out.Kinds = make([]nostr.Kind, len(f.Kinds))
		for i, k := range f.Kinds {
			if k < 0 || k > 0xFFFF {
				return out, fmt.Errorf("buzz: translate filter kind: %d is out of the valid 0-65535 range", k)
			}
			out.Kinds[i] = nostr.Kind(k)
		}
	}

	if f.Tags != nil {
		out.Tags = make(nostr.TagMap, len(f.Tags))
		for k, v := range f.Tags {
			cp := make([]string, len(v))
			copy(cp, v)
			out.Tags[k] = cp
		}
	}

	if f.Since != nil {
		out.Since = nostr.Timestamp(*f.Since)
	}
	if f.Until != nil {
		out.Until = nostr.Timestamp(*f.Until)
	}
	out.Limit = f.Limit

	return out, nil
}

// FromLibraryFilter translates a fiatjaf.com/nostr Filter into a
// domain.Filter. A zero Since/Until (the library's "absent" value)
// translates to nil, matching ToLibraryFilter's nil handling.
func FromLibraryFilter(f nostr.Filter) domain.Filter {
	var out domain.Filter

	if f.Kinds != nil {
		out.Kinds = make([]int, len(f.Kinds))
		for i, k := range f.Kinds {
			out.Kinds[i] = int(k)
		}
	}

	if f.Tags != nil {
		out.Tags = make(map[string][]string, len(f.Tags))
		for k, v := range f.Tags {
			cp := make([]string, len(v))
			copy(cp, v)
			out.Tags[k] = cp
		}
	}

	if f.Since != 0 {
		since := int64(f.Since)
		out.Since = &since
	}
	if f.Until != 0 {
		until := int64(f.Until)
		out.Until = &until
	}
	out.Limit = f.Limit

	return out
}

func toLibraryTags(tags [][]string) nostr.Tags {
	if tags == nil {
		return nil
	}
	out := make(nostr.Tags, len(tags))
	for i, t := range tags {
		out[i] = nostr.Tag(t)
	}
	return out
}

func fromLibraryTags(tags nostr.Tags) [][]string {
	if tags == nil {
		return nil
	}
	out := make([][]string, len(tags))
	for i, t := range tags {
		out[i] = []string(t)
	}
	return out
}

func hexID(s string) (nostr.ID, error) {
	if s == "" {
		return nostr.ZeroID, nil
	}
	return nostr.IDFromHex(s)
}

func hexPubKey(s string) (nostr.PubKey, error) {
	if s == "" {
		return nostr.ZeroPK, nil
	}
	return nostr.PubKeyFromHexCheap(s)
}

func hexSig(s string) ([64]byte, error) {
	var out [64]byte
	if s == "" {
		return out, nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return out, fmt.Errorf("%q is not valid hex: %w", s, err)
	}
	if len(b) != 64 {
		return out, fmt.Errorf("sig should be 64 bytes (128 hex chars), got %d bytes", len(b))
	}
	copy(out[:], b)
	return out, nil
}
