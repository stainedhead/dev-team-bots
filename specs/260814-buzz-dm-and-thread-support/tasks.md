# Tasks: Buzz DM Support and Full Threaded-Reply Support

**Created:** 2026-08-14
**Status:** Planning

## Progress Summary

0/10 tasks complete.

## Phase 1 — Threading fixes (independent of DM path, do first — smaller and unblocks FR-206's history-replay pattern reuse)

### P1.1 — Fix `DirectTask.ThreadID` to use NIP-10 thread root, not channel UUID

- **Depends on:** none
- **Acceptance criteria:** `monitor.go`'s `dispatchViaBridge` passes `root` (already computed via `rootEventID`) instead of `channelUUID` as `threadID`. `BuzzTaskDispatcher.Dispatch`'s doc comment corrected. Regression test: two concurrent threads in the same channel get independent `ChatTaskManager.pendingMap` entries (FR-208).

### P1.2 — Add `triggerThreadReply` classification for in-thread replies without re-mention

- **Depends on:** P1.3 (needs `KnownThread` to check against)
- **Acceptance criteria:** New `triggerThreadReply` kind in `trigger.go`; new NIP-10 reply/root-tag detection helper; `handleChannelEvent` computes `root` and checks `KnownThread` when `classifyTrigger` returns `triggerNone`, dispatching via the thread-reply path if true (FR-205). TDD: failing test first — a reply-without-mention event, currently dropped, must dispatch after the fix.

### P1.3 — `BuzzTaskBridge.KnownThread` + `dispatchedThreads` tracking

- **Depends on:** P1.1 (needs correct `root`-keyed `threadID` to track against)
- **Acceptance criteria:** New `KnownThread(botName, rootID string) bool` method on `BuzzTaskDispatcher` interface, implemented by `BuzzTaskBridge` via a new `dispatchedThreads map[string]time.Time` sibling to `seenEvts`, populated inside `Dispatch` under the existing lock.

### P1.4 — Complete outbound NIP-10 tagging on `publishReply`

- **Depends on:** none
- **Acceptance criteria:** `publishReply` adds a `reply`-marked `e` tag (immediate parent) and a `p` tag (original author) alongside the existing root `e` tag (FR-207). TDD: test asserting all three tags present and correctly marked on a published reply event.

### P1.5 — Thread-continuation via `ChatStore` history replay

- **Depends on:** P1.1, P1.2, P1.3
- **Acceptance criteria:** `BuzzTaskBridge`'s instruction-building for a `triggerThreadReply`-dispatched (or DM) message replays recent `ChatStore` history for that `threadID`, mirroring `handleChatSend`'s "Prior conversation" prepend pattern (FR-206). TDD: a two-turn thread conversation where the second turn's response reflects context from the first.

## Phase 2 — DM support (largely independent of Phase 1, can run in parallel via worktree/agent teammate)

### P2.1 — `nostr.Keyer` adapter over per-persona key material

- **Depends on:** none
- **Acceptance criteria:** New adapter in `internal/infrastructure/buzz` satisfying `nip17`'s required Signer+Cipher contract, backed by each persona's existing `buzz_private_key` resolution (no new secret type).

### P2.2 — DM subscription and inbound decrypt

- **Depends on:** P2.1
- **Acceptance criteria:** Each Buzz-enabled persona subscribes via `nip17.ListenForMessages` (FR-201); inbound kind:1059 events are gift-unwrapped to plaintext kind:14 rumors (FR-202). Self-authored rumors (the `toUs` self-copy `nip17.PrepareMessage` produces for every outbound DM) are detected via `rumor.pubkey == self` and skipped, not dispatched — see spec.md Edge Cases (self-message-loop risk). TDD: synthetic gift-wrapped event decrypts to expected plaintext; a self-authored rumor does not trigger dispatch.

### P2.3 — DM author-gating and dispatch

- **Depends on:** P2.2, P1.1 (reuses corrected `ThreadID` pattern for DM conversation id), P1.5 (reuses `ChatStore` history-replay for DM continuation)
- **Acceptance criteria:** Decrypted DMs pass through the existing author-gate before dispatch (FR-204 — unauthorized senders don't trigger dispatch); authorized DMs dispatch through `BuzzTaskBridge` identically to channel messages, tagged `DirectTaskSourceBuzz`, visibly DM-labeled on the board (FR-203). TDD: unauthorized-sender DM does not create a `DirectTask`; authorized-sender DM does.

### P2.4 — DM reply publishing

- **Depends on:** P2.1, P2.3
- **Acceptance criteria:** Outbound DM replies use `nip17.PrepareMessage`/`PublishMessage` (FR-209), distinct from `publishReply`. TDD: a published DM reply gift-unwraps correctly for the original sender's keypair in a test.

## Phase 3 — Security/observability verification (cross-cutting, do after Phases 1-2 land)

### P3.1 — Log-safety and privacy-property audit

- **Depends on:** all above
- **Acceptance criteria:** No private key, `nip44` conversation key, or decrypted DM plaintext appears in any log statement (grep-verifiable). NIP-17 ephemeral-key/timestamp-randomization behavior confirmed unmodified from the library's defaults (no custom `modify` callback that reintroduces correlatable metadata).
