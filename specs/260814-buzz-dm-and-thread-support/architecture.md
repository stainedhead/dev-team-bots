# Architecture: Buzz DM Support and Full Threaded-Reply Support

**Created:** 2026-08-14
**Status:** Draft

## Architecture Overview

[TBD — to be filled in Phase 3, after research confirms the exact `nip44`/`nip59` API shape and the conversation-continuation reuse decision (RQ1).]

## Component Architecture

- DM subscription + unwrap/decrypt path (`internal/infrastructure/buzz`, new).
- DM reply-publish path (`internal/infrastructure/buzz`, new, parallel to `publishReply`).
- Thread-continuation trigger classification (`trigger.go`/`monitor.go`, modified).
- Outbound NIP-10 completion (`monitor.go`'s `publishReply`, modified).
- `DirectTask.ThreadID`/pending-intent keying fix (`monitor.go`, `buzz_task_bridge.go`, modified).

## Layer Responsibilities

- **Domain:** `ChannelMonitor` interface unchanged (confirmed generic enough by audit — `Start`/`Stop`/`HandleResult`, no channel-specific coupling). Possible new value object for DM origin labeling.
- **Application:** `BuzzTaskBridge` extended for thread-keyed dispatch and (pending RQ1) conversation continuation.
- **Infrastructure:** `internal/infrastructure/buzz` gains DM subscribe/decrypt/reply-publish; existing channel path's trigger classification extended for thread-continuation recognition.

## Data Flow

[TBD — pending RQ1/RQ2/RQ4 resolution during research phase.]

## Sequence Diagrams

[TBD]

## Integration Points

- `fiatjaf.com/nostr`'s `nip44`/`nip59` packages (new usage).
- Existing author-gate (`guard.go`) — reused for DM dispatch authorization (FR-204).
- Existing `BuzzTaskBridge`/`Dispatcher`/`DirectTaskStore`/`BoardStore` — reused for DM dispatch (FR-203).

## Architectural Decisions

[TBD — record here once RQ1 (conversation-continuation reuse vs. new machinery) and RQ4 (ThreadID keying fix approach) are resolved during research.]
