# Research: BaoBot Dev Team — Auto-Review Fixes

**Feature:** baobot-dev-team-auto-review
**Date:** 2026-05-05
**Source PRD:** specs/260505-baobot-dev-team-auto-review/baobot-dev-team-auto-review-PRD.md

---

## Research Questions

### RQ-1 — DynamoDB BudgetTracker exact method signatures (OQ-1, OQ-2)
What are the exact method signatures of `BudgetTracker.CheckBudget`, `RecordSpend`, and `DailySpend`? Which maps to `CheckAndRecordToolCall` and which to `CheckAndRecordTokens`? Is `DailySpend` needed at all for the adapter?

### RQ-2 — `domain.BudgetTracker.Flush` semantics (OQ-2)
Is `Flush` expected to be a synchronous flush to the store, or a signal that a session is ending? The DynamoDB tracker writes on every call — is `Flush` truly a no-op, or should it call some cleanup method?

### RQ-3 — htmx.org@1.9.12 official SRI hash (OQ-3)
What is the canonical SHA-384 SRI hash for `https://unpkg.com/htmx.org@1.9.12/dist/htmx.min.js`? Verify against srihash.org or the official htmx release notes.

### RQ-4 — `users` table schema in db.Migrate (OQ-4)
What columns does the `users` table have in `db.Migrate`? Does it include `display_name`? What are the column types? Does `domain.User` have fields that are not persisted?

### RQ-5 — `cmd/boabot/main.go` current wiring structure
What does the current `main.go` look like? Where are infrastructure components constructed? Where should `UserRepo` and `DynamoBudgetTrackerAdapter` be inserted?

---

## Industry Standards

[TBD]

## Existing Implementations

[TBD — read dynamodb/budget_tracker.go, db/db.go (Migrate function), cmd/boabot/main.go before implementing]

## API Documentation

[TBD]

## Best Practices

[TBD]

## Open Questions

See PRD OQ-1 through OQ-4 with recommended resolutions.

## References

- `boabot/internal/infrastructure/aws/dynamodb/budget_tracker.go`
- `boabot/internal/infrastructure/db/db.go` (Migrate function)
- `boabot/internal/domain/` (BudgetTracker, UserStore interfaces)
- `boabot/cmd/boabot/main.go`
- htmx.org release: https://github.com/bigskysoftware/htmx/releases/tag/v1.9.12
