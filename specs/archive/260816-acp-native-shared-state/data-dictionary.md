# Data Dictionary: ACP/Native Shared State and Task-Layer Parity

**Created:** 2026-08-16

## Purpose

Defines the data structures this feature introduces or modifies. Populated concretely during research/architecture; seeded here with the entities the PRD implies.

## Entities / Value Objects (existing, reused)

- `DirectTask` — existing board/task record type, tagged with `bot_name`. Reused unmodified for FR-504a (every ACP-dispatched task creates one).
- `Schedule` / `RecurrenceRule` — existing scheduling value objects, reused unmodified for FR-504.
- Board item (`board.json` entry) — existing type persisted by `InMemoryBoardStore`; reused unmodified.
- Chat message / `ChatStore` entry — existing type used by native mode's `handleChatSend`/`BuzzTaskBridge`; reused for FR-503's ACP-mode history replay.

## New Configuration Fields (FR-501, exact shape TBD at research)

- Candidate: `shared_state.root` (string, optional) — set identically on native mode's top-level config and any ACP persona config intended to share state. Absence preserves today's per-persona fallback behavior (non-breaking).

## Interfaces (existing, to be wired into ACP mode)

- `ChatStore` interface (domain layer) — read/append conversation history. New: constructed and wired in `acp.go`, consumed by `turn.go`.
- `ChatTaskManager` / `ParseScheduleNL` / `DispatchWithSchedule` — existing scheduling-detection and dispatch functions (application layer). New: invoked from ACP mode's `turn.go`.
- `Dispatcher` / `DirectTaskStore` — existing async task infrastructure (application/infrastructure layers). New: wired into ACP mode if FR-504 research concludes it's needed (see Open Questions).
- Heap watchdog (`watchdog.New` or equivalent) — existing infrastructure. New: wired into ACP mode's process lifecycle in `acp.go`.

## API Request/Response Types

No new wire-protocol types expected — ACP's existing `Prompt`/`PromptResponse` ACP-protocol types are unchanged (FR-504's correctness NFR requires the synchronous reply contract to be preserved for non-scheduling requests).
