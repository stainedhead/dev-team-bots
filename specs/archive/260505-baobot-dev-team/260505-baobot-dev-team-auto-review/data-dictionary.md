# Data Dictionary: BaoBot Dev Team — Auto-Review Fixes

**Feature:** baobot-dev-team-auto-review
**Date:** 2026-05-05

---

## Purpose

Documents data structures introduced or modified by the auto-review fix phase.

---

## Interfaces

### domain.UserStore (existing — being implemented)

```go
type UserStore interface {
    Create(ctx context.Context, u User) (User, error)
    Get(ctx context.Context, username string) (User, error)
    Update(ctx context.Context, u User) (User, error)
    Delete(ctx context.Context, username string) error
    List(ctx context.Context) ([]User, error)
}
```

### domain.BudgetTracker (existing — being adapted)

```go
type BudgetTracker interface {
    CheckAndRecordToolCall(ctx context.Context) error
    CheckAndRecordTokens(ctx context.Context, tokens int64) error
    Flush(ctx context.Context) error
}
```

---

## Entities

### domain.User (existing)

| Field | Type | DB Column | Notes |
|---|---|---|---|
| Username | string | username | Primary key |
| DisplayName | string | display_name | Optional |
| Role | UserRole | role | "admin" or "user" |
| Enabled | bool | disabled | Inverted: DB stores disabled=true |
| MustChangePassword | bool | must_change_password | |
| CreatedAt | time.Time | created_at | |

*Note: PasswordHash is managed by LocalAuthProvider, not exposed via UserStore.*

---

## New Components

### db.UserRepo

Implements `domain.UserStore` against the PostgreSQL `users` table.

| Method | SQL | Notes |
|---|---|---|
| Create | INSERT INTO users | Returns created user |
| Get | SELECT … WHERE username=$1 | Returns ErrNotFound if missing |
| Update | UPDATE users SET … WHERE username=$1 | Returns updated user |
| Delete | DELETE FROM users WHERE username=$1 | Hard delete |
| List | SELECT … FROM users ORDER BY username | Returns all users |

### dynamodb.DynamoBudgetTrackerAdapter

Wraps `dynamodb.BudgetTracker` to satisfy `domain.BudgetTracker`.

| domain method | DynamoDB delegation |
|---|---|
| CheckAndRecordToolCall | CheckBudget(ctx, botID) then RecordSpend (tool call units) |
| CheckAndRecordTokens(n) | RecordSpend(ctx, botID, n tokens) |
| Flush | no-op |

---

## Constants

### htmxSRIHash (new)

Named constant in `internal/infrastructure/http/server.go` holding the SHA-384 SRI hash for htmx.org@1.9.12. Value to be determined via RQ-3.
