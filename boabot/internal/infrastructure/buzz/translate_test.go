package buzz

import (
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func mustSecretKey(t *testing.T) nostr.SecretKey {
	t.Helper()
	sk := nostr.Generate()
	return sk
}

// --- Event round-trips -------------------------------------------------

func TestEventRoundTrip_SignedEvent(t *testing.T) {
	sk := mustSecretKey(t)

	libEvt := nostr.Event{
		CreatedAt: 1735689600,
		Kind:      9,
		Tags:      nostr.Tags{{"h", "channel-uuid"}, {"e", "root-id"}},
		Content:   "hello, channel",
	}
	if err := libEvt.Sign(sk); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	dEvt := FromLibraryEvent(libEvt)

	if dEvt.ID != libEvt.ID.Hex() {
		t.Errorf("ID mismatch: got %q want %q", dEvt.ID, libEvt.ID.Hex())
	}
	if dEvt.PubKey != libEvt.PubKey.Hex() {
		t.Errorf("PubKey mismatch: got %q want %q", dEvt.PubKey, libEvt.PubKey.Hex())
	}
	if dEvt.CreatedAt != int64(libEvt.CreatedAt) {
		t.Errorf("CreatedAt mismatch: got %d want %d", dEvt.CreatedAt, libEvt.CreatedAt)
	}
	if dEvt.Kind != int(libEvt.Kind) {
		t.Errorf("Kind mismatch: got %d want %d", dEvt.Kind, libEvt.Kind)
	}
	if dEvt.Content != libEvt.Content {
		t.Errorf("Content mismatch: got %q want %q", dEvt.Content, libEvt.Content)
	}
	if len(dEvt.Tags) != 2 || dEvt.Tags[0][0] != "h" || dEvt.Tags[0][1] != "channel-uuid" {
		t.Errorf("Tags mismatch: got %+v", dEvt.Tags)
	}

	// Now translate back to a library event and confirm every field
	// round-trips byte for byte, including the signature -- this is the
	// "encode then decode" direction of the required round-trip test.
	back, err := ToLibraryEvent(dEvt)
	if err != nil {
		t.Fatalf("ToLibraryEvent: %v", err)
	}
	if back.ID != libEvt.ID {
		t.Errorf("ID did not round-trip: got %s want %s", back.ID.Hex(), libEvt.ID.Hex())
	}
	if back.PubKey != libEvt.PubKey {
		t.Errorf("PubKey did not round-trip: got %s want %s", back.PubKey.Hex(), libEvt.PubKey.Hex())
	}
	if back.Sig != libEvt.Sig {
		t.Errorf("Sig did not round-trip")
	}
	if back.CreatedAt != libEvt.CreatedAt || back.Kind != libEvt.Kind || back.Content != libEvt.Content {
		t.Errorf("scalar fields did not round-trip: got %+v want %+v", back, libEvt)
	}
	if !back.VerifySignature() {
		t.Error("round-tripped event should still verify its own signature")
	}
}

func TestEventRoundTrip_UnsignedEmptyEvent(t *testing.T) {
	// An unsigned, freshly-constructed domain.Event (as a caller would
	// build before Publish signs it) has empty ID/PubKey/Sig strings.
	// These must decode/encode to zero values on the library side, not
	// error or produce 64 zero-hex-char strings.
	dEvt := domain.Event{
		Kind:    9,
		Tags:    [][]string{{"h", "channel-uuid"}},
		Content: "unsigned",
	}

	libEvt, err := ToLibraryEvent(dEvt)
	if err != nil {
		t.Fatalf("ToLibraryEvent: %v", err)
	}
	if libEvt.ID != nostr.ZeroID {
		t.Errorf("expected zero ID, got %s", libEvt.ID.Hex())
	}
	if libEvt.PubKey != nostr.ZeroPK {
		t.Errorf("expected zero PubKey, got %s", libEvt.PubKey.Hex())
	}
	if libEvt.Sig != ([64]byte{}) {
		t.Errorf("expected zero Sig, got %x", libEvt.Sig)
	}

	back := FromLibraryEvent(libEvt)
	if back.ID != "" {
		t.Errorf("expected empty ID string on decode of zero ID, got %q", back.ID)
	}
	if back.PubKey != "" {
		t.Errorf("expected empty PubKey string on decode of zero PubKey, got %q", back.PubKey)
	}
	if back.Sig != "" {
		t.Errorf("expected empty Sig string on decode of zero Sig, got %q", back.Sig)
	}
	if back.Content != dEvt.Content || back.Kind != dEvt.Kind {
		t.Errorf("scalar fields did not round-trip: got %+v want %+v", back, dEvt)
	}
	if len(back.Tags) != 1 || back.Tags[0][0] != "h" {
		t.Errorf("tags did not round-trip: got %+v", back.Tags)
	}
}

func TestToLibraryEvent_KindOutOfRange(t *testing.T) {
	for _, k := range []int{-1, 65536, 1 << 20} {
		_, err := ToLibraryEvent(domain.Event{Kind: k})
		if err == nil {
			t.Errorf("ToLibraryEvent(Kind=%d): expected error, got nil", k)
		}
	}
}

func TestToLibraryEvent_MalformedHex(t *testing.T) {
	cases := map[string]domain.Event{
		"bad id":     {ID: "not-hex"},
		"bad pubkey": {PubKey: "not-hex"},
		"bad sig":    {Sig: "not-hex"},
	}
	for name, evt := range cases {
		if _, err := ToLibraryEvent(evt); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

// --- Filter round-trips --------------------------------------------------

func TestFilterRoundTrip_Full(t *testing.T) {
	since := int64(1000)
	until := int64(2000)
	dFilter := domain.Filter{
		Kinds: []int{9, 7},
		Tags:  map[string][]string{"h": {"channel-uuid"}, "p": {"pubkey-hex"}},
		Since: &since,
		Until: &until,
		Limit: 50,
	}

	libFilter, err := ToLibraryFilter(dFilter)
	if err != nil {
		t.Fatalf("ToLibraryFilter: %v", err)
	}
	if len(libFilter.Kinds) != 2 || libFilter.Kinds[0] != 9 || libFilter.Kinds[1] != 7 {
		t.Errorf("Kinds mismatch: got %+v", libFilter.Kinds)
	}
	if got := libFilter.Tags["h"]; len(got) != 1 || got[0] != "channel-uuid" {
		t.Errorf("Tags[h] mismatch: got %+v", got)
	}
	if libFilter.Since != nostr.Timestamp(since) || libFilter.Until != nostr.Timestamp(until) {
		t.Errorf("Since/Until mismatch: got %d/%d", libFilter.Since, libFilter.Until)
	}
	if libFilter.Limit != 50 {
		t.Errorf("Limit mismatch: got %d", libFilter.Limit)
	}

	back := FromLibraryFilter(libFilter)
	if len(back.Kinds) != 2 || back.Kinds[0] != 9 || back.Kinds[1] != 7 {
		t.Errorf("Kinds did not round-trip: got %+v", back.Kinds)
	}
	if got := back.Tags["h"]; len(got) != 1 || got[0] != "channel-uuid" {
		t.Errorf("Tags[h] did not round-trip: got %+v", got)
	}
	if back.Since == nil || *back.Since != since {
		t.Errorf("Since did not round-trip: got %v", back.Since)
	}
	if back.Until == nil || *back.Until != until {
		t.Errorf("Until did not round-trip: got %v", back.Until)
	}
	if back.Limit != 50 {
		t.Errorf("Limit did not round-trip: got %d", back.Limit)
	}
}

func TestFilterRoundTrip_NilSinceUntil(t *testing.T) {
	dFilter := domain.Filter{Kinds: []int{7}, Tags: map[string][]string{"h": {"x"}}}

	libFilter, err := ToLibraryFilter(dFilter)
	if err != nil {
		t.Fatalf("ToLibraryFilter: %v", err)
	}
	if libFilter.Since != 0 || libFilter.Until != 0 {
		t.Errorf("expected zero Since/Until for nil bounds, got %d/%d", libFilter.Since, libFilter.Until)
	}

	back := FromLibraryFilter(libFilter)
	if back.Since != nil || back.Until != nil {
		t.Errorf("expected nil Since/Until round-trip, got %v/%v", back.Since, back.Until)
	}
}

func TestToLibraryFilter_KindOutOfRange(t *testing.T) {
	_, err := ToLibraryFilter(domain.Filter{Kinds: []int{70000}})
	if err == nil {
		t.Fatal("expected error for out-of-range kind")
	}
}

// TestReactionFilter_RequiresHTag documents and asserts the "reaction
// channel derivation" protocol trap from the PRD's Background section:
// a reaction subscription MUST be {"kinds":[7],"#h":[channel-uuid]}, never
// kinds-only -- the relay silently drops a kinds-only reaction
// subscription. This is a translation-layer sanity check, not the full
// guard (that is Phase F's F18); it just confirms the #h tag survives
// translation unmolested.
func TestReactionFilter_HTagSurvivesTranslation(t *testing.T) {
	dFilter := domain.Filter{Kinds: []int{7}, Tags: map[string][]string{"h": {"channel-uuid"}}}
	libFilter, err := ToLibraryFilter(dFilter)
	if err != nil {
		t.Fatalf("ToLibraryFilter: %v", err)
	}
	if got := libFilter.Tags["h"]; len(got) != 1 || got[0] != "channel-uuid" {
		t.Fatalf("expected #h tag to survive translation, got %+v", libFilter.Tags)
	}
}

func TestToLibraryEvent_TagsNilStaysNil(t *testing.T) {
	libEvt, err := ToLibraryEvent(domain.Event{Kind: 0, Tags: nil})
	if err != nil {
		t.Fatalf("ToLibraryEvent: %v", err)
	}
	if libEvt.Tags != nil {
		t.Errorf("expected nil Tags to stay nil, got %+v", libEvt.Tags)
	}
}

func TestFromLibraryEvent_SigHexLowercase(t *testing.T) {
	sk := mustSecretKey(t)
	libEvt := nostr.Event{Kind: 1, Content: "x"}
	if err := libEvt.Sign(sk); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	dEvt := FromLibraryEvent(libEvt)
	if strings.ToLower(dEvt.Sig) != dEvt.Sig {
		t.Errorf("expected lowercase hex sig, got %q", dEvt.Sig)
	}
	if len(dEvt.Sig) != 128 {
		t.Errorf("expected 128 hex chars for a 64-byte sig, got %d", len(dEvt.Sig))
	}
}
