# Data Dictionary: Buzz DM Support and Full Threaded-Reply Support

**Created:** 2026-08-14

## Entities

- `DirectTask` (existing, `internal/domain/direct_task.go:32`) — `ThreadID` semantics fixed: now set from the NIP-10 thread root (channel) or DM conversation id, not `channelUUID`. One-argument fix at `monitor.go:484` (pass `root` instead of `channelUUID`); no structural change to the type itself.
- `domain.ChatStore` (existing, `internal/domain/direct_task.go:130-144`) — reused as-is for Buzz thread/DM conversation history, keyed by the same `threadID` value now correctly set above. No structural change; Buzz's monitor/bridge appends to it the same way `handleChatSend` does.

## Value Objects

- DM-vs-channel-vs-thread-continuation origin label for UI display (FR-203) — use the existing board-item title/metadata convention (see `buzzBoardTitle` from the prior feature) rather than a new `DirectTaskSource` variant, since `DirectTaskSourceBuzz` already covers "this came from Buzz" and a title/metadata label is sufficient to distinguish DM vs. channel without a domain-layer enum change.

## Interfaces

- `internal/infrastructure/buzz`'s `Monitor` — gains a DM subscription path via `nip17.ListenForMessages` (new, additive) alongside its existing channel subscription.
- New DM reply-publish path via `nip17.PrepareMessage`/`PublishMessage` (new), parallel to but distinct from `publishReply` (channel-shaped, hardcodes an `h` tag — does not apply to DMs).
- New `nostr.Keyer` adapter (new, `internal/infrastructure/buzz`) implementing `nostr.Keyer`'s Signer+Cipher contract over boabot's existing per-persona key material — required by every `nip17` function.
- `domain.BuzzTaskDispatcher` (existing) — gains a new method `KnownThread(botName, rootID string) bool`, implemented by `BuzzTaskBridge`, used by the new `triggerThreadReply` classification path.

## Enumerations

- `DirectTaskSource` (existing, `internal/domain/direct_task.go`) — `DirectTaskSourceBuzz` already exists and is reused for both channel and DM origin (no new value — see Value Objects above for how DM-vs-channel is instead distinguished).
- `triggerKind` (existing, `internal/infrastructure/buzz/trigger.go:14-16`) — gains a new `triggerThreadReply` value, alongside existing `triggerNone`/`triggerMention`.

## API Request/Response Types

None — no new external-facing API surface; this wires two new Nostr event kinds (1059 inbound, gift-wrapped outbound) into the existing internal dispatch pipeline.
