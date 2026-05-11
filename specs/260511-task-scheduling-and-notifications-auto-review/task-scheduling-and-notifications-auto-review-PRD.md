# PRD: Code Review Fixes — Task Scheduling and Notifications

**Created:** 2026-05-11
**Status:** Draft
**Branch:** `feat/repeative-tasks-scheduling`

---

## Executive Summary

The task scheduling and notifications feature is well-structured overall: domain types are clean, test coverage is broad, and the separation between SchedulerService, NotificationService, and ChatTaskManager is appropriate. However, there is one correctness bug that causes recurring tasks to be incorrectly reverted to "pending" status immediately after dispatch (overwriting the "running" state set by RunNow), and two input-validation gaps in the HTTP layer that permit out-of-range values to reach the domain. Several secondary issues reduce confidence in production correctness — most importantly, silent persist() failures that produce no log output and a goroutine in the legacy `Dispatch` path that ignores its parent context.

---

## Functional Requirements

---

### FR-001 (P0): Recurring task status overwrite after dispatch

**Finding:** In `internal/application/scheduling/scheduler_service.go`, `processTask` discards the return value of `dispatcher.RunNow` (which sets the task to `running` in the store), then calls `s.store.Update(ctx, task)` using the *original* snapshot from `ListDue`. That snapshot has `Status = DirectTaskStatusPending`. The explicit assignment on line 109 (`task.Status = domain.DirectTaskStatusPending`) then writes `pending` back over the `running` record that RunNow just persisted. For every recurring task that fires, the store briefly shows the task as `running` (from RunNow), then immediately reverts to `pending`. If the scheduler ticks again before the task completes, `ListDue` will return it again, causing double-dispatch.

**Root cause:** `processTask` should re-read the task from the store (or use the return value of `RunNow`) before issuing the reschedule update, rather than reusing the stale ListDue snapshot.

**TDD guidance — Red:** Write a scheduler_service integration test (using the real `InMemoryDirectTaskStore` and a fake dispatcher that records the status written by the Update call) that asserts: after `Tick` on a recurring task, the store record has `Status = running` (or a status other than `pending`) immediately after `RunNow` and before the task completes.

**TDD guidance — Green:** Change `processTask` to re-fetch the task from the store after `RunNow` succeeds, apply only `NextRunAt` to the freshly-fetched record (preserving the `running` status), and call `Update` with that record.

**TDD guidance — Refactor:** Extract a `rescheduleRecurring(ctx, task, now)` helper that encapsulates the re-fetch and update to keep `processTask` readable.

Acceptance Criteria:
- [ ] After `processTask` completes for a recurring task, `store.Get(id).Status` is `running`, not `pending`.
- [ ] A double-dispatch scenario (scheduler ticks while task is running) is prevented by the `ListDue` filter (which already excludes non-pending tasks) and is covered by a test.
- [ ] The existing `TestTick_RecurringTask_DispatchesAndAdvancesNextRunAt` is updated to assert `running` status is preserved in the real store (not just mock).

---

### FR-002 (P0): RunNow does not guard against `dispatching` status

**Finding:** `LocalTaskDispatcher.RunNow` in `internal/infrastructure/local/orchestrator/task_dispatcher.go` only guards against `DirectTaskStatusRunning` (returns early). It does not guard against `DirectTaskStatusDispatching`. If `ClaimDue` transitions the task to `dispatching` and then `RunNow` is called before `dispatchNow` completes, a concurrent call to `RunNow` for the same task ID (e.g., from a crash-recovery path or a duplicate HTTP call) will see `dispatching` and call `dispatchNow` again, sending a second message to the bot for the same task.

**TDD guidance — Red:** Write a test for `RunNow` that seeds the store with a task in `DirectTaskStatusDispatching` and asserts that `RunNow` returns the task as-is without calling `sendMessage`.

**TDD guidance — Green:** Add `DirectTaskStatusDispatching` to the early-return guard in `RunNow`.

**TDD guidance — Refactor:** Extract the "terminal or in-progress status" check to a domain helper function `DirectTask.IsAlreadyDispatched() bool` to keep the guard readable and reusable.

Acceptance Criteria:
- [ ] `RunNow` returns the task unchanged when its status is `dispatching`.
- [ ] A test covers this path and passes under `-race`.

---

### FR-003 (P1): `parseRecurrenceRequest` does not validate `Frequency`

**Finding:** In `internal/infrastructure/http/notifications.go`, `parseRecurrenceRequest` casts the raw string directly to `domain.RecurrenceFrequency` without checking whether it is one of the three valid values (`daily`, `weekly`, `monthly`). An unknown frequency (e.g., `"hourly"`) is silently accepted and passed to the domain. `RecurrenceRule.NextAfter` then falls through to its `default:` case and behaves as daily, producing a silently incorrect schedule. `RecurrenceRule.Validate` does not validate the `Frequency` field at all.

**TDD guidance — Red:** Write a test for `parseRecurrenceRequest` with `Frequency = "hourly"` and assert a non-nil error. Also add a `RecurrenceRule.Validate` test with `Frequency = ""` (zero value) asserting error.

**TDD guidance — Green:** (a) Add a `Frequency` check in `RecurrenceRule.Validate` that returns an error if `Frequency` is not one of the three known values. (b) `parseRecurrenceRequest` already calls `s.Validate()` transitively through `parseScheduleRequest`, so once `Validate` is fixed the HTTP layer is automatically protected.

**TDD guidance — Refactor:** Move the set of valid frequencies into a domain-level `ValidFrequencies` slice or a `RecurrenceFrequency.IsValid() bool` method to avoid duplication.

Acceptance Criteria:
- [ ] `RecurrenceRule{Frequency: ""}.Validate()` returns a non-nil error.
- [ ] `RecurrenceRule{Frequency: "hourly"}.Validate()` returns a non-nil error.
- [ ] POST `/api/v1/bots/:bot/tasks` with `frequency: "hourly"` returns 400.
- [ ] Tests cover all three valid frequencies still pass.

---

### FR-004 (P1): `parseRecurrenceRequest` does not validate `Time` field range

**Finding:** `parseRecurrenceRequest` uses `fmt.Sscanf(req.Time, "%d:%d", &h, &m)` to parse the `HH:MM` string but performs no range checks. Values like `25:00`, `9:99`, or negative values are accepted and stored as `TimeOfDay` durations. These produce incorrect fire times without any error surfacing to the caller.

**TDD guidance — Red:** Write HTTP-layer tests asserting that `schedule.recurrence.time = "25:00"` and `"9:99"` return 400.

**TDD guidance — Green:** After parsing `h` and `m`, validate `0 <= h <= 23` and `0 <= m <= 59`. Return an error if either is out of range.

**TDD guidance — Refactor:** Extract a `parseHHMM(s string) (time.Duration, error)` pure function that is unit-tested independently of the HTTP layer.

Acceptance Criteria:
- [ ] `"25:00"` produces a 400 Bad Request.
- [ ] `"9:99"` produces a 400 Bad Request.
- [ ] `"00:00"` and `"23:59"` are accepted and produce the correct `TimeOfDay` durations.

---

### FR-005 (P1): `Config.Notifications` is a concrete type, violating layer boundaries

**Finding:** `httpserver.Config.Notifications` in `internal/infrastructure/http/server.go` is declared as `*appnotifications.NotificationService` — a pointer to a concrete application-layer struct. The HTTP infrastructure layer should depend only on a behavioural interface, not on the concrete application type. This creates a hard compile-time dependency from the infrastructure layer into the application layer's concrete type, making it impossible to swap the implementation in tests without constructing a real `NotificationService`.

**TDD guidance — Red:** Write a test that injects a minimal stub implementing only the notification methods into the server config. This will not compile until the type is changed to an interface.

**TDD guidance — Green:** Define a `NotificationService` interface in `internal/infrastructure/http/server.go` (or in the domain layer) with the methods used by the handlers (`List`, `UnreadCount`, `AppendDiscuss`, `ActionNotification`, `RequeueTask`, `Delete`). Change `Config.Notifications` to that interface type.

**TDD guidance — Refactor:** Move the interface to the domain layer as `domain.AgentNotificationService` if multiple infrastructure adapters may implement it, or keep it in the http package as a local interface if only one adapter is needed.

Acceptance Criteria:
- [ ] `Config.Notifications` is declared as an interface type, not `*appnotifications.NotificationService`.
- [ ] The existing `NotificationService` in `application/notifications` satisfies the interface.
- [ ] HTTP handler tests use a stub struct that only implements the interface (not a full `NotificationService`).

---

### FR-006 (P1): `ScheduledTaskDispatcher` interface duplicated across layers

**Finding:** The `ScheduledTaskDispatcher` interface is defined in two places: `internal/application/orchestrator/chat_task_manager.go` and `internal/infrastructure/http/server.go`. Both definitions are structurally identical. Duplicating interface definitions at two layers means any future signature change requires synchronised edits and introduces risk of divergence.

**TDD guidance — Red:** Add a compile-time interface satisfaction check in one package that will fail if the two definitions diverge.

**TDD guidance — Green:** Move the interface to the domain layer as part of `domain.TaskDispatcher` or as a separate `domain.ScheduledTaskDispatcher`, then import it from both packages.

**TDD guidance — Refactor:** Remove the local definitions and update the single import site.

Acceptance Criteria:
- [ ] `ScheduledTaskDispatcher` is defined in exactly one location (domain layer preferred).
- [ ] Both `ChatTaskManager` and the HTTP server use the same definition.
- [ ] A compile-time satisfaction assertion (`var _ domain.ScheduledTaskDispatcher = (*LocalTaskDispatcher)(nil)`) exists in the infrastructure package.

---

### FR-007 (P1): `dispatchAt` goroutine loses parent context and leaks on shutdown

**Finding:** In `LocalTaskDispatcher.Dispatch` (legacy path, `internal/infrastructure/local/orchestrator/task_dispatcher.go`), when `scheduledAt` is in the future, a goroutine is spawned with `go d.dispatchAt(created, *scheduledAt)`. Inside `dispatchAt`, a fresh `context.Background()` is used rather than the caller's context. This goroutine will continue running even after the application's context is cancelled (e.g., during graceful shutdown), and any in-flight timer cannot be stopped. This is a goroutine leak on shutdown and prevents clean termination.

Note: the `SchedulerService` / `DispatchWithSchedule` path correctly stores future tasks as `pending` and relies on the scheduler loop (which respects context cancellation). The `dispatchAt` goroutine is the legacy path that bypasses this mechanism.

**TDD guidance — Red:** Write a test that calls `Dispatch` with a far-future `scheduledAt`, then cancels the context and asserts the goroutine has exited within a reasonable timeout.

**TDD guidance — Green:** Refactor `Dispatch` to deprecate the goroutine path entirely — future `scheduledAt` values should be stored with `ScheduleModeFuture` and `NextRunAt`, following the same pattern as `DispatchWithSchedule`. The scheduler loop then handles the dispatch. Remove `dispatchAt`.

**TDD guidance — Refactor:** If backward compatibility with callers passing a raw `*time.Time` is required, have `Dispatch` delegate to `DispatchWithSchedule` internally.

Acceptance Criteria:
- [ ] No goroutine is spawned by `Dispatch` for future `scheduledAt` values.
- [ ] Future tasks created via `Dispatch` are stored as `pending` with `Schedule.Mode = Future` and `NextRunAt` set.
- [ ] Existing tests for the scheduled dispatch path still pass.

---

### FR-008 (P1): `AppendDiscuss` in `NotificationService` has a TOCTOU window

**Finding:** `NotificationService.AppendDiscuss` performs a read-modify-write cycle: `store.Get` → local modification → `store.Save`. The in-memory store's read lock is released between `Get` and `Save`. Under concurrent calls for the same notification ID, two goroutines can each read the same state, apply independent changes, and one will silently overwrite the other's entry in the discuss thread. The 100-entry cap and status transition are both subject to this race.

**TDD guidance — Red:** Write a concurrent test that calls `AppendDiscuss` 50 times in parallel for the same notification ID and asserts the discuss thread length is exactly 50 (no entries lost). This test will be flaky or fail under `-race` before the fix.

**TDD guidance — Green:** Move the read-modify-write logic into the store layer as an atomic operation (the store's mutex already guards single operations; extend it to cover the full append+save). Alternatively, use a per-notification mutex at the service level.

**TDD guidance — Refactor:** The `AppendDiscuss` method on `AgentNotificationStore` already exists for exactly this reason — the service should use `store.AppendDiscuss` instead of `store.Get + store.Save`. Update `NotificationService.AppendDiscuss` to call `store.AppendDiscuss` for the thread append, and handle the status-transition and cap-enforcement separately with a focused `store.UpdateStatus` or similar atomic call.

Acceptance Criteria:
- [ ] `NotificationService.AppendDiscuss` uses the store's atomic `AppendDiscuss` method instead of `Get + Save`.
- [ ] A concurrent test with `-race` passes.
- [ ] The 100-entry cap and unread→read transition are still applied correctly.

---

### FR-009 (P2): `persist()` silently swallows errors with no logging

**Finding:** Both `InMemoryAgentNotificationStore.persist()` and `InMemoryDirectTaskStore.persist()` silently `return` on any error (JSON marshal failure, directory creation failure, file write failure, rename failure). There is no `slog` call, no metric, and no returned error. A corrupt or full disk will cause all mutations to be silently lost without any operator-visible signal.

**TDD guidance — Red:** Inject a filesystem abstraction (or a write function) that can be configured to fail. Assert that a `slog` error message is emitted when `os.WriteFile` fails.

**TDD guidance — Green:** Add `slog.Error(...)` calls in `persist()` on each failure branch, with sufficient context (path, operation, error).

**TDD guidance — Refactor:** Consider promoting the `persist` error to a return value and surfacing it to the caller (the mutation methods), so callers can propagate it to the HTTP response where appropriate.

Acceptance Criteria:
- [ ] Every `return` in `persist()` on an error path is preceded by a `slog.Error` call.
- [ ] The `slog` message includes the file path and the originating operation.
- [ ] A unit test asserts the log is emitted when a write fails (use a testable log handler or slog test helper).

---

### FR-010 (P2): `ChatTaskManager.pendingMap` entries never expire

**Finding:** `ChatTaskManager.pendingMap` (a `sync.Map`) accumulates `ChatTaskIntent` entries indefinitely. An entry is only removed when the user sends a confirmation or cancellation in the same thread. If the user abandons the conversation, sends a typo, or the thread is never resumed, the entry lives in memory forever. This is a minor memory leak for long-running processes with many threads. More concretely, after a restart the intent is gone but the test `TestDetectAndHandle_Confirmation_ClearsPending` relies on in-process state not persisting across restarts — this is by design but should be documented.

Additionally, a stale intent can be accidentally confirmed much later if a user sends "yes" to a completely different question in the same thread.

**TDD guidance — Red:** Write a test that stores an intent, waits past a configurable TTL, sends "yes", and asserts the intent has expired (no dispatch).

**TDD guidance — Green:** Add a `CreatedAt time.Time` field to `ChatTaskIntent` and a configurable TTL (default 10 minutes). In `DetectAndHandle`, check if the pending intent is expired before treating it as confirmable.

**TDD guidance — Refactor:** Optionally start a background goroutine (driven by the application context) that periodically purges expired entries from `pendingMap`.

Acceptance Criteria:
- [ ] A pending intent older than the configured TTL is treated as non-existent.
- [ ] `isConfirmation` returns `handled=false` (not `handled=true`) when the intent has expired.
- [ ] TTL is configurable via `NewChatTaskManager` and defaults to 10 minutes.

---

### FR-011 (P2): Author identity in `handleNotificationDiscuss` is hardcoded

**Finding:** `handleNotificationDiscuss` in `internal/infrastructure/http/notifications.go` calls `s.cfg.Notifications.AppendDiscuss(r.Context(), id, "user", req.Message)`. The author is unconditionally set to the literal string `"user"`. The authenticated user's identity is available in the request context via `claimsFromContext(r).Subject` (as used in at least five other handlers). Using a hardcoded author makes the discuss thread audit trail unreliable — all entries from any operator appear as `"user"` rather than their actual username.

**TDD guidance — Red:** Write a test that authenticates as two different users and calls the discuss endpoint, then asserts the `DiscussThread[i].Author` matches the authenticated user's subject, not the string `"user"`.

**TDD guidance — Green:** Replace the hardcoded `"user"` with `claimsFromContext(r).Subject`.

**TDD guidance — Refactor:** If `claimsFromContext` can return an empty subject (unauthenticated path), add a fallback and a unit test for that case.

Acceptance Criteria:
- [ ] The `Author` field of each discuss entry matches the authenticated user's subject (username / claim).
- [ ] If `Subject` is empty for any reason, a safe fallback (e.g., `"unknown"`) is used rather than panicking.

---

### FR-012 (P2): `Tick` and `CatchUpMissedRuns` have identical bodies

**Finding:** `SchedulerService.Tick` and `CatchUpMissedRuns` in `internal/application/scheduling/scheduler_service.go` have byte-for-byte identical implementations. The semantic distinction (periodic tick vs. startup catch-up) is real, but there is no code difference. Any future change to the catch-up logic (e.g., rate-limiting how many missed tasks are dispatched at once) requires modifying both methods and keeping them in sync. This is a code quality issue and a future bug risk.

**TDD guidance — Red:** Write a test that relies on a diverged implementation of `CatchUpMissedRuns` (e.g., rate-limited to 5 dispatches per call) that fails on the current implementation.

**TDD guidance — Green:** Extract the shared body into a private `processAllDue(ctx, now)` helper. Both `Tick` and `CatchUpMissedRuns` delegate to it. When catch-up semantics diverge (e.g., rate-limiting), override only `CatchUpMissedRuns`.

**TDD guidance — Refactor:** Add a brief comment to each public method explaining when it is called and what behaviour may differ in future.

Acceptance Criteria:
- [ ] `Tick` and `CatchUpMissedRuns` share a private helper rather than duplicating the body.
- [ ] Existing tests continue to pass.

---

### FR-013 (P2): `parseTimeOfDay` contains an unused variable `digits`

**Finding:** In `internal/application/orchestrator/chat_task_manager.go`, `parseTimeOfDay` declares `digits := strings.TrimFunc(rest, ...)` and then immediately does `_ = digits` on the next line. The variable is computed but never used. `go vet` would flag this if `_ =` were absent; the blank assignment suppresses the warning without addressing the issue. The variable was likely a leftover from an earlier implementation.

**TDD guidance:** No new test is needed. Remove the two lines (the assignment and the blank suppressor). Run `go vet` to confirm no warnings.

Acceptance Criteria:
- [ ] The unused `digits` variable and its blank-assignment suppressor are removed.
- [ ] `go vet ./...` passes with zero warnings.

---

### FR-014 (P2): `RecurrenceRule.Validate` does not reject `MonthDay > 31`

**Finding:** `RecurrenceRule.Validate` checks `MonthDay == 0` but does not check `MonthDay > 31`. A value of 32 or higher is accepted and stored. `nextAfterMonthly` passes it directly to `time.Date`, which normalises overflow (day 32 of May → June 1). This silent normalisation produces a different fire date than the operator intended without any validation error.

**TDD guidance — Red:** Write a `Validate` test with `MonthDay = 32` asserting a non-nil error. Also add a test with `MonthDay = -1` for symmetry.

**TDD guidance — Green:** Add bounds checks in `Validate`: `MonthDay < 1 || MonthDay > 31`.

**TDD guidance — Refactor:** Update the existing comment from "1–31" to add a note that the domain rejects values outside this range, and that `nextAfterMonthly` relies on `time.Date` normalisation only for short-months (e.g., `MonthDay=31` in February), which is intentional behaviour documented in the code comment.

Acceptance Criteria:
- [ ] `RecurrenceRule{Frequency: Monthly, MonthDay: 32}.Validate()` returns a non-nil error.
- [ ] `RecurrenceRule{Frequency: Monthly, MonthDay: 0}.Validate()` returns a non-nil error (already exists).
- [ ] `RecurrenceRule{Frequency: Monthly, MonthDay: 31}.Validate()` passes.

---

### FR-015 (P2): Missing unit tests for `parseScheduleRequest` / `parseRecurrenceRequest` helpers

**Finding:** `parseScheduleRequest` and `parseRecurrenceRequest` are package-private functions in `internal/infrastructure/http/notifications.go`. They are exercised only through end-to-end HTTP handler tests (e.g., `TestBotTaskCreate_WithScheduleRecurring`). Edge cases — unknown frequency, invalid time, missing recurrence object for recurring mode, `run_at` in the past — are not individually unit-tested. The helpers are exported-by-convention candidates (the file already exports other helpers); they should have their own table-driven test.

**TDD guidance — Red:** Write `TestParseScheduleRequest` and `TestParseRecurrenceRequest` table-driven tests covering: (a) nil request → ASAP, (b) unknown mode → error, (c) future mode with nil RunAt → error, (d) recurring mode with nil recurrence object → error, (e) invalid frequency → error (once FR-003 is resolved), (f) invalid time format → error (once FR-004 is resolved), (g) valid daily/weekly/monthly → correct rule.

**TDD guidance — Green:** Export `ParseScheduleRequest` and `ParseRecurrenceRequest` (uppercase) or move them to a testable internal package.

**TDD guidance — Refactor:** Consider moving schedule parsing into a dedicated `internal/infrastructure/http/schedule_parse.go` file with its own `_test.go` so it is not buried in `notifications.go`.

Acceptance Criteria:
- [ ] Table-driven tests exist for `parseScheduleRequest` and `parseRecurrenceRequest` covering at least the 7 cases listed above.
- [ ] Tests live in the same package (`httpserver_test` or `httpserver` with an export test file).

---

### FR-016 (P2): `handleNotificationDelete` does not validate that `IDs` is non-empty

**Finding:** `handleNotificationDelete` decodes the JSON body and passes `req.IDs` directly to `s.cfg.Notifications.Delete`. If the request body is `{"ids":[]}` or `{"ids":null}`, the delete method is called with an empty or nil slice. The store silently no-ops, returning 200 OK. This is not a security issue, but it is a usability issue: callers that accidentally send empty IDs receive a success response with no indication that nothing was deleted.

**TDD guidance — Red:** Write a test that sends `DELETE /api/v1/notifications` with `{"ids":[]}` and asserts a 400 Bad Request.

**TDD guidance — Green:** Add a check after decoding: `if len(req.IDs) == 0 { writeError(w, http.StatusBadRequest, "ids must not be empty"); return }`.

**TDD guidance — Refactor:** Consider consolidating all request-body validation into a named struct with a `validate() error` method for consistency with other handlers.

Acceptance Criteria:
- [ ] `DELETE /api/v1/notifications` with `{"ids":[]}` returns 400.
- [ ] `DELETE /api/v1/notifications` with `{"ids":["n1"]}` still returns 200.

---

## Implementation Guidance

All fixes follow the TDD red-green-refactor cycle documented per finding. Prioritisation:

| Priority | FRs | Rationale |
|---|---|---|
| P0 (block PR) | FR-001, FR-002 | Correctness bugs: incorrect task status in store and potential double-dispatch |
| P1 (must fix) | FR-003, FR-004, FR-005, FR-006, FR-007, FR-008 | Input validation, architecture violations, goroutine leak, concurrency race |
| P2 (should fix) | FR-009 through FR-016 | Observability, code quality, and minor usability gaps |

P0 findings must be resolved before the PR is merged. P1 findings should be resolved in the same PR. P2 findings may be deferred to follow-up issues if agreed with the team.
