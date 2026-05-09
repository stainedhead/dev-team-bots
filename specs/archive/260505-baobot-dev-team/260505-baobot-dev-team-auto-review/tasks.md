# Tasks: BaoBot Dev Team — Auto-Review Fixes

**Feature:** baobot-dev-team-auto-review
**Date:** 2026-05-05
**Status:** Planning

---

## Progress Summary

0/9 tasks complete

---

## Milestone 1 — FR-003: SRI Hash (independent)

| ID | Task | Deps | Est | Status |
|---|---|---|---|---|
| M1.1 | Verify SRI hash for htmx.org@1.9.12 | — | 5m | ⬜ |
| M1.2 | Write failing test: kanbanHTML contains integrity= attribute | M1.1 | 10m | ⬜ |
| M1.3 | Add htmxSRIHash constant + update kanbanHTML script tag | M1.2 | 10m | ⬜ |

**Acceptance:** `TestKanbanUI_ServesHTML` passes; new SRI test passes; golangci-lint clean.

---

## Milestone 2 — FR-004: UserRepo (independent)

| ID | Task | Deps | Est | Status |
|---|---|---|---|---|
| M2.1 | Read db.Migrate to confirm users table schema | — | 5m | ⬜ |
| M2.2 | Write failing tests for UserRepo (Create, Get, Update, Delete, List) | M2.1 | 20m | ⬜ |
| M2.3 | Implement UserRepo | M2.2 | 20m | ⬜ |
| M2.4 | Wire UserRepo in cmd/boabot/main.go | M2.3 | 10m | ⬜ |

**Acceptance:** UserRepo tests pass; coverage ≥ 90%; `go build ./cmd/boabot` compiles.

---

## Milestone 3 — FR-005: BudgetTrackerAdapter (independent)

| ID | Task | Deps | Est | Status |
|---|---|---|---|---|
| M3.1 | Read dynamodb/budget_tracker.go to confirm method signatures | — | 5m | ⬜ |
| M3.2 | Write failing tests for DynamoBudgetTrackerAdapter | M3.1 | 20m | ⬜ |
| M3.3 | Implement DynamoBudgetTrackerAdapter | M3.2 | 20m | ⬜ |
| M3.4 | Wire adapter in cmd/boabot/main.go | M3.3 | 10m | ⬜ |

**Acceptance:** Adapter tests pass; `WithBudgetTracker` wired; `go build ./cmd/boabot` compiles.

---

## Milestone 4 — FR-006: Coverage Gaps (after M2, M3)

| ID | Task | Deps | Est | Status |
|---|---|---|---|---|
| M4.1 | Identify uncovered lines in aws/dynamodb and otel | M2.3, M3.3 | 10m | ⬜ |
| M4.2 | Add targeted tests to reach ≥ 90% in both packages | M4.1 | 20m | ⬜ |

**Acceptance:** Both packages ≥ 90%; no regressions.
