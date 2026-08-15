package buzz

import "github.com/stainedhead/dev-team-bots/boabot/internal/domain"

// triggerKind classifies why (or whether) an inbound event qualifies as a
// task trigger (F10/FR-019). It is a small enum rather than a bool so a
// second trigger source is additive -- a new case here plus a new branch at
// the dispatch call site, not a rewrite of an if-chain. triggerThreadReply
// (P1.2/FR-205) is exactly that: an in-thread reply that lacks a fresh
// @mention but matches a thread this persona previously dispatched in
// (BuzzTaskDispatcher.KnownThread) -- see monitor.go's handleChannelEvent
// and matchKnownThread.
type triggerKind int

const (
	triggerNone triggerKind = iota
	triggerMention
	triggerThreadReply
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

// threadReplyCandidates returns candidate thread-root IDs for evt, in
// priority order, for matching against BuzzTaskDispatcher.KnownThread when
// classifyTrigger did not find an explicit @mention (P1.2/FR-205). NIP-10
// tagging is inconsistent in the wild: modern clients mark e-tags
// "root"/"reply" explicitly; older/simpler clients rely on the deprecated
// positional convention (first e tag = root, last e tag = reply) or send a
// single unmarked e tag. This collects every plausible root candidate --
// root-marked, then reply-marked, then positional first/last -- rather
// than relying on rootEventID's marked-tag-only lookup, so a genuine
// in-thread reply isn't missed just because the sending client used a
// different (still NIP-10-legal) tagging convention. Deduplicated, order
// preserved: an explicitly root-marked tag is checked before positional
// fallbacks, since it is the least ambiguous signal. Returns nil for an
// event with no `e` tags at all -- such an event cannot be a reply within
// any existing thread.
func threadReplyCandidates(evt domain.Event) []string {
	var eTags [][]string
	for _, t := range evt.Tags {
		if len(t) >= 2 && t[0] == "e" {
			eTags = append(eTags, t)
		}
	}
	if len(eTags) == 0 {
		return nil
	}

	var candidates []string
	seen := make(map[string]bool, len(eTags))
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			candidates = append(candidates, id)
		}
	}

	for _, t := range eTags {
		if len(t) >= 4 && t[3] == "root" {
			add(t[1])
		}
	}
	for _, t := range eTags {
		if len(t) >= 4 && t[3] == "reply" {
			add(t[1])
		}
	}
	// Deprecated positional convention: first e tag = root, last e tag =
	// reply (may be the same tag if there is only one e tag).
	add(eTags[0][1])
	add(eTags[len(eTags)-1][1])

	return candidates
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
