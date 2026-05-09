# Implementation Notes: BaoBot Dev Team — Auto-Review Fixes

**Feature:** baobot-dev-team-auto-review
**Date:** 2026-05-05

---

## Purpose

Running log of technical decisions, surprises, and deviations made during the fix implementation phase.

---

## Technical Decisions

### BudgetTrackerAdapter constructor injection

`dynamodb.BudgetTracker.CheckBudget` takes `perBotCap` and `systemBudget` as call-time parameters. To satisfy the `domain.BudgetTracker` interface (which has no such params), `BudgetTrackerAdapter` stores these at construction time. This is cleaner than passing them through the application layer.

### UserRepo — missing `display_name` column

`domain.User.DisplayName` is not persisted by `UserRepo` because the existing `users` table schema (from `db.Migrate`) has no `display_name` column. The field exists on the struct but is silently ignored in persistence. A future migration can add this column without breaking the adapter.

### UserRepo — locked-account sentinel

`UserRepo.Create` uses `'!'` as the `password_hash` value when `domain.User.PasswordHash` is empty. This makes the row NOT NULL-safe while preventing bcrypt authentication (bcrypt hashes always start with `$2`). Callers must call `Auth.SetPassword` separately to activate the account.

### SRI hash — raw string literal constraint

Go raw string literals (backtick) do not support `+` concatenation. The htmx SRI hash must be inlined directly into the HTML string. A standalone `const htmxSRIHash` would be unused and causes a lint error. The hash value is in a comment in the test file for discoverability.

---

## Edge Cases & Solutions

### errcheck on `defer rows.Close()`

`golangci-lint` (errcheck) flags `defer rows.Close()` as dropping the error return. The fix pattern is `defer func() { _ = rows.Close() }()`. Applied to 5 call sites in `db.go` and all affected test files.

### `io.Copy` vs `strings.Builder.ReadFrom`

`strings.Builder` does not implement `io.ReaderFrom`. Tests that need to read HTTP response bodies into a string must use `io.Copy(&buf, resp.Body)` not `buf.ReadFrom(resp.Body)`.

---

## Deviations from Plan

### FR-006: otel package remains at 85.7% coverage

The `internal/infrastructure/otel` package has 3 unreachable error-return branches in `New()`:
- `resource.New` failure (no detectors used, cannot fail with `WithAttributes` only)
- `otlptracehttp.New` failure (OTel v1.43.0 connects lazily — construction never errors)
- `otlpmetrichttp.New` failure (same reason)

Covering these would require either:
1. Injecting mock exporter constructors (API change to production code)
2. Running a real OTLP server that rejects connections at the protocol level

Since otel is infrastructure (not domain/use case), and FR-006 is P2 (medium priority), the 85.7% is accepted. The 90% requirement from CLAUDE.md targets Domain and Use Case layers specifically.

---

## Lessons Learned

- OTel SDK v1.x exporters connect lazily — testing error paths at construction time requires dependency injection or refactoring the production API.
- errcheck in golangci-lint v2 requires explicit `_ =` suppression for deferred Close calls.
