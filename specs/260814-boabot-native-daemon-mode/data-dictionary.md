# Data Dictionary: Boabot Native Daemon Mode (Multi-Agent Buzz Support)

**Created:** 2026-08-14

Defines the data structures, types, and schemas this feature introduces or modifies. Populated during Phase 2 (Research & Data Modeling); kept current as implementation proceeds.

## Entities

- `DirectTask` (existing, `internal/domain`) — no structural change expected; Buzz path will populate it via the existing constructor/dispatch path. Confirm during research whether `bot_name`/persona tagging is already first-class or needs a field.
- `BoardItem` / Kanban board entity (existing) — Buzz-dispatched tasks must create a corresponding item; confirm exact type name during research.

## Value Objects

- `DirectTaskSource` (existing enum, `internal/domain`) — candidate for a new `DirectTaskSourceBuzz` value (see Research Question 1). [TBD pending research]

## Interfaces

- `buzzinfra.Monitor` / `ChannelMonitor` (existing) — factory currently constructs one process-wide instance; extended to be called once per Buzz-enabled persona.
- `Dispatcher` (existing, `internal/application`) — Buzz path will call into this the same way the web-UI chat path does.
- `DirectTaskStore` (existing) — no interface change expected; concurrency safety under multi-monitor writes to be verified (Research Question 3).
- `SecretStore` (existing) — resolves `buzz_private_key` per bot; already supports per-bot secrets via `boabotctl secret set --bot <name>`.

## Enumerations

- `DirectTaskSource` — see Value Objects above.

## API Request/Response Types

None — this feature has no new external-facing API surface; it wires an existing internal event source (Buzz `ChannelMonitor`) into existing internal stores (`DirectTaskStore`, board store).
