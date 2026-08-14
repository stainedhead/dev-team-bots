# Plan: Boabot Native Daemon Mode (Multi-Agent Buzz Support)

**Created:** 2026-08-14
**Status:** Planning

## Development Approach

TDD (Red → Green → Refactor) throughout, per AGENTS.md. Clean Architecture boundaries enforced — Buzz dispatch bridge logic lives in `internal/application`, not directly in `main.go` wiring beyond construction/registration.

## Phase Breakdown

1. Research (this directory's `research.md`) — resolve the 5 open research questions.
2. Data modeling (`data-dictionary.md`) — finalize `DirectTaskSource` decision and any new types.
3. Architecture (`architecture.md`) — finalize component/data-flow design.
4. Task breakdown (`tasks.md`) — concrete TDD tasks.
5. Implementation — per-bot monitor wiring, Buzz→DirectTask/Board bridge, schedule bridge, multi-persona no-cross-talk verification.

## Critical Path

Per-bot monitor factory (FR-001/002/003) → Buzz→DirectTask/Board bridge (FR-005/006) → schedule bridge (FR-007/008) → multi-persona no-cross-talk verification (FR-004) run in parallel where possible once the bridge exists.

## Testing Strategy

- Unit tests (domain/application) for the Buzz→Dispatcher bridge, mocking `ChannelMonitor`, `Dispatcher`, `DirectTaskStore`, `BoardStore`.
- `-race` tests for concurrent multi-monitor dispatch to catch JSON-store write races (Risk table, spec.md).
- Manual verification against a live Buzz relay with 2+ personas for the no-cross-talk acceptance criterion (not practical to fully automate).

## Rollout Strategy

Single PR via dev-flow; ACP mode remains available and untouched as a fallback. One-time manual cutover step (stopping the ACP-managed "Boa" Buzz Desktop registration) documented in user-docs, performed by the operator after native mode is verified.

## Success Metrics

See spec.md Acceptance Criteria — all checklist items must pass before this is considered complete.
