# Data Dictionary: Buzz DM Support and Full Threaded-Reply Support

**Created:** 2026-08-14

## Entities

- `DirectTask` (existing, `internal/domain`) — `ThreadID` field's semantics change from channel-UUID-keyed to NIP-10-thread-root-keyed (or DM-conversation-keyed). No structural change, but a meaningful behavioral one — must be documented clearly in this file once implemented (research question 4).

## Value Objects

- Possible new DM-vs-channel-vs-thread-continuation origin label for UI display (FR-203's "visibly labeled as DM-originated" requirement) — exact shape TBD at architecture phase; may be a new field, a new `DirectTaskSource` variant, or metadata on the existing task title/description.

## Interfaces

- `internal/infrastructure/buzz`'s `Monitor` — gains a DM subscription path (new, additive) alongside its existing channel subscription.
- New (unnamed pending research) DM reply-publish function, parallel to `publishReply` but producing gift-wrapped kind:1059 output instead of a plain channel-tagged event.

## Enumerations

- `DirectTaskSource` (existing, `internal/domain/direct_task.go`) — `DirectTaskSourceBuzz` already exists from the prior feature; whether DM origin needs its own enum value or a separate metadata field is TBD at architecture phase.

## API Request/Response Types

None — no new external-facing API surface; this wires two new Nostr event kinds (1059 inbound, gift-wrapped outbound) into the existing internal dispatch pipeline.
