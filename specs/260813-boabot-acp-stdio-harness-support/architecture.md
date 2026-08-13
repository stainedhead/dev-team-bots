# Architecture: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13
**Status:** Draft

## Architecture Overview

`[TBD]` — to be filled in during Phase 3, informed by Phase 2 research findings (especially `buzz-acp`'s per-turn vs. per-session process model and Go ACP SDK availability).

## Component Architecture

`[TBD]`

## Layer Responsibilities

- **Domain:** `[TBD]` — likely a new narrow interface for a single-session ACP turn, kept separate from `ChannelMonitor`/`MessageQueue` (which model BaoBot's existing async multi-bot daemon model, not a fit for ACP's synchronous per-session pattern).
- **Application:** `[TBD]` — a use case invoking `Worker` for one ACP turn while reusing existing `BudgetTracker`/autonomy-gate logic (per FR-005; this is the direct mitigation for ADR-B020's original objection).
- **Infrastructure:** `[TBD]` — new `internal/infrastructure/acp/` package implementing the stdio JSON-RPC transport and ACP method handlers.

## Data Flow

`[TBD]`

## Sequence Diagrams

`[TBD]`

## Integration Points

- `cmd/boabot/main.go` — new mode routing (bypassing `TeamManager.Run()` for ACP mode).
- `internal/domain/worker.go` — reused `Worker`/`WorkerFactory` for turn execution.
- `internal/application` budget/autonomy logic — reused, not duplicated.
- External: `buzz-acp` binary (process host/spawner), model providers (Anthropic/Bedrock/OpenAI, per persona config).

## Architectural Decisions

- This design must produce a new or superseding ADR entry addressing ADR-B020's rejection directly (see spec.md Acceptance Criteria). Do not treat this feature as implicitly overturning ADR-B020 without that documentation.
