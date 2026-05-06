# Spec: BaoBot Dev Team — Auto-Review Fixes

**Spec directory:** `specs/260505-baobot-dev-team-auto-review/`
**Source PRD:** `specs/260505-baobot-dev-team-auto-review/baobot-dev-team-auto-review-PRD.md`
**Date:** 2026-05-05
**Status:** Active

---

## Executive Summary

Code review of the `feat/baobot-dev-team` branch found two critical security defects (P0), three production-robustness gaps (P1), and one coverage gap (P2). The P0 items have been remediated in-branch. This spec drives the P1 and P2 fix phase: adding SRI to the HTMX script tag, implementing the missing `domain.UserStore` PostgreSQL adapter, bridging the `domain.BudgetTracker` interface to the DynamoDB implementation, and closing coverage gaps in `aws/dynamodb` and `otel`.

---

## Problem Statement

The BaoBot implementation is architecturally sound but has three gaps that prevent production deployment:

1. The Kanban web UI loads HTMX from a CDN without SRI, leaving it vulnerable to CDN-based script injection.
2. `domain.UserStore` has no concrete implementation — the HTTP server cannot be wired in production and `cmd/boabot/main.go` would fail to compile.
3. `domain.BudgetTracker` cannot be satisfied by the existing DynamoDB `BudgetTracker` without an adapter — budget enforcement is effectively disabled in production.

Additionally, two packages fall below the 90% coverage threshold required by `AGENTS.md`.

---

## Goals

- FR-003: Add SRI integrity hash to the HTMX `<script>` tag in the Kanban web UI.
- FR-004: Implement `UserRepo` in `internal/infrastructure/db/` satisfying `domain.UserStore`; wire into `cmd/boabot/main.go`.
- FR-005: Implement `DynamoBudgetTrackerAdapter` wrapping the DynamoDB `BudgetTracker` to satisfy `domain.BudgetTracker`; wire into `cmd/boabot/main.go`.
- FR-006: Raise test coverage in `aws/dynamodb` and `otel` to ≥ 90%.

## Non-Goals

- No changes to existing domain interfaces beyond what is required for the adapter.
- No architectural refactoring beyond named findings.
- No new bot types, workflow steps, or orchestrator features.
- No changes to CDK stacks, `boabotctl/`, or `boabot-team/`.
- No schema migrations — `UserRepo` uses the existing `users` table schema from `db.Migrate`.
- No coverage remediation for packages not named in FR-006.

---

## Functional Requirements

### FR-003 — HTMX loaded with Subresource Integrity (P1)

**Acceptance criteria:**
- The `<script>` tag for HTMX in `kanbanHTML` includes a valid `integrity="sha384-..."` attribute matching htmx.org@1.9.12.
- The `<script>` tag includes `crossorigin="anonymous"`.
- The SRI hash constant is extracted to a named constant at the top of `server.go` for maintainability.
- A test asserts the Kanban HTML response body contains an `integrity=` substring.
- All existing HTTP server tests continue to pass.

### FR-004 — PostgreSQL UserRepo implementing domain.UserStore (P1)

**Acceptance criteria:**
- `UserRepo` struct in `internal/infrastructure/db/` implements all five `domain.UserStore` methods: `Create`, `Get`, `Update`, `Delete`, `List`.
- Uses the `users` table columns: `username`, `password_hash`, `role`, `disabled`, `must_change_password`, `created_at`, plus optional `display_name`.
- `Delete` performs a hard delete (`DELETE FROM users WHERE username = $1`).
- Tests use `go-sqlmock` following `db_test.go` conventions; coverage ≥ 90%.
- `cmd/boabot/main.go` wires `UserRepo` into `httpserver.Config.Users`.
- All existing tests pass after wiring.

### FR-005 — DynamoBudgetTrackerAdapter satisfying domain.BudgetTracker (P1)

**Acceptance criteria:**
- `DynamoBudgetTrackerAdapter` in `internal/infrastructure/aws/dynamodb/` wraps the existing `BudgetTracker` struct.
- `botID` is injected at construction time from config.
- `CheckAndRecordToolCall(ctx)` delegates to `BudgetTracker.CheckBudget(ctx, botID)` then `RecordSpend(ctx, botID, 0, 1)` (or the appropriate call signature).
- `CheckAndRecordTokens(ctx, n)` delegates to `BudgetTracker.RecordSpend(ctx, botID, n, 0)`.
- `Flush(ctx)` is a no-op (DynamoDB tracker writes on each call).
- Adapter tests use the existing DynamoDB mock; coverage ≥ 90% for the adapter.
- `cmd/boabot/main.go` wires the adapter into `ExecuteTaskUseCase.WithBudgetTracker`.

### FR-006 — Coverage ≥ 90% in aws/dynamodb and otel (P2)

**Acceptance criteria:**
- `internal/infrastructure/aws/dynamodb` coverage ≥ 90% (currently 86.5%).
- `internal/infrastructure/otel` coverage ≥ 90% (currently 85.7%).
- No existing package coverage is reduced.
- New tests follow TDD and use existing mock patterns.

---

## Non-Functional Requirements

- All fixes follow TDD (failing test before implementation).
- Clean Architecture: no AWS SDK imports in domain or application packages.
- `go fmt`, `go vet`, `golangci-lint` pass after every fix.
- Full test suite passes with race detector (`go test -race`).

---

## System Architecture

**Affected layers:**
- Infrastructure (HTTP): `internal/infrastructure/http/server.go` — SRI hash addition (FR-003)
- Infrastructure (DB): `internal/infrastructure/db/` — new `UserRepo` (FR-004)
- Infrastructure (AWS/DynamoDB): `internal/infrastructure/aws/dynamodb/` — new adapter (FR-005)
- Wiring: `cmd/boabot/main.go` — wire `UserRepo` and adapter (FR-004, FR-005)

**New components:**
- `internal/infrastructure/db/user_repo.go` + `user_repo_test.go`
- `internal/infrastructure/aws/dynamodb/budget_tracker_adapter.go` + `budget_tracker_adapter_test.go`

**Modified components:**
- `internal/infrastructure/http/server.go` — add SRI hash constant + update `<script>` tag
- `internal/infrastructure/http/server_test.go` — add SRI assertion test
- `cmd/boabot/main.go` — wire new components

---

## Scope of Changes

| File | Action |
|---|---|
| `internal/infrastructure/http/server.go` | Add `htmxSRIHash` constant; update kanbanHTML script tag |
| `internal/infrastructure/http/server_test.go` | Add SRI hash presence test |
| `internal/infrastructure/db/user_repo.go` | Create new file |
| `internal/infrastructure/db/user_repo_test.go` | Create new file |
| `internal/infrastructure/aws/dynamodb/budget_tracker_adapter.go` | Create new file |
| `internal/infrastructure/aws/dynamodb/budget_tracker_adapter_test.go` | Create new file |
| `cmd/boabot/main.go` | Wire UserRepo and BudgetTrackerAdapter |
| `internal/infrastructure/aws/dynamodb/*_test.go` | Add tests to reach 90% |
| `internal/infrastructure/otel/*_test.go` | Add tests to reach 90% |

## Breaking Changes

None — all changes are additive. `cmd/boabot/main.go` wiring adds new dependencies but does not change existing behaviour.

---

## Success Criteria

- `go test -race -coverprofile=coverage.out ./...` shows ≥ 90% for all packages with testable statements.
- `golangci-lint run` reports 0 issues.
- `go build ./cmd/boabot` compiles without error with the new wiring in place.
- The Kanban web UI HTML contains a valid `integrity=sha384-...` attribute on the HTMX script tag.

---

## Risks and Mitigation

| Risk | Likelihood | Mitigation |
|---|---|---|
| DynamoDB BudgetTracker interface mismatch is deeper than expected | Medium | Read both interfaces carefully before writing adapter; use OQ-1 resolution |
| `users` table schema differs from what `domain.User` expects | Low | Read `db.Migrate` SQL before writing `UserRepo` |
| SRI hash for htmx@1.9.12 is incorrect | Low | Verify against official htmx.org release or srihash.org |

---

## Timeline and Milestones

| Milestone | Scope | Target |
|---|---|---|
| M1 | FR-003 (SRI hash) — small, independent | Same session |
| M2 | FR-004 (UserRepo) — medium | Same session |
| M3 | FR-005 (BudgetTrackerAdapter) — medium | Same session |
| M4 | FR-006 (coverage) — targeted tests | Same session |

FR-003, FR-004, FR-005 can run in parallel using agent teammates and git worktrees.

---

## References

- Source PRD: `specs/260505-baobot-dev-team-auto-review/baobot-dev-team-auto-review-PRD.md`
- Original spec (archived): `specs/archive/260505-baobot-dev-team/`
- Domain interfaces: `boabot/internal/domain/`
- DynamoDB BudgetTracker: `boabot/internal/infrastructure/aws/dynamodb/budget_tracker.go`
