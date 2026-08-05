package buzz

import "github.com/stainedhead/dev-team-bots/boabot/internal/domain"

// triggerKind classifies why (or whether) an inbound event qualifies as a
// task trigger (F10/FR-019). It is a small enum rather than a bool so a
// second trigger source (DMs, kind:1059, explicitly out of scope this run)
// is additive later -- a new case here plus a new branch in
// classifyTrigger, not a rewrite of an if-chain scattered through the
// dispatch call site.
type triggerKind int

const (
	triggerNone triggerKind = iota
	triggerMention
)

// classifyTrigger implements F10: an inbound event qualifies as a task
// trigger only when it is a kind:9 channel message carrying a #p tag
// referencing the agent's own pubkey. This is the standard Nostr
// convention for an addressed mention/notification (the same primitive
// F3/F4's own p-gated subscriptions rely on), chosen over parsing evt.Content
// for a nostr:npub1... URI -- simpler, protocol-correct, and testable
// without bech32 codec plumbing. See implementation-notes.md for this
// judgment call (neither the PRD nor architecture.md specifies the exact
// tag-vs-content detection mechanism).
func classifyTrigger(evt domain.Event, selfPubKeyHex string) triggerKind {
	if evt.Kind != kindChannelMessage {
		return triggerNone
	}
	if !hasTagValue(evt.Tags, "p", selfPubKeyHex) {
		return triggerNone
	}
	return triggerMention
}

// authorGate implements F8's inbound author gate (FR-029) and F17's wider
// !shutdown gate (FR-026).
type authorGate struct {
	respondTo string
	allowlist []string // nil = no gate; non-nil (including empty) = allow-list gate active
}

func newAuthorGate(cfg Config) authorGate {
	return authorGate{respondTo: cfg.RespondTo, allowlist: cfg.RespondToAllowlist}
}

// active reports whether F8's gate is configured at all. Per
// architecture.md's "Empty vs. unset respond_to_allowlist" edge case: a nil
// allowlist and an empty respond_to together mean no gate (allow
// everyone); an explicitly configured empty allowlist ([]string{},
// non-nil) means the gate IS active and allows no one unless respond_to
// also matches. The constructor/caller MUST pass the config's
// RespondToAllowlist through unmodified -- a defensive copy that turns nil
// into a non-nil empty slice would silently convert "no gate" into
// "allow-none", exactly the lockout this distinction exists to prevent.
func (g authorGate) active() bool {
	return g.respondTo != "" || g.allowlist != nil
}

// allows implements F8's ordinary dispatch gate.
func (g authorGate) allows(pubkeyHex string) bool {
	if !g.active() {
		return true
	}
	if g.respondTo != "" && pubkeyHex == g.respondTo {
		return true
	}
	for _, p := range g.allowlist {
		if p == pubkeyHex {
			return true
		}
	}
	return false
}

// allowsShutdown implements FR-026's wider three-way gate for the
// !shutdown control command specifically: respond_to, or a member of
// respond_to_allowlist, or the configured owner_pubkey. The owner override
// applies ONLY here, not to the ordinary F8 dispatch gate (allows), so an
// owner who isn't in an explicitly configured allowlist can still always
// stop their own bot.
func (g authorGate) allowsShutdown(pubkeyHex, ownerPubkeyHex string) bool {
	if ownerPubkeyHex != "" && pubkeyHex == ownerPubkeyHex {
		return true
	}
	return g.allows(pubkeyHex)
}

// rootEventID implements F6's "referencing the mention's root event": if
// evt already carries a NIP-10 "root"-marked e tag (["e", id, relay-hint,
// "root"]) -- i.e. it is itself a reply within an existing thread -- that
// referenced event is the thread's root. Otherwise evt is the first
// message of its own thread, so it is its own root.
func rootEventID(evt domain.Event) string {
	for _, t := range evt.Tags {
		if len(t) >= 4 && t[0] == "e" && t[3] == "root" {
			return t[1]
		}
	}
	return evt.ID
}

// hasTagValue reports whether tags contains a tag named name whose first
// value equals value.
func hasTagValue(tags [][]string, name, value string) bool {
	for _, t := range tags {
		if len(t) >= 2 && t[0] == name && t[1] == value {
			return true
		}
	}
	return false
}

// firstTagValue returns the first value of the first tag named name, or ""
// if no such tag exists.
func firstTagValue(tags [][]string, name string) string {
	for _, t := range tags {
		if len(t) >= 2 && t[0] == name {
			return t[1]
		}
	}
	return ""
}
