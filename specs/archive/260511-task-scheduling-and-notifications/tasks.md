# Tasks: Task Scheduling and Notifications

**Feature:** Task Scheduling and Notifications
**Created:** 2026-05-11
**Status:** Planning

---

## Progress Summary

**0 / 44 tasks complete**

---

## M1 — Domain Model

| ID | Task | Deps | Est (h) | Status | Acceptance Criteria |
|---|---|---|---|---|---|
| M1.1 | Define `ScheduleMode` and `RecurrenceFrequency` enums | — | 0.5 | ⬜ | Enums exist in `internal/domain/schedule.go`; `go vet` passes |
| M1.2 | Implement `RecurrenceRule` value object with `NextAfter()` | M1.1 | 2 | ⬜ | `NextAfter` returns correct next time for daily/weekly/monthly cases; unit tests pass |
| M1.3 | Implement `Schedule` value object with `NextRunAt()` and `Validate()` | M1.2 | 1 | ⬜ | `NextRunAt` delegates correctly for all three modes; invalid states rejected |
| M1.4 | Define `NotificationStatus` enum and `DiscussEntry` type | — | 0.5 | ⬜ | Types defined in `internal/domain/notification.go` |
| M1.5 | Implement `Notification` entity | M1.4 | 1 | ⬜ | All fields present; constructor validates required fields |
| M1.6 | Define `NotificationStore` interface | M1.5 | 0.5 | ⬜ | Interface defined with all CRUD + discuss + unread-count methods |
| M1.7 | Extend `Task` domain type with `Schedule` and `NextRunAt` | M1.3 | 1 | ⬜ | `Task` has `Schedule Schedule` and `NextRunAt *time.Time`; existing tests still pass |
| M1.8 | Write unit tests for `RecurrenceRule.NextAfter()` edge cases | M1.2 | 2 | ⬜ | Day-exclusion, month boundary, DST boundary, leap year all tested |
| M1.9 | Write unit tests for `Schedule.NextRunAt()` all modes | M1.3 | 1 | ⬜ | ASAP returns nil, Future returns RunAt, Recurring delegates to rule |
| M1.10 | Generate/write mock for `NotificationStore` | M1.6 | 0.5 | ⬜ | Mock exists at `internal/domain/mocks/mock_notification_store.go` |

## M2 — Scheduler Service

| ID | Task | Deps | Est (h) | Status | Acceptance Criteria |
|---|---|---|---|---|---|
| M2.1 | Write failing test for `SchedulerService.Tick()` | M1.7 | 1 | ⬜ | Test exists and fails (red) |
| M2.2 | Implement `SchedulerService.Tick()` | M2.1 | 2 | ⬜ | Due tasks enqueued; recurring tasks get updated `NextRunAt`; test passes |
| M2.3 | Write failing test for `CatchUpMissedRuns()` | M2.2 | 1 | ⬜ | Test exists and fails (red) |
| M2.4 | Implement `CatchUpMissedRuns()` | M2.3 | 2 | ⬜ | Missed tasks enqueued; multiple recurring occurrences collapsed; test passes |
| M2.5 | Implement scheduler loop goroutine (10s ticker) | M2.2 | 1 | ⬜ | Loop starts/stops cleanly; calls `Tick` on interval |
| M2.6 | Wire scheduler loop into `cmd/boabot/main.go` | M2.5 | 0.5 | ⬜ | Loop starts when orchestrator enabled; stops on shutdown signal |

## M3 — Notification Service

| ID | Task | Deps | Est (h) | Status | Acceptance Criteria |
|---|---|---|---|---|---|
| M3.1 | Write failing tests for `RaiseNotification` | M1.10 | 1 | ⬜ | Test exists and fails (red) |
| M3.2 | Implement `RaiseNotification` | M3.1 | 1 | ⬜ | Notification persisted via store; test passes |
| M3.3 | Implement `ListNotifications` and `UnreadCount` | M3.2 | 1 | ⬜ | Filter applied correctly; count accurate |
| M3.4 | Write failing test for `AppendDiscuss` | M3.2 | 0.5 | ⬜ | Test exists and fails (red) |
| M3.5 | Implement `AppendDiscuss` and `ActionNotification` | M3.4 | 1 | ⬜ | Thread appended; status updated; test passes |
| M3.6 | Write failing test for `RequeueTask` | M3.5 | 1 | ⬜ | Test exists and fails (red) |
| M3.7 | Implement `RequeueTask` | M3.6 | 1.5 | ⬜ | Discuss context appended to task; task status set to queued; test passes |
| M3.8 | Implement `DeleteNotifications` | M3.2 | 0.5 | ⬜ | Bulk delete confirmed via store mock |

## M4 — DB Migration and Repos

| ID | Task | Deps | Est (h) | Status | Acceptance Criteria |
|---|---|---|---|---|---|
| M4.1 | Write DB migration: extend `tasks`, create `notifications` | M1.7 | 1.5 | ⬜ | Migration runs without error; rollback possible |
| M4.2 | Extend `TaskRepo` for `schedule_mode`, `recurrence_rule`, `next_run_at` | M4.1 | 2 | ⬜ | Save and load round-trip; existing task data unaffected |
| M4.3 | Implement `NotificationRepo` (MariaDB) | M4.1 | 3 | ⬜ | All `NotificationStore` methods implemented; integration tests pass |
| M4.4 | Add `ListDueTasks(now time.Time)` to `TaskRepo` | M4.2 | 1 | ⬜ | Returns only tasks with `next_run_at <= now` and status not running |

## M5 — API Layer

| ID | Task | Deps | Est (h) | Status | Acceptance Criteria |
|---|---|---|---|---|---|
| M5.1 | Extend task create/update handlers to accept `schedule` object | M4.2 | 2 | ⬜ | `schedule` parsed and persisted; backwards-compatible with ASAP default |
| M5.2 | Implement `GET /api/v1/notifications` | M4.3 | 1 | ⬜ | Returns filtered list; supports bot/status/search params |
| M5.3 | Implement `GET /api/v1/notifications/count` | M4.3 | 0.5 | ⬜ | Returns `{"unread": N}` |
| M5.4 | Implement `POST /api/v1/notifications/:id/discuss` | M4.3 | 1 | ⬜ | Appends entry; returns updated notification |
| M5.5 | Implement `POST /api/v1/notifications/:id/requeue` | M4.3 | 1 | ⬜ | Task re-queued with discuss context |
| M5.6 | Implement `DELETE /api/v1/notifications` | M4.3 | 0.5 | ⬜ | Bulk delete by ID list |

## M6 — Web UI

| ID | Task | Deps | Est (h) | Status | Acceptance Criteria |
|---|---|---|---|---|---|
| M6.1 | Add scheduling mode selector to Add Task dialog (ASAP/Future/Recurring) | M5.1 | 2 | ⬜ | Three modes visible; correct fields shown/hidden per mode |
| M6.2 | Add Future datetime picker to Add Task dialog | M6.1 | 1 | ⬜ | Date/time input appears in Future mode only |
| M6.3 | Add Recurring visual builder (frequency + day checkboxes + time) | M6.1 | 3 | ⬜ | Builder appears in Recurring mode; produces correct schedule object |
| M6.4 | Add NL text input + preview to Recurring mode | M6.3 | 2 | ⬜ | User can toggle to text input; preview shows parsed rule before confirm |
| M6.5 | Update Task detail screen to show Next Run | M5.1 | 1 | ⬜ | "Next run: <datetime>" shown for Future/Recurring; ASAP shows mode label |
| M6.6 | Add Notifications tab to nav with unread badge | M5.3 | 1.5 | ⬜ | Tab visible; badge increments/clears; polling `/count` every 15s |
| M6.7 | Build Notifications list screen | M5.2 | 3 | ⬜ | Filters, bot filter, search, Refresh, Delete Selected all functional |
| M6.8 | Build Notification detail screen with Discuss panel | M5.4 | 3 | ⬜ | Message, context, thread visible; user can post; Re-queue button works |

## M7 — Chat-Driven Task Management

| ID | Task | Deps | Est (h) | Status | Acceptance Criteria |
|---|---|---|---|---|---|
| M7.1 | Extend Chat handler with task-management intent detection | M5.1 | 2 | ⬜ | Create/edit/delete/list intents recognised |
| M7.2 | Implement NL → `Schedule` parsing via model call | M7.1 | 2 | ⬜ | Parsed rule shown in confirmation prompt; ambiguous input asks for clarification |
| M7.3 | Implement confirmation gate (human must approve before persist) | M7.2 | 1.5 | ⬜ | No task saved without explicit confirmation; bot rejection tested |

## M8 — Integration and Quality Pass

| ID | Task | Deps | Est (h) | Status | Acceptance Criteria |
|---|---|---|---|---|---|
| M8.1 | Run full test suite; fix failures | M7.3 | 2 | ⬜ | `go test -race ./...` passes |
| M8.2 | Coverage check; write tests to close any gap | M8.1 | 2 | ⬜ | ≥ 90% on domain + application layers |
| M8.3 | Lint pass | M8.2 | 1 | ⬜ | `golangci-lint run` zero warnings |
| M8.4 | Manual acceptance criteria verification | M8.3 | 2 | ⬜ | All 11 ACs checked off in spec.md |
