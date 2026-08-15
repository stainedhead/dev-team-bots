# Research: Buzz DM Support and Full Threaded-Reply Support

**Created:** 2026-08-14
**Source PRD:** [buzz-dm-and-thread-support-PRD.md](./buzz-dm-and-thread-support-PRD.md)

## Research Questions

1. ~~Conversation-continuation mechanism~~ — **Resolved.** No Worker/Task-level continuation exists (`domain.Task`, `internal/domain/worker.go:9-15`, has no session field; `ExecuteTaskUseCase.Execute` builds `messages` fresh from `task.Instruction` alone every call, `execute_task.go:116`). The web-UI chat path doesn't resume a task either — every message dispatches a brand-new `DirectTask`. "Continuation" is achieved entirely at the instruction-string layer: `handleChatSend` (`http/server.go:1739-1761`) fetches the last 10 messages from `domain.ChatStore` (keyed by `threadID`, `direct_task.go:130-144`) and prepends a "Prior conversation" block to the new instruction before dispatch. **Decision:** reuse this exact pattern for Buzz — append each inbound/outbound message to `ChatStore` keyed by the NIP-10 thread root (or DM conversation id), and build each dispatch's instruction by replaying that thread's history, same as `handleChatSend`. No new session/continuation machinery needed.
2. ~~`nip44`/`nip59` API shape~~ — **Resolved, and better than expected.** The vendored `fiatjaf.com/nostr` (`go.mod:6`) has a complete `nip17` package built on top of `nip44`/`nip59` requiring only a `nostr.Keyer` (Signer+Cipher): `nip17.PrepareMessage(ctx, content, tags, kr, recipientPubKey, modify)` builds the kind:14 rumor and gift-wraps it (to self + recipient); `nip17.PublishMessage(...)` publishes both copies with auth-retry; `nip17.ListenForMessages(ctx, pool, kr, ourRelays, since)` subscribes kind:1059 `#p`-tagged to self, gift-unwraps, yields plaintext kind:14 rumors. **Decision:** implement a `nostr.Keyer` adapter over boabot's existing per-persona key material and call `nip17.PrepareMessage`/`PublishMessage`/`ListenForMessages` directly — do not hand-roll `nip44`/`nip59` calls, the library already does the correct thing (raw signatures: `nip44.GenerateConversationKey`/`Encrypt`/`Decrypt` at `nip44/nip44.go:166,39,99`; `nip59.GiftWrap`/`GiftUnwrap` at `nip59/nip59.go:14,67`, confirming ephemeral-key + randomized-timestamp privacy properties are handled by the library, not something to reimplement).
3. Does the configured Buzz relay (`wss://feral-sysd.communities.buzz.xyz`) actually support gift-wrap (kind:1059) event kinds — subscription and publish? **Not resolvable by static code reading** — deferred to implementation-time verification against the live relay (first DM subscription test will confirm or fail fast).
4. ~~`ThreadID` keying fix~~ — **Resolved — a one-argument fix, no structural change.** `ChatTaskManager.pendingMap` is `sync.Map` keyed by an opaque `threadID string` (`chat_task_manager.go:31,67,91,100`) — no schema change needed. Root cause: `monitor.go:484`'s `dispatchViaBridge` already has the NIP-10 `root` in scope (computed at `monitor.go:432` via `rootEventID(evt)`) but discards it, passing `channelUUID` instead. **Decision:** change `monitor.go:484` to pass `root`. `channelUUID` remains independently used for `publishReply`'s `h` tag and `replyTarget` — unaffected. Update `BuzzTaskDispatcher.Dispatch`'s doc comment (`buzz_dispatch.go:44-48`, currently says "e.g. the Buzz channel UUID") to reflect the corrected semantics.
5. ~~Thread-continuation trigger flow~~ — **Resolved.** `handleChannelEvent` (`monitor.go:362-377`) calls `classifyTrigger`; a non-`triggerMention` result `return`s immediately (line 367-368), so `rootEventID` never runs for a reply-without-mention. `rootEventID` is a pure function of the event alone — cheap to call unconditionally. **Decision:** add a `triggerThreadReply` kind (`trigger.go:14-16`); when `classifyTrigger` returns `triggerNone`, compute `root := rootEventID(evt)`, detect a NIP-10 `e`/`"reply"` or `e`/`"root"` tag (new helper — `rootEventID` today only inspects `t[3]=="root"`), and ask a new `BuzzTaskDispatcher.KnownThread(botName, rootID string) bool` method (implemented by `BuzzTaskBridge`) whether this persona has previously dispatched in that thread. State lives in `BuzzTaskBridge` alongside its existing per-persona `seenEvts map[string]time.Time` (`buzz_task_bridge.go:30-44`) — add a sibling `dispatchedThreads map[string]time.Time`, populated inside `Dispatch` using the same `threadID`/`root` value RQ4's fix already threads through, under the existing `b.mu` lock.

## Industry Standards

- NIP-17 (private direct messages): kind 14 (unsigned DM "rumor") → sealed as kind 13 (`nip44`-encrypted, signed by sender) → gift-wrapped as kind 1059 (encrypted again with an ephemeral key, `p`-tagged to the recipient, timestamp-randomized).
- NIP-10 (thread/reply conventions): `e` tags marked `"root"` and `"reply"`; `p` tags addressing participants.
- NIP-44 (encryption) and NIP-59 (gift wrap) — already vendored in `fiatjaf.com/nostr`, confirmed via module cache.

## Existing Implementations

- `rootEventID` (trigger.go:90-102) — existing NIP-10 root resolution, reused for FR-207/FR-208.
- `publishReply` (monitor.go:531-546) — existing outbound channel reply path, to be extended (FR-207) and used as the template (not directly reused — different tag shape) for the new DM reply path (FR-209).
- `guard.go`'s p-gate (lines 14-18, 43-57) — already anticipates a kind-1059 subscription ("P1's DM work" comment) — confirms this feature was designed for, not just bolted onto, the existing architecture.
- `BuzzTaskBridge` (`internal/application/orchestrator/buzz_task_bridge.go`) — existing dispatch bridge, to be extended for thread-keyed dispatch (FR-208) and reused as-is for DM dispatch (FR-203).

## API Documentation

- `fiatjaf.com/nostr`'s `nip44`/`nip59` package APIs — to be read directly from the vendored module source during implementation (RQ2).

## Best Practices

[TBD — NIP-17's own spec text on preserving privacy properties (ephemeral keys, timestamp randomization) is the primary reference; no additional external standard expected.]

## Open Questions

- RQ3 (relay NIP-17 support) — cannot be resolved statically; first DM subscription in implementation will confirm or fail fast. Not a blocker to starting code (DM subscribe/decrypt logic can be built and unit-tested against synthetic events regardless of relay support, per the existing pattern of mocking the relay client).
- FR-204's unauthorized-DM-handling decision (silent ignore vs. decline reply) — operator decision, not yet made.
- Whether DM conversations that go dormant should eventually be treated as a new conversation, or always continue the original task indefinitely — not yet decided (PRD Open Questions). Given RQ1's resolution (continuation via `ChatStore` history replay, capped at last 10 messages same as chat), this may not need a special case — an old conversation "naturally" fades in relevance as history scrolls past the 10-message window, matching existing chat behavior. Recommend accepting this as sufficient rather than building a separate dormancy/timeout mechanism, unless implementation reveals a concrete problem.

## References

- Source PRD: [buzz-dm-and-thread-support-PRD.md](./buzz-dm-and-thread-support-PRD.md)
- Prior feature precedent: `specs/archive/260814-boabot-native-daemon-mode/research.md` (similar research-question resolution pattern)
