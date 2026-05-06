# Plan: BaoBot Dev Team — Auto-Review Fixes

**Feature:** baobot-dev-team-auto-review
**Date:** 2026-05-05
**Status:** Planning

---

## Development Approach

TDD throughout. Each fix: write failing test → implement → verify green → lint. Brief design check before moving to next fix. FR-003, FR-004, FR-005 are independent and can run in parallel via agent teammates with git worktrees.

---

## Phase Breakdown

| Milestone | FRs | Parallel? | Estimated effort |
|---|---|---|---|
| M1 | FR-003 (SRI hash) | Independent | ~15 min |
| M2 | FR-004 (UserRepo) | Independent | ~45 min |
| M3 | FR-005 (BudgetTrackerAdapter) | Independent | ~45 min |
| M4 | FR-006 (coverage gaps) | After M2/M3 complete | ~30 min |

M1, M2, M3 run in parallel. M4 runs after all others are merged to main worktree.

---

## Critical Path

1. Read `dynamodb/budget_tracker.go` and `db.Migrate` before starting M2/M3.
2. M3 depends on understanding the exact DynamoDB BudgetTracker call signatures.
3. M4 (coverage) depends on M2 and M3 being complete so the new files count toward coverage.
4. `cmd/boabot/main.go` wiring is the final integration step — do after M2 and M3 are individually tested.

---

## Testing Strategy

- FR-003: `go-sqlmock` not needed; test asserts `kanbanHTML` string contains `integrity=sha384-`.
- FR-004: `go-sqlmock` for all `UserRepo` methods; pattern from `db_test.go`.
- FR-005: existing DynamoDB mock for `DynamoBudgetTrackerAdapter`; verify delegations.
- FR-006: targeted tests for uncovered lines in `aws/dynamodb` and `otel`.

---

## Rollout Strategy

All fixes are in the existing `feat/baobot-dev-team` branch. Commit and push after each milestone. Final push before Step 10 (archive fixes spec).

---

## Success Metrics

- `go test -race ./...` — all pass.
- `go tool cover -func=coverage.out` — all packages ≥ 90%.
- `golangci-lint run` — 0 issues.
- `go build ./cmd/boabot` — compiles cleanly with new wiring.
