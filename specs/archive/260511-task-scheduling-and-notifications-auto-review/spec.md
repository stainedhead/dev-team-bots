# Spec: Code Review Fixes — Task Scheduling and Notifications

**Created:** 2026-05-11
**Branch:** feat/repeative-tasks-scheduling
**Source PRD:** [task-scheduling-and-notifications-auto-review-PRD.md](task-scheduling-and-notifications-auto-review-PRD.md)

---

## Executive Summary

Targeted fixes for 16 findings identified in the code review of the task scheduling and notifications feature. Two P0 correctness bugs (recurring task status overwrite, dispatching-guard gap in RunNow), six P1 issues (input validation, architecture violations, goroutine leak, TOCTOU race), and eight P2 quality/observability improvements.

---

## Problem Statement

The initial implementation is structurally sound but has a correctness bug in `processTask` that overwrites the `running` status of recurring tasks back to `pending` immediately after dispatch, enabling double-dispatch if the scheduler ticks again before the bot completes. Additional issues include input validation gaps, Clean Architecture violations, a goroutine leak on shutdown, and a TOCTOU race in `AppendDiscuss`.

---

## Goals

- Eliminate the double-dispatch risk for recurring tasks (FR-001, P0)
- Guard `RunNow` against the `dispatching` status (FR-002, P0)
- Validate recurrence frequency and time range in the HTTP layer (FR-003, FR-004, P1)
- Replace concrete type in `Config.Notifications` with an interface (FR-005, P1)
- Deduplicate `ScheduledTaskDispatcher` interface to a single domain definition (FR-006, P1)
- Eliminate goroutine leak in legacy `dispatchAt` path (FR-007, P1)
- Fix TOCTOU race in `NotificationService.AppendDiscuss` (FR-008, P1)
- Add logging to silent `persist()` error paths (FR-009, P2)
- Add TTL expiry to `ChatTaskManager.pendingMap` (FR-010, P2)
- Use authenticated author identity in discuss entries (FR-011, P2)
- Extract shared body of `Tick`/`CatchUpMissedRuns` (FR-012, P2)
- Remove unused `digits` variable (FR-013, P2)
- Validate `MonthDay > 31` in `RecurrenceRule.Validate` (FR-014, P2)
- Add table-driven unit tests for `parseScheduleRequest` / `parseRecurrenceRequest` (FR-015, P2)
- Reject empty `ids` slice in `handleNotificationDelete` (FR-016, P2)

## Non-Goals

- Rewriting the scheduling architecture
- Changing the storage backend from file-backed JSON
- Adding new product features

---

## User Requirements

**FR-001 (P0):** After `processTask` for a recurring task, the store record retains `Status = running` (not `pending`) until the bot completes.

**FR-002 (P0):** `RunNow` returns early without dispatching when task status is `dispatching`.

**FR-003 (P1):** `RecurrenceRule.Validate` rejects unknown `Frequency` values; HTTP layer returns 400 for unknown frequencies.

**FR-004 (P1):** `parseRecurrenceRequest` validates HH:MM range; HTTP layer returns 400 for `h > 23` or `m > 59`.

**FR-005 (P1):** `Config.Notifications` in the HTTP server is an interface type, not a concrete `*appnotifications.NotificationService`.

**FR-006 (P1):** `ScheduledTaskDispatcher` is defined in exactly one location (domain layer); compile-time satisfaction assertion added.

**FR-007 (P1):** `Dispatch` no longer spawns a goroutine for future tasks; future tasks are stored as `ScheduleModeFuture` with `NextRunAt`, dispatched by the scheduler loop.

**FR-008 (P1):** `NotificationService.AppendDiscuss` uses the store's atomic `AppendDiscuss` method; concurrent appends do not lose entries.

**FR-009 (P2):** Every `persist()` error branch emits a `slog.Error` with path and operation.

**FR-010 (P2):** Pending intents in `ChatTaskManager` expire after a configurable TTL (default 10 minutes).

**FR-011 (P2):** Discuss entry `Author` is set from `claimsFromContext(r).Subject`, not hardcoded `"user"`.

**FR-012 (P2):** `Tick` and `CatchUpMissedRuns` share a private `processAllDue(ctx, now)` helper.

**FR-013 (P2):** Unused `digits` variable removed from `parseTimeOfDay`.

**FR-014 (P2):** `RecurrenceRule.Validate` rejects `MonthDay < 1` or `MonthDay > 31`.

**FR-015 (P2):** Table-driven unit tests for `parseScheduleRequest` and `parseRecurrenceRequest` covering 7+ cases.

**FR-016 (P2):** `handleNotificationDelete` returns 400 when `ids` is empty or nil.

---

## Non-Functional Requirements

- **Correctness:** All existing tests must continue to pass after each fix.
- **Coverage:** All new fix code must have corresponding failing test before production code (TDD).
- **Race safety:** All concurrent paths must pass `go test -race`.
- **Lint:** `golangci-lint run` produces 0 issues after all fixes.

---

## System Architecture

### Affected Layers

| Layer | Files |
|---|---|
| Domain | `internal/domain/schedule.go`, `internal/domain/direct_task.go` |
| Application | `internal/application/scheduling/scheduler_service.go`, `internal/application/notifications/notification_service.go`, `internal/application/orchestrator/chat_task_manager.go` |
| Infrastructure | `internal/infrastructure/local/orchestrator/task_dispatcher.go`, `internal/infrastructure/http/server.go`, `internal/infrastructure/http/notifications.go` |

---

## Scope of Changes

### Files to Modify

- `internal/domain/schedule.go` — FR-003, FR-014: `RecurrenceRule.Validate` frequency + MonthDay checks
- `internal/domain/direct_task.go` — FR-006: move `ScheduledTaskDispatcher` interface here
- `internal/application/scheduling/scheduler_service.go` — FR-001: re-fetch task after RunNow; FR-012: extract `processAllDue`
- `internal/application/notifications/notification_service.go` — FR-008: use atomic `store.AppendDiscuss`
- `internal/application/orchestrator/chat_task_manager.go` — FR-010: TTL expiry; FR-013: remove `digits`; FR-006: import domain interface
- `internal/infrastructure/local/orchestrator/task_dispatcher.go` — FR-002: dispatching guard; FR-007: remove `dispatchAt`
- `internal/infrastructure/http/server.go` — FR-005: interface for Notifications; FR-006: import domain interface
- `internal/infrastructure/http/notifications.go` — FR-004: HH:MM validation; FR-011: author from claims; FR-016: empty IDs check; FR-015: export parse helpers

### Files to Create

- `internal/infrastructure/http/schedule_parse.go` (optional, FR-015 refactor)

### Test Files to Modify/Create

- `internal/domain/schedule_test.go`
- `internal/application/scheduling/scheduler_service_test.go`
- `internal/application/notifications/notification_service_test.go`
- `internal/application/orchestrator/chat_task_manager_test.go`
- `internal/infrastructure/local/orchestrator/task_dispatcher_test.go`
- `internal/infrastructure/http/notifications_test.go` (create or extend)

---

## Breaking Changes

None — all changes are internal correctness fixes, not API surface changes.

---

## Acceptance Criteria

- [ ] FR-001: `store.Get(id).Status == running` after `processTask` for recurring task
- [ ] FR-002: `RunNow` returns early for `dispatching` status; test passes under `-race`
- [ ] FR-003: Invalid frequency → `Validate()` error; HTTP → 400
- [ ] FR-004: `"25:00"` and `"9:99"` → HTTP 400; `"23:59"` → 200
- [ ] FR-005: `Config.Notifications` is an interface; stub usable in tests
- [ ] FR-006: Single `ScheduledTaskDispatcher` definition in domain; compile-time assertion
- [ ] FR-007: No goroutine spawned for future tasks; scheduler loop handles dispatch
- [ ] FR-008: Concurrent `AppendDiscuss` test passes under `-race`; no entries lost
- [ ] FR-009: All `persist()` error paths log `slog.Error`
- [ ] FR-010: Expired pending intents are not confirmable; TTL defaults to 10 min
- [ ] FR-011: `Author` field uses JWT subject claim
- [ ] FR-012: `Tick` and `CatchUpMissedRuns` delegate to shared `processAllDue`
- [ ] FR-013: No unused `digits` variable; `go vet` passes
- [ ] FR-014: `MonthDay = 32` → `Validate()` error; `MonthDay = 31` → pass
- [ ] FR-015: Table-driven tests for parse helpers with 7+ cases
- [ ] FR-016: Empty `ids` → 400; non-empty → 200
- [ ] All existing tests pass; `golangci-lint` → 0 issues; `go test -race ./...` passes

---

## Risks and Mitigation

| Risk | Mitigation |
|---|---|
| FR-001 fix introduces new store.Get call in hot path | Store.Get is O(1) in-memory; acceptable overhead |
| FR-007 removing `dispatchAt` breaks existing callers of `Dispatch` | Audit all call sites before removal; delegate via `DispatchWithSchedule` if needed |
| FR-008 TOCTOU fix requires store API change | `store.AppendDiscuss` already exists; service just needs to use it |

---

## References

- Source PRD: [task-scheduling-and-notifications-auto-review-PRD.md](task-scheduling-and-notifications-auto-review-PRD.md)
- Original spec: `specs/archive/260511-task-scheduling-and-notifications/`
