# Data Dictionary: Boabot Native Daemon Mode (Multi-Agent Buzz Support)

**Created:** 2026-08-14

Defines the data structures, types, and schemas this feature introduces or modifies. Populated during Phase 2 (Research & Data Modeling); kept current as implementation proceeds.

## Entities

- `DirectTask` (existing, `internal/domain`) — no structural change expected; Buzz path will populate it via the existing constructor/dispatch path. Confirm during research whether `bot_name`/persona tagging is already first-class or needs a field.
- `BoardItem` / Kanban board entity (existing) — Buzz-dispatched tasks must create a corresponding item; confirm exact type name during research.

## Value Objects

- `DirectTaskSource` (existing enum, `internal/domain/direct_task.go:9-17`) — three values today: `DirectTaskSourceChat = "chat"`, `DirectTaskSourceOperator = "operator"`, `DirectTaskSourceBoard = "board"`. **Adding `DirectTaskSourceBuzz = "buzz"`** — confirmed additive; no exhaustive switch to update, only the `execute_task.go:100` provider-selection check (see Architectural Decisions).

## Interfaces

- `buzzinfra.Monitor` / `ChannelMonitor` (existing, `internal/infrastructure/buzz/monitor.go`) — factory (`buildBuzzMonitor`, `main.go:196-266`) currently constructs one process-wide instance from `run()` (`main.go:174`); extended to be called once per Buzz-enabled persona in a loop. No signature change to `buildBuzzMonitor`; no singleton state found in the package (per-identity lock file at `buzz/lock.go`, keyed by pubkey — already multi-identity safe).
- `Dispatcher` (existing, `internal/application`) — Buzz path will call `DispatchWithSchedule` the same way `ChatTaskManager` does today (`chat_task_manager.go:60-90`).
- `DirectTaskStore` (existing, `internal/infrastructure/local/orchestrator/direct_task_store.go`) — no interface change; confirmed thread-safe via `sync.RWMutex` (`direct_task_store.go:22-26`), atomic persist via temp-file + `os.Rename`.
- `BoardStore` (existing, `internal/infrastructure/local/orchestrator/board.go:26-27`) — same `sync.RWMutex` pattern as `DirectTaskStore`; confirmed thread-safe.
- `SecretStore` (existing) — resolves `buzz_private_key` per bot via `buzzinfra.LoadKeypair(ctx, store, bc.BotName)` (`main.go:206`); already namespaced per bot name, supports per-bot secrets via `boabotctl secret set --bot <name>`.
- `TeamManager.WithChannelMonitor(...)` (`team_manager.go:208`) — appends to a slice; already called multiple times today (Slack + Buzz), confirmed safe to call once per Buzz-enabled persona.

## Enumerations

- `DirectTaskSource` — see Value Objects above.

## API Request/Response Types

None — this feature has no new external-facing API surface; it wires an existing internal event source (Buzz `ChannelMonitor`) into existing internal stores (`DirectTaskStore`, board store).
