# Plan: Code Review Fixes — Task Scheduling and Notifications

**Created:** 2026-05-11
**Status:** Planning

---

## Development Approach

TDD (Red → Green → Refactor) for every fix. P0 first, then P1, then P2. Each fix is committed independently with a passing test suite before moving to the next.

---

## Phase Breakdown

### Phase 1 — P0 Fixes (blockers)

- FR-001: processTask re-fetch after RunNow
- FR-002: RunNow dispatching guard

### Phase 2 — P1 Fixes (required)

- FR-003: RecurrenceRule.Validate frequency check
- FR-004: HH:MM range validation + parseHHMM helper
- FR-005: Config.Notifications interface
- FR-006: ScheduledTaskDispatcher consolidated to domain
- FR-007: Remove dispatchAt goroutine
- FR-008: AppendDiscuss TOCTOU fix

### Phase 3 — P2 Fixes (quality)

- FR-009: persist() logging
- FR-010: pendingMap TTL
- FR-011: Author from JWT claims
- FR-012: processAllDue helper
- FR-013: Remove unused digits
- FR-014: MonthDay > 31 validation
- FR-015: Table-driven parse tests
- FR-016: Empty IDs → 400

---

## Critical Path

FR-006 (interface move) → must complete before FR-005 (Config interface) if `ScheduledTaskDispatcher` is needed in the http package. Otherwise independent.

FR-003 (Validate) → prerequisite for FR-015 (table tests) and FR-004.

---

## Testing Strategy

- One failing test per finding before any production change.
- All tests pass under `go test -race ./...` after each fix.
- `golangci-lint run` passes after all P2 fixes.

---

## Rollout Strategy

All fixes on the existing `feat/repeative-tasks-scheduling` branch. No additional branching needed.

---

## Success Metrics

- All 16 acceptance criteria pass.
- `go test -race ./...` passes.
- `golangci-lint run` → 0 issues.
- Coverage on target packages remains ≥90%.
