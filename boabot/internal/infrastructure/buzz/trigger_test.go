package buzz

import (
	"errors"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// --- F10: trigger classification --------------------------------------

func TestClassifyTrigger_MentionViaPTag_IsMention(t *testing.T) {
	evt := domain.Event{Kind: 9, Tags: [][]string{{"h", "chan-1"}, {"p", "self-pk"}}, Content: "hey"}
	if got := classifyTrigger(evt, "self-pk"); got != triggerMention {
		t.Fatalf("expected triggerMention, got %v", got)
	}
}

func TestClassifyTrigger_NoPTag_IsNone(t *testing.T) {
	evt := domain.Event{Kind: 9, Tags: [][]string{{"h", "chan-1"}}, Content: "hey"}
	if got := classifyTrigger(evt, "self-pk"); got != triggerNone {
		t.Fatalf("expected triggerNone, got %v", got)
	}
}

func TestClassifyTrigger_PTagForSomeoneElse_IsNone(t *testing.T) {
	evt := domain.Event{Kind: 9, Tags: [][]string{{"p", "someone-else"}}, Content: "hey"}
	if got := classifyTrigger(evt, "self-pk"); got != triggerNone {
		t.Fatalf("expected triggerNone, got %v", got)
	}
}

func TestClassifyTrigger_NonChannelMessageKind_IsNone(t *testing.T) {
	// A #p tag on a non-kind:9 event (e.g. a kind:7 reaction) must not be
	// treated as a mention -- FR-019 scopes the P0 trigger surface to
	// "@mention ... in a channel it is subscribed to", i.e. kind:9 only.
	evt := domain.Event{Kind: 7, Tags: [][]string{{"p", "self-pk"}}}
	if got := classifyTrigger(evt, "self-pk"); got != triggerNone {
		t.Fatalf("expected triggerNone for non-kind:9 event, got %v", got)
	}
}

// --- F8: inbound author gate --------------------------------------------

func TestAuthorGate_NilAllowlistAndNoRespondTo_AllowsEveryone(t *testing.T) {
	g := authorGate{respondTo: "", allowlist: nil}
	if !g.allows("anyone") {
		t.Fatal("nil allowlist + no respond_to must allow everyone (no gate)")
	}
}

func TestAuthorGate_ExplicitEmptyAllowlist_AllowsNoOne(t *testing.T) {
	g := authorGate{respondTo: "", allowlist: []string{}}
	if g.allows("anyone") {
		t.Fatal("explicit empty allowlist must be an active allow-none gate")
	}
}

func TestAuthorGate_ExplicitEmptyAllowlist_ButRespondToMatches_Allowed(t *testing.T) {
	g := authorGate{respondTo: "owner-pk", allowlist: []string{}}
	if !g.allows("owner-pk") {
		t.Fatal("respond_to match must still be allowed even with an empty allowlist")
	}
	if g.allows("someone-else") {
		t.Fatal("non-matching sender must be rejected")
	}
}

func TestAuthorGate_NonEmptyAllowlist_MembershipChecked(t *testing.T) {
	g := authorGate{allowlist: []string{"a", "b"}}
	if !g.allows("a") || !g.allows("b") {
		t.Fatal("allowlist members must be allowed")
	}
	if g.allows("c") {
		t.Fatal("non-member must be rejected")
	}
}

func TestAuthorGate_AllowsShutdown_OwnerOverrideAppliesEvenWithActiveGate(t *testing.T) {
	// FR-026: !shutdown honours a WIDER gate than F8's ordinary dispatch
	// gate -- respond_to, allowlist, OR owner_pubkey. An owner who isn't
	// in an explicitly configured allowlist can still always stop their
	// own bot; the ordinary F8 gate (allows) must NOT grant this override.
	g := authorGate{allowlist: []string{}} // active, allow-none gate
	if g.allows("owner-pk") {
		t.Fatal("sanity: ordinary gate must reject owner-pk when not in the allowlist")
	}
	if !g.allowsShutdown("owner-pk", "owner-pk") {
		t.Fatal("owner_pubkey must always pass the !shutdown gate, even outside the allowlist")
	}
	if g.allowsShutdown("stranger", "owner-pk") {
		t.Fatal("non-owner, non-allowlisted sender must still be rejected for !shutdown")
	}
}

func TestAuthorGate_AllowsShutdown_NoGateConfigured_AllowsAnyone(t *testing.T) {
	// FR-026 gates on "the FR-029 author gate" -- when that base gate is
	// unconfigured (allows everyone), !shutdown inherits the same
	// permissiveness; this is a documented operator choice (see
	// implementation-notes.md), not an oversight.
	g := authorGate{}
	if !g.allowsShutdown("anyone", "") {
		t.Fatal("expected !shutdown to be allowed when no gate is configured at all")
	}
}

// --- F7: content screening -----------------------------------------------

type fakeScreener struct {
	screenFn func(string) (string, error)
	calls    []string
}

func (s *fakeScreener) Screen(content string) (string, error) {
	s.calls = append(s.calls, content)
	if s.screenFn != nil {
		return s.screenFn(content)
	}
	return "<untrusted-content>" + content + "</untrusted-content>", nil
}

func TestMonitor_Screen_WrapsViaScreener(t *testing.T) {
	fs := &fakeScreener{}
	m := &Monitor{screener: fs, logger: discardLogger()}
	got := m.screen("message_body", "hello")
	if got != "<untrusted-content>hello</untrusted-content>" {
		t.Fatalf("unexpected screened content: %q", got)
	}
	if len(fs.calls) != 1 || fs.calls[0] != "hello" {
		t.Fatalf("expected screener to be called with raw content, got %v", fs.calls)
	}
}

func TestMonitor_Screen_NilScreener_ReturnsOriginal(t *testing.T) {
	m := &Monitor{logger: discardLogger()}
	if got := m.screen("message_body", "hello"); got != "hello" {
		t.Fatalf("expected passthrough with nil screener, got %q", got)
	}
}

func TestMonitor_Screen_ErrorFailsOpenToOriginal(t *testing.T) {
	fs := &fakeScreener{screenFn: func(string) (string, error) { return "", errors.New("boom") }}
	m := &Monitor{screener: fs, logger: discardLogger()}
	if got := m.screen("message_body", "hello"); got != "hello" {
		t.Fatalf("expected original content on screener error, got %q", got)
	}
}

// --- F6: NIP-10 root-event resolution --------------------------------------

func TestRootEventID_NoRootTag_EventIsItsOwnRoot(t *testing.T) {
	evt := domain.Event{ID: "evt-1"}
	if got := rootEventID(evt); got != "evt-1" {
		t.Fatalf("expected evt-1, got %s", got)
	}
}

func TestRootEventID_ExistingRootTag_UsesIt(t *testing.T) {
	evt := domain.Event{ID: "evt-2", Tags: [][]string{{"e", "evt-1", "", "root"}}}
	if got := rootEventID(evt); got != "evt-1" {
		t.Fatalf("expected evt-1 (the existing root), got %s", got)
	}
}

func TestRootEventID_NonRootETag_Ignored(t *testing.T) {
	evt := domain.Event{ID: "evt-3", Tags: [][]string{{"e", "evt-1", "", "reply"}}}
	if got := rootEventID(evt); got != "evt-3" {
		t.Fatalf("expected evt-3 (its own root, reply marker doesn't count), got %s", got)
	}
}

// --- tag helpers ------------------------------------------------------------

func TestHasTagValue(t *testing.T) {
	tags := [][]string{{"h", "chan-1"}, {"p", "pk-1"}}
	if !hasTagValue(tags, "p", "pk-1") {
		t.Fatal("expected match")
	}
	if hasTagValue(tags, "p", "pk-2") {
		t.Fatal("expected no match")
	}
	if hasTagValue(tags, "e", "chan-1") {
		t.Fatal("expected no match for wrong tag name")
	}
}

func TestFirstTagValue(t *testing.T) {
	tags := [][]string{{"h", "chan-1"}, {"h", "chan-2"}}
	if got := firstTagValue(tags, "h"); got != "chan-1" {
		t.Fatalf("expected first match chan-1, got %s", got)
	}
	if got := firstTagValue(tags, "d"); got != "" {
		t.Fatalf("expected empty string for absent tag, got %s", got)
	}
}
