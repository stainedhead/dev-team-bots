# Spec: Buzz DM Support and Full Threaded-Reply Support

**Created:** 2026-08-14
**Status:** Draft
**Source PRD:** [buzz-dm-and-thread-support-PRD.md](./buzz-dm-and-thread-support-PRD.md)

## Executive Summary

Extends `boabot`'s Buzz integration in two directions: (1) DM reachability via NIP-17 gift-wrap direct messages, using the same per-persona identity already used for channel participation, dispatched through the same `BuzzTaskBridge`/`Dispatcher` pipeline and visible on the shared orchestrator board (per explicit product decision); and (2) completes threaded-reply support that today only partially works — in-thread replies without re-`@mention` are currently dropped, outbound NIP-10 tagging is incomplete, and concurrent threads in one channel currently share one scheduling-confirmation slot due to a channel-keyed (not thread-keyed) `ThreadID`.

## Problem Statement

`boabot`'s native-mode Buzz integration only dispatches on an explicit channel/group `@mention` (kind:9, `#p`-tagged). This is now a firm requirement gap on two fronts, confirmed by direct code audit at current `HEAD`:

1. **No DM support.** `trigger.go:7` explicitly flags kind:1059/NIP-17 DMs as out of scope from the prior feature. No `nip17`/`nip44`/`nip59` usage exists anywhere in `boabot` today (confirmed via grep — zero hits), though the pinned `fiatjaf.com/nostr` dependency already vendors the `nip44`/`nip59` primitives needed.
2. **Threading is only partially implemented.** Inbound: `rootEventID` (trigger.go:90-102) can resolve a NIP-10 thread's root, but only runs *after* `classifyTrigger` (trigger.go:27-35) has already required an explicit `@mention` — a reply without re-mention is silently dropped as `triggerNone` (monitor.go:366-368). Outbound: `publishReply` (monitor.go:531-546) tags only a root `e` tag — no `reply`-marked `e` tag for the immediate parent, no `p` tag back to the author. Separately, `DirectTask.ThreadID` is populated from `channelUUID` (monitor.go:484), not the NIP-10 thread root — two concurrent threads in one channel share one `ChatTaskManager` pending-intent/scheduling slot (buzz_task_bridge.go:68,85,104).

## Goals

- Any Buzz-enabled persona with a provisioned key is reachable via a direct 1:1 message (NIP-17 gift-wrap DM), using its existing channel identity — no separate DM key.
- A human replying in-thread to a bot's prior message continues that conversation without re-`@mention`ing on every turn.
- Outbound Buzz replies (channel and DM) carry complete, correct NIP-10 threading metadata.
- Concurrent threads within the same channel (and concurrent DM conversations) are tracked independently — no shared/colliding scheduling-confirmation state.
- DM-triggered and thread-continued work is fully visible in the orchestrator UI (Kanban board + Tasks list), same observability standard as channel-triggered work, per explicit product decision (not treated as private/hidden).

## Non-Goals

- Not building group/multi-party encrypted DM support — 1:1 DMs only.
- Not adding new orchestrator UI screens or widgets — reuses the existing Kanban board and Tasks list.
- Not changing ACP mode (`boabot -acp`) — must keep building and passing its existing tests, untouched.
- Not building DM support for personas without `buzz.enabled: true`/without a provisioned key.
- Not re-litigating channel `@mention` dispatch, which already works correctly, except where thread-continuation logic needs to interact with it.

## User Requirements / Functional Requirements

**FR-201:** Each Buzz-enabled persona subscribes to NIP-17 gift-wrapped DM events (kind:1059, `#p`-tagged to its own pubkey) using the same relay connection and identity it already uses for channel participation.

**FR-202:** Inbound DM events are unwrapped (kind:1059 → decrypt → kind:13 sealed rumor → decrypt → kind:14 DM) using the vendored `nip44`/`nip59` primitives already present in the pinned `fiatjaf.com/nostr` dependency.

**FR-203:** A successfully decrypted DM dispatches through the same `BuzzTaskBridge`/`Dispatcher` pipeline channel messages already use — creating a real `DirectTask` and Kanban board item, tagged with the persona's `bot_name` and visibly labeled as DM-originated.

**FR-204:** DMs from senders not on the existing author-authorization mechanism do not trigger dispatch. Unauthorized-DM reply behavior (silent ignore vs. decline reply) — operator decision, see Open Questions.

**FR-205:** A human replying in-thread to a bot's own prior channel message is recognized as directed at the bot and dispatched, without a fresh `@mention`.

**FR-206:** An in-thread reply recognized under FR-205 continues the same underlying task/conversation, carrying forward context, rather than spawning an unrelated new task. Applies symmetrically to DM conversations.

**FR-207:** Outbound **channel** Buzz replies are tagged with root `e`, `reply`-marked `e` (immediate parent), and `p` (original author) — complete NIP-10 metadata. DM replies are a separate case with their own acceptance criterion (see Success Criteria below: "Outbound DM replies are correctly gift-wrapped and decrypt correctly for the sender") and carry no `e`/root NIP-10 tags at all — a 1:1 NIP-17 DM has no thread to reference, so NIP-10 threading metadata does not apply to it. (Tightened 2026-08-15 per the `buzz-dm-and-thread-support-auto-review` review's FR-305 finding: the original "(channel and DM)" phrasing here could be misread, in isolation, as requiring the three NIP-10 tags on DM replies too; the Success Criteria list always specified the correct, narrower scope.)

**FR-208:** `DirectTask.ThreadID` for Buzz-originated tasks is keyed by the actual NIP-10 thread root (or DM conversation), not channel UUID — independent scheduling-confirmation state per thread/conversation.

**FR-209:** DM reply publishing uses its own outbound path (encrypted kind:14 rumor → seal → gift-wrap) distinct from the channel-shaped `publishReply` (which hardcodes an `h` channel tag).

## Edge Cases

- **Self-message loop (real risk, not hypothetical):** `nip17.PrepareMessage` produces *two* gift-wrapped copies of every outbound DM — one to the recipient, one to self (`toUs`), so the sender can see their own sent message from another device. The inbound `nip17.ListenForMessages` subscription (`#p`-tagged to self) will receive this self-copy. The DM dispatch path must recognize and skip self-authored rumors (check the decrypted kind:14 rumor's `pubkey` against the persona's own pubkey) — failing to do so risks an infinite dispatch loop (bot replies to its own reply's self-copy, which produces another self-copy, ad infinitum).
- **Relay doesn't support kind:1059 (RQ3 unresolved statically):** subscription may silently receive nothing, or the relay may reject the subscription filter. Must not crash or block channel monitoring for the same persona — same failure-isolation standard as any other monitor issue.
- **Malformed/corrupted gift-wrap event:** decrypt/unwrap failure (bad ciphertext, wrong recipient, corrupted seal signature) must be logged and skipped, not crash the monitor goroutine.
- **Relay-replay of a DM event:** same class of risk as the prior feature's FR-101 fix (channel dedup) — a redelivered kind:1059 event must not double-dispatch. Reuse the existing event-ID dedup pattern (`checkAndMarkSeen`/`unmarkEvent`) for DMs, not a separate mechanism.
- **Thread reply to a root this persona never dispatched in:** e.g., a different persona's thread in a multi-persona channel, or a thread that predates this persona joining. `KnownThread` must be strictly per-persona (keyed by `botName` + `rootID`, not just `rootID`) so persona A's dispatched-thread state never causes persona B to incorrectly treat an unrelated reply as directed at it.
- **Concurrent replies in the same thread:** `dispatchedThreads` map access and `ChatStore` reads/writes for the same `threadID` from concurrent goroutines must be race-safe — covered by `-race` testing, consistent with the prior feature's concurrency-test standard.

## Non-Functional Requirements

- **Performance:** DM decrypt-and-dispatch latency comparable to the existing channel-mention path.
- **Reliability:** A DM subscription/decrypt failure for one persona is isolated — must not crash `TeamManager` or block any other persona's channel/DM monitor, extending the existing per-monitor failure-isolation pattern.
- **Security:**
  - DM decryption must never leak plaintext, private keys, or `nip44` conversation keys into logs.
  - NIP-17 privacy properties (ephemeral gift-wrap keys, randomized timestamps) must be preserved, not defeated by implementation choices (e.g., logging real receipt timestamps in a re-correlatable way).
  - DM dispatch gated by the same author-authorization mechanism as channel dispatch.
- **Observability:** DM- and thread-triggered `DirectTask`/board items tagged per-persona and per-origin (DM vs. channel vs. thread-continuation), traceable in logs and UI, visible on the shared board per explicit product decision.

## System Architecture

- **Affected layers:** `internal/infrastructure/buzz` (new DM subscription/unwrap/reply-publish path, thread-root-aware trigger classification), `internal/application/orchestrator` (`BuzzTaskBridge` extended for thread-keyed dispatch and conversation continuation), `internal/domain` (`ChannelMonitor` interface already generic enough — confirmed no change needed per audit).
- **New/modified components:**
  - DM subscription + unwrap/decrypt path in `internal/infrastructure/buzz` (new).
  - DM-specific outbound reply path (gift-wrap publish), separate from `publishReply` (new).
  - Inbound thread-continuation recognition: extend trigger classification so an in-thread reply without `@mention` still dispatches when the thread's root was previously bot-dispatched (modify `trigger.go`/`monitor.go`).
  - Outbound NIP-10 completion: add `reply` `e` tag + `p` tag to `publishReply` (modify).
  - `DirectTask.ThreadID` keying fix: use NIP-10 thread root (or DM conversation identifier) instead of `channelUUID` (modify `monitor.go`, `buzz_task_bridge.go`).
  - Conversation-continuation state: correlate a new inbound event to an existing task's conversation — mechanism TBD at research phase (may reuse existing multi-turn continuation if the worker/chat path already has one).
- **Out of scope architecturally:** ACP harness (`internal/infrastructure/acp`) — untouched.

## Scope of Changes

- Files to modify:
  - `boabot/internal/infrastructure/buzz/monitor.go` — DM subscription wiring, `ThreadID` one-argument fix (line ~484: pass `root` not `channelUUID`), `publishReply` NIP-10 completion (add `reply` `e` tag + `p` tag).
  - `boabot/internal/infrastructure/buzz/trigger.go` — new `triggerThreadReply` kind; new NIP-10 reply/root-tag detection helper.
  - `boabot/internal/application/orchestrator/buzz_task_bridge.go` — new `KnownThread` method + `dispatchedThreads` map; `ChatStore` history-replay instruction building (mirroring `handleChatSend`'s pattern).
  - `boabot/internal/domain/buzz_dispatch.go` (or wherever `BuzzTaskDispatcher` is defined) — add `KnownThread(botName, rootID string) bool` to the interface; correct `Dispatch`'s doc comment (currently says "e.g. the Buzz channel UUID").
- Files to create:
  - A `nostr.Keyer` adapter in `internal/infrastructure/buzz` (new file) implementing Signer+Cipher over boabot's existing per-persona key material.
  - A DM subscribe/decrypt/reply-publish path (new file(s) in `internal/infrastructure/buzz`), using `nip17.ListenForMessages`/`PrepareMessage`/`PublishMessage`.
- Dependencies: `fiatjaf.com/nostr`'s `nip17` package (built on already-vendored `nip44`/`nip59`), existing author-gating (`guard.go`), existing `ChatStore`/`Dispatcher`/`DirectTaskStore`/`BoardStore` infrastructure.

## Breaking Changes

None expected to public config schema. `DirectTask.ThreadID`'s semantic change (channel-keyed → thread-keyed) is an internal correctness fix, not a schema change, but must be verified not to break any existing behavior relying on the current (arguably buggy) channel-keyed value.

## Success Criteria and Acceptance Criteria

- [ ] A Buzz-enabled persona receives and responds to a direct 1:1 message sent to its own pubkey.
- [ ] A DM-triggered task appears in the Tasks UI and creates a Kanban board item, tagged with the correct `bot_name` and visibly labeled DM-originated.
- [ ] A DM from an unauthorized sender does not trigger task dispatch.
- [ ] Replying in-thread to a bot's prior channel message, without re-mentioning, is recognized and dispatched.
- [ ] An in-thread reply (channel or DM) continues the same task/conversation's context.
- [ ] Two concurrent threads in the same channel (or two concurrent DM conversations) maintain independent scheduling-confirmation state.
- [ ] Outbound channel replies carry root `e`, reply `e`, and `p` tags.
- [ ] Outbound DM replies are correctly gift-wrapped and decrypt correctly for the sender.
- [ ] `boabot -acp` still builds and passes its existing tests, untouched.
- [ ] No private key, conversation key, or decrypted DM plaintext appears in logs.
- [ ] A persona's own outbound DM does not trigger a self-dispatch loop (self-copy correctly filtered).

**Quality gates:** `go fmt`, `go vet`, `golangci-lint run`, `go test -race -gcflags=all=-d=checkptr=0 ./...` all clean; domain+application aggregate coverage ≥90% (currently 91.3%, must not regress).

## Risks and Mitigation

| Item | Type | Notes | Mitigation |
|------|------|-------|------------|
| `fiatjaf.com/nostr`'s `nip44`/`nip59` packages | Dependency | Already vendored — confirmed present. | Wiring/subscribe/decrypt work, not building crypto from scratch. |
| NIP-17 privacy properties | Risk | Naive implementation could leak correlatable metadata (real timestamps, key reuse). | Preserve library's ephemeral-key/timestamp-randomization behavior; do not log raw receipt times in a re-correlatable way. |
| Unauthorized/spam DMs | Risk | Reachable from any Nostr identity, unlike curated channel membership. | Reuse existing author-gate (`guard.go`) as a hard requirement, not optional. |
| Thread-continuation state design | Risk | FR-206 is a materially larger behavioral change than dispatch — needs to correlate new events to existing task/conversation state. | Research phase: check whether the worker/chat path already supports multi-turn continuation before designing new machinery. |
| `DirectTask.ThreadID` fix | Risk | Fixing already-shipped, channel-keyed behavior — must not regress the existing channel-mention path. | Regression test for the existing single-thread-per-channel case before/after the fix. |
| Buzz relay NIP-17 support | Dependency | Not independently verified whether `wss://feral-sysd.communities.buzz.xyz` supports gift-wrap kinds. | Verify during research phase before implementation. |
| Unauthorized-DM handling decision | Team dependency | Operator must decide silent-ignore vs. decline-reply (FR-204) before that FR is finalized. | Resolve during research/spec-review phase, not implementation. |

## Timeline and Milestones

[TBD] — tracked via `status.md`.

## References

- Source PRD: [buzz-dm-and-thread-support-PRD.md](./buzz-dm-and-thread-support-PRD.md)
- Prior feature (context/precedent): `specs/archive/260814-boabot-native-daemon-mode/`, `specs/archive/260814-boabot-native-daemon-mode-auto-review/`
