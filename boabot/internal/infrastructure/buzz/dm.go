// dm.go implements Buzz's NIP-17 direct-message support (P2.1-P2.4):
// subscribe + gift-unwrap inbound DMs, author-gate + dispatch through the
// same domain.BuzzTaskDispatcher bridge channel messages use, and a
// gift-wrap reply-publish path distinct from the channel-shaped
// publishReply (FR-209).
//
// Deviation from architecture.md's literal "use nip17.ListenForMessages/
// PublishMessage" (documented in implementation-notes.md): both of those
// functions require a *nostr.Pool, which this codebase does not have --
// Monitor is built around a single-relay relayClient seam (FR-201: "using
// the same relay connection ... it already uses for channel
// participation"), not a pool of relays. Introducing a parallel
// nostr.Pool-based connection just for DMs would violate that "same
// connection" requirement and duplicate connection-management logic this
// package already has. Instead:
//   - Outbound: nip17.PrepareMessage IS used directly -- it only needs a
//     nostr.Keyer, no Pool -- and its two gift-wrapped outputs (toUs,
//     toThem) are published via the existing relayClient.PublishRaw.
//   - Inbound: relayClient.Subscribe (the existing seam) opens the kind:1059
//     subscription, and nip59.GiftUnwrap (the exact primitive
//     nip17.ListenForMessages itself calls internally) unwraps each event --
//     not a reimplementation, just the same call without Pool plumbing
//     wrapped around it.
//
// This preserves NIP-17's privacy properties (ephemeral gift-wrap keys,
// randomized timestamps -- both handled entirely inside nip59.GiftWrap/
// GiftUnwrap, called either directly here or via nip17.PrepareMessage) and
// the "same relay connection and identity" requirement, without
// introducing new connection-management surface.
package buzz

import (
	"context"
	"errors"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip17"
	"fiatjaf.com/nostr/nip59"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// kindGiftWrap is NIP-59's gift-wrap kind (nostr.KindGiftWrap == 1059),
// named locally for consistency with this file's other kind consts
// (monitor.go's kindChannelMessage etc., which are also plain int
// literals rather than nostr.Kind values).
const kindGiftWrap = 1059

// dmBoardLabel prefixes a DM-dispatched instruction so it is visibly
// DM-originated wherever the instruction text surfaces -- the board item's
// title (buzzBoardTitle truncates the instruction) and description, and the
// Tasks list -- per architecture.md's decision to distinguish DM origin via
// the existing board-item title/metadata convention rather than a new
// domain.DirectTaskSource value (data-dictionary.md's "Value Objects").
const dmBoardLabel = "[Buzz DM] "

// dmReplyTarget is where HandleResult (via publishDMReply) sends a
// DM-dispatched task's eventual reply -- the DM analogue of replyTarget.
type dmReplyTarget struct {
	recipientPubKey nostr.PubKey
	threadID        string
}

// dmThreadID returns the stable per-conversation key for a 1:1 DM with
// counterparty (FR-208): a "dm:" prefix distinguishes it, in logs and in
// DirectTask.ThreadID, from a channel thread's bare NIP-10 root hex.
func dmThreadID(counterparty nostr.PubKey) string {
	return "dm:" + counterparty.Hex()
}

// startDMSubscription implements P2.2/FR-201: subscribes to kind:1059
// events #p-tagged to this persona's own pubkey (the p-gate guard.go
// already enforces on any such subscription filter) and processes each on
// its own goroutine loop. A subscribe failure is logged and isolated --
// channel monitoring, already started by the time run() calls this, is
// completely unaffected (spec.md's "Relay doesn't support kind:1059" edge
// case).
func (m *Monitor) startDMSubscription(ctx context.Context) {
	ch, err := m.relay.Subscribe(ctx, domain.Filter{
		Kinds: []int{kindGiftWrap},
		Tags:  map[string][]string{"p": {m.cfg.AgentPubKeyHex}},
	})
	if err != nil {
		m.logger.Error("buzz monitor: dm subscription failed; DMs will not be received (channel monitoring is unaffected)",
			"agent_pubkey", m.cfg.AgentPubKeyHex, "err", err)
		return
	}
	go m.dmLoop(ctx, ch)
}

// dmLoop consumes ch (the kind:1059 subscription) until ctx is done or the
// channel is closed (relay disconnect or Monitor shutdown).
func (m *Monitor) dmLoop(ctx context.Context, ch <-chan domain.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			m.handleDMEvent(ctx, evt)
		}
	}
}

// handleDMEvent implements P2.2 (gift-unwrap) and P2.3 (self-filter,
// author-gate, dispatch). evt.ID is the outer gift-wrap event's ID, used
// unchanged as Dispatch's eventID -- FR-101/relay-replay dedup is reused
// as-is (BuzzTaskBridge.checkAndMarkSeen), not a separate mechanism
// (spec.md's "Relay-replay of a DM event" edge case).
func (m *Monitor) handleDMEvent(ctx context.Context, evt domain.Event) {
	nevt, err := ToLibraryEvent(evt)
	if err != nil {
		// NFR Security: log only the outer event id and the translation
		// error -- never content (which, pre-unwrap, is gift-wrap
		// ciphertext, not plaintext, but is still never logged on
		// principle).
		m.logger.Warn("buzz monitor: dm event translate failed, skipping", "event_id", evt.ID, "err", err)
		return
	}

	rumor, err := nip59.GiftUnwrap(nevt, func(otherpubkey nostr.PubKey, ciphertext string) (string, error) {
		return m.dmKeyer.Decrypt(ctx, ciphertext, otherpubkey)
	})
	if err != nil {
		// spec.md's "Malformed/corrupted gift-wrap event" edge case: log and
		// skip, never crash the monitor goroutine.
		//
		// NFR Security: deliberately does NOT log err's text (only the
		// outer event id). nip44's conversation-key derivation -- called
		// transitively by dmKeyer.Decrypt via GenerateConversationKey, for
		// both the seal and rumor decrypt steps GiftUnwrap performs -- has
		// an error path (an out-of-range secret key) that formats the RAW
		// secret key bytes into its error string. LoadKeypair's own
		// validation makes that path unreachable for this persona's own
		// key in practice, but the only way to make the "never log key
		// material" requirement airtight (not merely "unreachable today")
		// is to never let this error's text reach a log statement at all.
		// "malformed/corrupted" + the outer event id is a sufficient
		// diagnostic for every other failure this call can produce
		// (invalid ciphertext shape, bad signature, non-JSON payload).
		m.logger.Warn("buzz monitor: dm gift-unwrap failed, skipping (malformed/corrupted event)",
			"event_id", evt.ID)
		return
	}

	selfPK, err := m.dmKeyer.GetPublicKey(ctx)
	if err != nil {
		m.logger.Warn("buzz monitor: dm keyer public key resolution failed, skipping", "event_id", evt.ID, "err", err)
		return
	}

	if rumor.PubKey == selfPK {
		// spec.md's "Self-message loop" edge case: nip17.PrepareMessage
		// (and our own publishDMReply, which calls it) produces a
		// self-copy (toUs) of every outbound DM, which this very
		// subscription (#p-tagged to self) also receives. Dispatching on
		// it would create an infinite self-reply loop. Silently skip --
		// this is expected, ordinary traffic, not an error.
		return
	}

	if m.taskDispatcher == nil {
		m.logger.Warn("buzz monitor: dm received but no task dispatcher wired, dropping", "event_id", evt.ID)
		return
	}

	// FR-204: reuse the exact same author-gate channel dispatch uses.
	// Unauthorized senders are silently ignored, not sent a decline reply
	// -- see implementation-notes.md's documented default (a decline reply
	// could leak which personas exist/respond to an arbitrary Nostr
	// identity, which a curated channel membership doesn't expose to the
	// same degree).
	if !m.gate.allows(rumor.PubKey.Hex()) {
		m.logger.Warn("buzz monitor: dm rejected by author gate", "event_id", evt.ID)
		return
	}

	text := strings.TrimSpace(rumor.Content)
	if text == "" {
		return
	}
	if n := len(rumor.Content); n > maxContentLen {
		m.logger.Warn("buzz monitor: dm content exceeds max size, rejecting",
			"event_id", evt.ID, "content_len", n, "max_content_len", maxContentLen)
		return
	}

	threadID := dmThreadID(rumor.PubKey)
	instruction := dmBoardLabel + m.screen("message_body", text)

	result, err := m.taskDispatcher.Dispatch(ctx, m.cfg.BotName, evt.ID, threadID, instruction)
	if err != nil {
		m.logger.Error("buzz monitor: dm task dispatcher bridge failed", "err", err, "event_id", evt.ID)
		return
	}
	if result.Duplicate {
		m.logger.Info("buzz monitor: duplicate dm event, skipping re-dispatch", "event_id", evt.ID)
		return
	}

	if result.Reply != "" {
		// FR-301: see publishDMReply's doc comment -- this immediate reply
		// has no DirectTask/TaskResultPayload, so recordOutbound must run
		// here (only on a successful publish, matching the pre-FR-301
		// behaviour for this path), not inside publishDMReply.
		if err := m.publishDMReply(ctx, rumor.PubKey, threadID, result.Reply); err == nil {
			m.recordOutbound(ctx, threadID, m.cfg.BotName, result.Reply)
		} // publishDMReply logs its own failure internally
	}
	if result.TaskID != "" && result.AwaitResult {
		m.awaitDMResult(result.TaskID, rumor.PubKey, threadID)
	}

	m.logger.Info("buzz monitor: dispatched dm task",
		"task_id", result.TaskID, "agent_pubkey", m.cfg.AgentPubKeyHex, "relay_url", m.cfg.RelayURL,
		"await_result", result.AwaitResult)
}

// awaitDMResult registers taskID in the pending map so a later HandleResult
// call publishes the eventual reply via the DM path. Unlike awaitResult
// (channel), no typing-indicator loop is started -- F16's typing indicator
// is a channel (#h-tagged) concept with no DM analogue.
func (m *Monitor) awaitDMResult(taskID string, recipient nostr.PubKey, threadID string) {
	m.mu.Lock()
	m.pending[taskID] = &pendingEntry{
		dmTarget: &dmReplyTarget{recipientPubKey: recipient, threadID: threadID},
	}
	m.mu.Unlock()
}

// publishDMReply implements FR-209: DM replies use their own outbound path
// (nip17.PrepareMessage's kind:14 rumor -> seal -> gift-wrap), distinct
// from publishReply, which hardcodes an "h" channel tag that does not apply
// to a 1:1 DM.
//
// modify is deliberately nil on every call (P3.1's explicit requirement):
// passing a non-nil modify here would risk reintroducing correlatable
// metadata into the gift-wrap envelope that nip59.GiftWrap's own
// ephemeral-key/timestamp-randomization behaviour is specifically designed
// to avoid.
//
// Both PrepareMessage outputs are published raw (relayClient.PublishRaw,
// not Publish -- see this file's package doc comment and PublishRaw's own
// doc comment for why Publish would break this): toUs (so the sender can
// see their own sent message from another device, per NIP-17) and toThem
// (the actual recipient copy). A toUs publish failure is logged but does
// not fail the call -- the recipient copy is what matters for the DM to be
// received at all; a toThem failure does fail the call.
//
// FR-301: unlike before, this method does NOT call recordOutbound itself --
// see publishReply's doc comment for the full rationale (this is the DM
// analogue of the same fix). HandleResult's DM task-completion replies are
// now recorded exactly once by TeamManager.handleSharedTaskResult;
// handleDMEvent's immediate-Reply branch (a scheduling confirmation prompt)
// calls recordOutbound itself, at its own call site, after this method
// returns successfully.
func (m *Monitor) publishDMReply(ctx context.Context, recipient nostr.PubKey, threadID, content string) error {
	if m.dmKeyer == nil {
		m.logger.Warn("buzz monitor: dm reply requested but no dm keyer wired, dropping")
		return errDMNotConfigured
	}

	toUs, toThem, err := nip17.PrepareMessage(ctx, content, nil, m.dmKeyer, recipient, nil)
	if err != nil {
		// NFR Security: no "err" field here either -- PrepareMessage's
		// error paths run through the same dmKeyer.Encrypt/nip44
		// conversation-key derivation as the inbound decrypt path above,
		// with the identical (LoadKeypair-guarded, but not
		// library-guaranteed) raw-key-in-error-text risk. See
		// handleDMEvent's gift-unwrap failure log for the full rationale.
		m.logger.Warn("buzz monitor: prepare dm reply failed")
		return err
	}

	if err := m.relay.PublishRaw(ctx, FromLibraryEvent(toUs)); err != nil {
		m.logger.Warn("buzz monitor: publish dm self-copy failed (recipient copy still attempted)", "err", err)
	}

	if err := m.relay.PublishRaw(ctx, FromLibraryEvent(toThem)); err != nil {
		m.logger.Warn("buzz monitor: publish dm reply failed", "err", err)
		return err
	}

	return nil
}

// errDMNotConfigured is returned by publishDMReply when no dmKeyer is
// wired (WithDMKeyer never called) -- DM support was never activated for
// this persona, so there is no key material to prepare a reply with.
var errDMNotConfigured = errors.New("buzz: dm support not configured (no dmKeyer wired)")
