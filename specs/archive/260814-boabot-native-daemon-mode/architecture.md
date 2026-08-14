# Architecture: Boabot Native Daemon Mode (Multi-Agent Buzz Support)

**Created:** 2026-08-14
**Status:** Draft

## Architecture Overview

Native mode's `run()` (`boabot/cmd/boabot/main.go`) currently constructs one process-wide Buzz `ChannelMonitor` from the daemon's own `cfg`. This work replaces that single call with a loop over `team.yaml`'s Buzz-enabled bot entries, constructing one `ChannelMonitor` per persona (own key, own relay connection, own dedup-registered queue name), all registered on the same shared `TeamManager`. Each monitor's dispatch path is rewired to go through `Dispatcher`/`DirectTaskStore`/`BoardStore` — the same infrastructure the web-UI chat path already uses — instead of calling `Worker.Execute` directly. No new stores, no new external adapters; this is a wiring + bridge extension of existing Clean Architecture layers.

## Component Architecture

- Per-bot Buzz monitor loop (replacing the single `buildBuzzMonitor` call at `main.go:174`) — iterates Buzz-enabled `team.yaml` entries, loads each bot's own `config.yaml`, calls `buildBuzzMonitor` + `mgr.WithChannelMonitor` per persona.
- `TeamManager`/`team_manager.go` team-entry loading exposed (currently unexported `loadTeamConfig`) so `main.go` doesn't duplicate YAML parsing — wiring stays in `cmd/`, config-loading logic stays where it already lives.
- Buzz → `Dispatcher`/`DirectTaskStore` bridge — new application-layer use case replacing the current direct `Worker.Execute` call (exact current call site: implementation task P1.0).
- Buzz → `BoardStore` bridge — same use case creates the corresponding Kanban item.
- Buzz → `DispatchWithSchedule` bridge — reuses `ChatTaskManager`'s existing `ParseScheduleNL` heuristic parser and confirm/cancel flow (no new NL mechanism).
- `execute_task.go:100` provider-selection check extended to treat `DirectTaskSourceBuzz` the same as `DirectTaskSourceChat` (both get `chatProvider`).

## Layer Responsibilities

- **Domain:** new `DirectTaskSourceBuzz` value in `internal/domain/direct_task.go` (additive, no exhaustive switch to update).
- **Application:** new use case orchestrating Buzz `ChannelMonitor` output → `Dispatcher` → `DirectTaskStore` + `BoardStore`; reuse of existing `ChatTaskManager`/`ParseScheduleNL` for the scheduling sub-path; `execute_task.go` provider-selection update.
- **Infrastructure:** `buzzinfra`/`main.go` per-bot monitor construction loop; no new external adapters — same relay client, same `SecretStore`.

## Data Flow

Buzz message received by persona's `ChannelMonitor` (kind 9, `#p`-tagged mention, per persona's own pubkey) → trigger classification (existing, unchanged) → new bridge use case decides immediate vs. scheduled (via `ParseScheduleNL`/`ChatTaskManager` reuse) → `Dispatcher.Dispatch` or `Dispatcher.DispatchWithSchedule` call, tagged `DirectTaskSourceBuzz` and the persona's `bot_name` → `DirectTaskStore` write (mutex-protected, already safe for concurrent multi-persona writes) + `BoardStore` write (same) → orchestrator UI reflects the update via its existing refresh/poll mechanism, no new endpoints needed.

## Sequence Diagrams

[TBD — add during Step 3 implementation once the exact current Buzz→`Worker.Execute` call site (P1.0) is traced; a diagram before that would misrepresent the current-state side.]

## Integration Points

- Buzz relay (`wss://feral-sysd.communities.buzz.xyz`) — one connection per Buzz-enabled persona; 3 personas already configured (`orchestrator`, `architect`, `tech-lead`).
- `SecretStore`/`boabotctl secret` — per-bot `buzz_private_key` resolution, already namespaced by `bot_name`.
- `router.Register` (`queue/queue.go:39`) — panics on duplicate bot-name registration; the multi-persona loop must track already-registered names per bot, mirroring the existing Slack/Buzz dedup pattern (`buildBuzzMonitor`'s `queueAlreadyRegistered` param).
- Existing orchestrator web UI/API — no new endpoints; existing Tasks/Board endpoints surface Buzz-originated records once written through the same stores.

## Architectural Decisions

- **`DirectTaskSourceBuzz` added as a new enum value** (not reusing `DirectTaskSourceChat`) — gives the UI and logs a precise, filterable origin label for Buzz-dispatched work, matching the NFR-Observability requirement, while `execute_task.go:100`'s provider-selection check is extended so Buzz-sourced tasks still get the same `chatProvider` treatment chat-sourced tasks get. Rejected reusing `DirectTaskSourceChat` directly because it would conflate two distinct origin channels in the UI/logs, working against the "traceable per-agent... not lumped together" NFR.
- **Team-entry loading exposed from `team_manager.go` rather than duplicated in `main.go`** — keeps config-parsing logic in one place (Clean Architecture: `cmd/` is wiring only, per AGENTS.md).
- **Reuse `ParseScheduleNL`/`ChatTaskManager` for Buzz's NL scheduling** rather than building a new mechanism — avoids duplicating an already-imperfect heuristic parser; if Buzz phrasing proves to defeat it in practice, that's an implementation-time finding to record in `implementation-notes.md`, not a reason to build two parsers up front.
- **No JSON-store locking changes** — `DirectTaskStore`/`BoardStore` are already `sync.RWMutex`-protected with atomic file persistence; the spec's original risk entry here is downgraded from "needs verification" to "confirmed safe, add `-race` tests to lock in the guarantee."
