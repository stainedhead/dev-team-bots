# Plan: Tech-Lead Dynamic Subteam Auto-Review Fixes

**Feature:** tech-lead-dynamic-subteam-auto-review  
**Created:** 2026-05-07  
**Status:** Complete (all fixes implemented in commit 27cdc9a)

---

## Development Approach

TDD (Red → Green → Refactor) for each fix. P0 items first, then P1, then P2.

---

## Phase Breakdown

| Phase | Fixes | Priority |
|-------|-------|----------|
| 5a | FR-001 (pool auth) + FR-002 (spawn wiring) | P0 |
| 5b | FR-003 (hook lock) + FR-004 (re-spawn) | P1 |
| 5c | FR-005 (TearDownAll) + FR-006 (stale records) + FR-007 (interface checks) | P1/P2 |

---

## Testing Strategy

- Each fix has a new or updated test demonstrating the exact failure mode.
- All tests use `t.Parallel()`.
- `go test -race ./...` must pass.
- Coverage ≥90% on `application/subteam` and `application/pool`.

---

## Success Metrics

- 0 open findings from the review PRD.
- All new tests pass.
- Lint clean.
