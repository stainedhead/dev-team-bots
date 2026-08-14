# Architecture: Buzz DM Support and Full Threaded-Reply Support

**Created:** 2026-08-14
**Status:** Draft

## Architecture Overview

Two largely independent workstreams converging at `BuzzTaskBridge`:

1. **DM path (new):** a `nostr.Keyer` adapter over each persona's existing key material, feeding `nip17.ListenForMessages` (inbound subscribe+decrypt) and `nip17.PrepareMessage`/`PublishMessage` (outbound reply). Dispatches through the existing `BuzzTaskBridge`/`Dispatcher` pipeline, identical to channel dispatch from that point on.
2. **Threading completion (fix + extend existing code):** a one-argument fix so `DirectTask.ThreadID` carries the real NIP-10 thread root instead of the channel UUID; a new `triggerThreadReply` classification path so in-thread replies dispatch without re-mention; NIP-10 tag completion on `publishReply`'s output.

Both workstreams reuse `domain.ChatStore` (already used by the web-UI chat path) for conversation-history replay — no new session/continuation machinery, per RQ1's resolution.

## Component Architecture

- `nostr.Keyer` adapter (new, `internal/infrastructure/buzz`) — wraps boabot's per-persona key resolution to satisfy `nip17`'s Signer+Cipher contract.
- DM subscription (new) — `nip17.ListenForMessages` per Buzz-enabled persona, alongside the existing channel subscription in the same `Monitor`.
- DM reply-publish (new) — `nip17.PrepareMessage`/`PublishMessage`, distinct from `publishReply`.
- `triggerThreadReply` classification (new, `trigger.go`) — fires when `classifyTrigger` would otherwise return `triggerNone` but the event is a NIP-10 reply/root-tagged event within a thread `BuzzTaskBridge.KnownThread` confirms this persona previously dispatched in.
- `ThreadID` fix (one-line, `monitor.go:484`) — pass `root` (already computed) instead of `channelUUID`.
- `publishReply` NIP-10 completion (modify, `monitor.go`) — add `reply`-marked `e` tag and `p` tag.
- `BuzzTaskBridge.KnownThread` (new method, `buzz_task_bridge.go`) — backed by a new `dispatchedThreads map[string]time.Time`, sibling to the existing `seenEvts` map, populated inside `Dispatch`.
- `ChatStore`-backed history replay (reuse, not new) — Buzz's dispatch path builds each instruction the same way `handleChatSend` does: fetch recent messages by `threadID`, prepend a "Prior conversation" block.

## Layer Responsibilities

- **Domain:** `ChannelMonitor` interface unchanged (confirmed generic — `Start`/`Stop`/`HandleResult`). `BuzzTaskDispatcher` gains `KnownThread(botName, rootID string) bool`. No new `DirectTaskSource` value (DM-vs-channel distinguished via board-item title/metadata, not a domain enum change).
- **Application:** `BuzzTaskBridge` implements `KnownThread`, gains `dispatchedThreads` tracking, and builds instructions via `ChatStore` history replay (same pattern `handleChatSend` already uses — reuse the helper if one is factored out, or mirror its logic).
- **Infrastructure:** `internal/infrastructure/buzz` gains the `nostr.Keyer` adapter, DM subscribe/decrypt/reply-publish, `triggerThreadReply` classification, `ThreadID`/`publishReply` fixes.

## Data Flow

**DM:** `nip17.ListenForMessages` yields a decrypted kind:14 rumor → author-gate check (reuse existing `guard.go` pattern, FR-204) → `BuzzTaskBridge.Dispatch` (same bridge as channel) tagged `DirectTaskSourceBuzz`, `ThreadID` = DM conversation id → `ChatStore` append + history-replay instruction build → `Dispatcher`/`DirectTaskStore`/`BoardStore` write (existing) → reply via `nip17.PrepareMessage`/`PublishMessage`.

**Thread reply without mention:** inbound event fails `classifyTrigger` (no `p`-tag mention) → `rootEventID` computed unconditionally → NIP-10 reply/root tag detected → `BuzzTaskBridge.KnownThread(botName, root)` checked → if true, `triggerThreadReply` fires → dispatch proceeds identically to a mention-triggered dispatch, with `ThreadID` = `root` (RQ4 fix) → `ChatStore` history replay → reply published with complete NIP-10 tags (root + reply + p, RQ7 fix).

## Sequence Diagrams

[Deferred — the two data-flow descriptions above are sufficiently precise for implementation; a diagram adds little beyond the prose given both flows are short and linear.]

## Integration Points

- `fiatjaf.com/nostr`'s `nip17` package (new usage — built on `nip44`/`nip59`, not calling those directly).
- Existing author-gate (`guard.go`) — reused for DM dispatch authorization (FR-204).
- Existing `BuzzTaskBridge`/`Dispatcher`/`DirectTaskStore`/`BoardStore`/`ChatStore` — reused for both DM dispatch (FR-203) and thread-continuation history (FR-206).

## Architectural Decisions

- **Use `nip17`'s high-level API, not raw `nip44`/`nip59` calls.** The vendored library already implements the correct privacy-preserving behavior (ephemeral gift-wrap keys, randomized timestamps) — reimplementing at the `nip44`/`nip59` level would risk subtly defeating those properties for no benefit.
- **Reuse `ChatStore` history-replay for conversation continuation (RQ1), not new session state.** Matches the existing web-UI chat pattern exactly; avoids building parallel conversation-state machinery. A conversation "naturally" fades as history scrolls past the existing 10-message replay window — no separate dormancy/timeout mechanism planned unless implementation reveals a concrete problem.
- **`ThreadID` fix is a one-argument change, not a `pendingMap` restructure.** `ChatTaskManager.pendingMap` was already keyed by an opaque string; only the value passed in (`monitor.go:484`) was wrong.
- **DM origin distinguished via board-item title/metadata, not a new `DirectTaskSource` value.** Keeps the domain enum stable; matches the prior feature's `buzzBoardTitle`-style labeling approach.
- **`triggerThreadReply` state lives in `BuzzTaskBridge`, not `Monitor`.** `BuzzTaskBridge` already holds per-persona dispatch state (`seenEvts`) — a sibling `dispatchedThreads` map keeps all per-persona dispatch-tracking state in one place rather than splitting it across `Monitor` and the bridge.
