# Architecture: ACP/Native Shared State and Task-Layer Parity

**Created:** 2026-08-16
**Status:** Draft

## Architecture Overview

[TBD at Phase 3 — populate after research resolves FR-504's integration shape.]

Expected shape: ACP mode's `acp.go` construction path gains three new optional wiring stages (ChatStore, scheduling/dispatch, heap watchdog), each gated on config presence and each degrading gracefully on construction failure — mirroring the existing `buildACPMCPOptions` pattern from the prior ACP-parity feature. `turn.go`'s `Prompt` handler gains a pre-check (scheduling-intent detection) and a post-check (DirectTask/board-item creation) around the existing `worker.Execute` call, with history replay feeding the instruction built for `worker.Execute`.

## Component Architecture

[TBD]

## Layer Responsibilities

- **Domain:** No new interfaces expected — `ChatStore`, `Dispatcher`, watchdog interfaces already exist. Shared-state config validation logic (FR-501) may need a small domain-layer value object if it's more than a string comparison.
- **Application:** Reuses existing `ChatTaskManager`, `DispatchWithSchedule`, `ParseScheduleNL` unmodified.
- **Infrastructure:** `acp.go` (wiring/construction), `turn.go` (per-turn flow), `session.go` (history-lookup key, if conversation ID scheme changes).

## Data Flow

[TBD — sequence: Prompt received → scheduling-intent pre-check → (if scheduling) DispatchWithSchedule + synchronous confirmation reply → (else) history replay + worker.Execute → DirectTask/board-item creation → ChatStore append → PromptResponse.]

## Sequence Diagrams

[TBD]

## Integration Points

- `board.json` shared state (existing, path-formula reconciled per FR-501)
- `chat.json` shared state (new, same shared-state root)
- Native-mode dashboard (read-only consumer of ACP-mode-created DirectTasks/board items — no dashboard code changes expected, since it already renders `bot_name`-tagged items)

## Architectural Decisions

[TBD — recorded here during Phase 3, then copied into `docs/architectural-decision-record.md` during Step 4 per repo convention.]
