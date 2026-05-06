# Architecture: BaoBot Dev Team — Auto-Review Fixes

**Feature:** baobot-dev-team-auto-review
**Date:** 2026-05-05
**Status:** Draft

---

## Architecture Overview

All fixes are additive infrastructure adapters. No domain interfaces change. The dependency direction remains Domain ← Application ← Infrastructure throughout.

---

## Component Architecture

```
cmd/boabot/main.go
  └── wires:
        db.UserRepo          → httpserver.Config.Users
        dynamodb.DynamoBudgetTrackerAdapter → ExecuteTaskUseCase.WithBudgetTracker

internal/infrastructure/db/
  └── user_repo.go           (new) implements domain.UserStore

internal/infrastructure/aws/dynamodb/
  └── budget_tracker_adapter.go  (new) wraps BudgetTracker, implements domain.BudgetTracker

internal/infrastructure/http/
  └── server.go              (modified) adds htmxSRIHash constant to kanbanHTML
```

---

## Layer Responsibilities

| Layer | Component | Responsibility |
|---|---|---|
| Domain | domain.UserStore | Interface contract — unchanged |
| Domain | domain.BudgetTracker | Interface contract — unchanged |
| Infrastructure (DB) | db.UserRepo | PostgreSQL CRUD for user records |
| Infrastructure (AWS) | dynamodb.DynamoBudgetTrackerAdapter | Bridges domain interface to DynamoDB calls |
| Infrastructure (HTTP) | server.go kanbanHTML | Serves SRI-protected HTMX |
| Wiring | cmd/boabot/main.go | Constructs and injects all components |

---

## Data Flow

**User management (FR-004):**
```
HTTP request → httpserver.handleUserCreate
  → httpserver.Config.Users.Create (domain.UserStore)
    → db.UserRepo.Create
      → PostgreSQL users table
```

**Budget enforcement (FR-005):**
```
ExecuteTaskUseCase.Execute
  → domain.BudgetTracker.CheckAndRecordToolCall
    → dynamodb.DynamoBudgetTrackerAdapter
      → dynamodb.BudgetTracker.CheckBudget(botID)
      → dynamodb.BudgetTracker.RecordSpend(botID, ...)
        → DynamoDB table
```

---

## Sequence Diagrams

[TBD — straightforward delegation chains; diagrams add no clarity beyond the data flow above]

---

## Integration Points

- PostgreSQL `users` table (existing schema from `db.Migrate`)
- DynamoDB budget table (existing, managed by `dynamodb.BudgetTracker`)
- unpkg CDN for HTMX (SRI pinned to htmx.org@1.9.12)

---

## Architectural Decisions

| Decision | Choice | Rationale |
|---|---|---|
| botID source in adapter | Constructor injection | Keeps domain.BudgetTracker interface botID-free; consistent with how other infra components are configured |
| Flush() implementation | No-op | DynamoDB tracker is already write-through; no buffering to flush |
| UserRepo.Delete | Hard delete | Soft deletion covered by existing DisableUser path; Delete means remove |
| SRI hash placement | Named constant at top of server.go | Single point of update; testable independently |
