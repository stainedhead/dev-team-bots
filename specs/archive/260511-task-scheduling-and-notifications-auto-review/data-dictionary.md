# Data Dictionary: Code Review Fixes — Task Scheduling and Notifications

**Created:** 2026-05-11

---

## Modified Types

### `RecurrenceRule` (domain/schedule.go)

- `Validate()` now rejects: empty `Frequency`, unknown `Frequency` values, `MonthDay < 1 || MonthDay > 31`

### `DirectTask` (domain/direct_task.go)

- May gain `IsAlreadyDispatched() bool` helper (FR-002 refactor)

### `ChatTaskIntent` (application/orchestrator/chat_task_manager.go)

- Gains `CreatedAt time.Time` field for TTL expiry (FR-010)

---

## New Interfaces

### `NotificationService` (infrastructure/http/server.go or domain)

Methods: `List`, `UnreadCount`, `AppendDiscuss`, `ActionNotification`, `RequeueTask`, `Delete`
Used by: HTTP notification handlers via `Config.Notifications`

### `ScheduledTaskDispatcher` (domain/direct_task.go — consolidated)

`DispatchWithSchedule(ctx, botName, instruction string, schedule Schedule, source DirectTaskSource, threadID, workDir, title string) (DirectTask, error)`
Used by: `ChatTaskManager` and HTTP server (formerly duplicated)

---

## New Functions

### `parseHHMM(s string) (time.Duration, error)` (infrastructure/http — FR-004 refactor)

Parses `HH:MM` strings, validates `0 <= h <= 23` and `0 <= m <= 59`.

### `processAllDue(ctx context.Context, now time.Time) error` (application/scheduling — FR-012 refactor)

Private helper shared by `Tick` and `CatchUpMissedRuns`.

---

## Enumerations

### `RecurrenceFrequency` — valid values (FR-003)

- `"daily"`
- `"weekly"`
- `"monthly"`

Any other value → `Validate()` error.
