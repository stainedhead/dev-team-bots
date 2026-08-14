# Tasks: Boabot Native Daemon Mode (Multi-Agent Buzz Support)

**Created:** 2026-08-14
**Status:** Planning

## Progress Summary

0/9 tasks complete.

## Phase 1 — Per-bot Buzz wiring

### P1.0 — Trace current Buzz → `Worker.Execute` dispatch path

- **Depends on:** none
- **Acceptance criteria:** Exact call site(s) where the single-identity Buzz `ChannelMonitor` today invokes `Worker.Execute` directly (bypassing `DirectTaskStore`/`BoardStore`) are documented in `implementation-notes.md` with file:line references, before any bridge code is written.

### P1.1 — Per-bot Buzz monitor factory loop

- **Depends on:** P1.0
- **Acceptance criteria:** `main.go`'s `run()` constructs one `buzzinfra.Monitor` per Buzz-enabled `team.yaml` persona (looping over entries, `config.Load`-ing each bot's own `config.yaml`, checking `buzz.enabled`), each registered via its own `mgr.WithChannelMonitor(...)` call. A team with exactly one Buzz-enabled bot behaves identically to today (regression test). Duplicate bot-name registration fails loudly per-persona, not process-wide (`router.Register` panic avoided via existing dedup pattern).
- **Test-first:** failing test asserting N monitors are constructed for N Buzz-enabled personas in a test `team.yaml` fixture, before the loop exists.

### P1.2 — Expose team-entry loading from `TeamManager`

- **Depends on:** none (can run parallel to P1.0/P1.1)
- **Acceptance criteria:** `team_manager.go`'s `loadTeamConfig` (or an equivalent) is exposed for `main.go`'s loop to reuse, rather than `main.go` reimplementing `team.yaml` parsing.

## Phase 2 — Buzz → DirectTask/Board bridge

### P2.1 — `DirectTaskSourceBuzz` domain value

- **Depends on:** none
- **Acceptance criteria:** `internal/domain/direct_task.go` gains `DirectTaskSourceBuzz`; `execute_task.go:100`'s provider-selection check treats it like `DirectTaskSourceChat`. Unit test confirms Buzz-sourced tasks route to `chatProvider`.

### P2.2 — Buzz dispatch → `Dispatcher`/`DirectTaskStore`/`BoardStore` bridge use case

- **Depends on:** P1.0, P2.1
- **Acceptance criteria:** New application-layer use case (package location TBD, likely alongside `chat_task_manager.go`) replaces the direct `Worker.Execute` call found in P1.0 with a `Dispatcher.Dispatch` call tagged `DirectTaskSourceBuzz` + persona `bot_name`, and creates the corresponding `BoardStore` item. Both update live and reflect completion (FR-005). Dedup by Nostr event ID guards against relay-replay double-dispatch (Edge Cases, spec.md).

### P2.3 — Multi-persona no-cross-talk verification

- **Depends on:** P1.1, P2.2
- **Acceptance criteria:** Mentioning persona A dispatches only to persona A's `ChannelMonitor`/bridge; concurrent dispatch from 2+ personas does not block, drop, or interleave each other's UI updates (FR-004, FR-006). `-race` test with concurrent simulated dispatch from 2+ monitors against `DirectTaskStore`/`BoardStore`.

## Phase 3 — Scheduling

### P3.1 — Reuse `ParseScheduleNL`/`ChatTaskManager` for Buzz scheduling requests

- **Depends on:** P2.2
- **Acceptance criteria:** A Buzz request phrased as a recurring instruction creates a real `Schedule`/`RecurrenceRule`-backed task via the existing `ChatTaskManager.DetectAndHandle`/`ParseScheduleNL` path, reused (not reimplemented) for Buzz-originated text (FR-007). Malformed/unparseable schedule text falls back to plain dispatch, matching existing chat-path behavior (Edge Cases).

### P3.2 — Update/cancel a previously-created Buzz task

- **Depends on:** P3.1
- **Acceptance criteria:** A Buzz request can update or cancel a previously-created task (immediate or scheduled), reflected in the Tasks UI (FR-008). Cancel racing with in-flight execution matches existing chat-path cancel behavior (Edge Cases).

## Phase 4 — Demo readiness

### P4.1 — Provision two personas' secrets and verify end-to-end

- **Depends on:** all above
- **Acceptance criteria:** Operator picks 2 of {`orchestrator`, `architect`, `tech-lead`} (research.md RQ5), provisions `buzz_private_key` for each via `boabotctl secret set --bot <name>`, and verifies all spec.md Acceptance Criteria against a live Buzz relay session.
