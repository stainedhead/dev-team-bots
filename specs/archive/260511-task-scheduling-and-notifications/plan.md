# Plan: Task Scheduling and Notifications

**Feature:** Task Scheduling and Notifications
**Created:** 2026-05-11
**Status:** Planning

---

## Development Approach

TDD throughout (Red → Green → Refactor). Work inward-out: domain types and logic first, application use cases second, infrastructure last. Each milestone produces tested, committed code before the next starts.

---

## Phase Breakdown

### M1 — Domain Model
- `Schedule`, `RecurrenceRule`, `ScheduleMode`, `RecurrenceFrequency` value objects
- `RecurrenceRule.NextAfter()` and `Schedule.NextRunAt()` calculation logic
- `Notification`, `DiscussEntry`, `NotificationStatus` entity
- `NotificationStore` interface
- `SchedulerLoop` interface stub
- Extend `Task` domain type with `Schedule` and `NextRunAt`
- All domain tests

### M2 — Scheduler Service
- `SchedulerService`: `Tick`, `CatchUpMissedRuns`, `RecalcNextRun`
- Unit tests with mocked `TaskRepo`
- Scheduler loop goroutine wired in `cmd/boabot/main.go`

### M3 — Notification Service
- `NotificationService`: `RaiseNotification`, `ListNotifications`, `UnreadCount`, `AppendDiscuss`, `ActionNotification`, `RequeueTask`, `DeleteNotifications`
- Unit tests with mocked `NotificationStore`

### M4 — DB Migration and Repos
- Migration: add `schedule_mode`, `recurrence_rule`, `next_run_at` to `tasks`; create `notifications` table
- Extend `TaskRepo` for new columns
- Implement `NotificationRepo` (MariaDB)
- Integration tests (tagged `//go:build integration`)

### M5 — API Layer
- Extend `POST /api/v1/bots/:bot/tasks` and `PUT /api/v1/tasks/:id` to accept `schedule` object
- `GET /api/v1/notifications` — list with filter
- `GET /api/v1/notifications/count` — unread badge count
- `POST /api/v1/notifications/:id/discuss` — append discuss entry
- `POST /api/v1/notifications/:id/requeue` — requeue task
- `DELETE /api/v1/notifications` — bulk delete
- API handler tests

### M6 — Web UI
- Add Task dialog: scheduling mode selector (ASAP / Future / Recurring), Future datetime picker, Recurring visual builder (frequency + day checkboxes + time), NL text input with preview
- Task detail screen: show scheduling mode and "Next run: <datetime>" for Future/Recurring
- Notifications tab: nav item + unread badge (polling `/count`)
- Notifications list screen: filters, bot filter, search, Refresh, Delete Selected
- Notification detail screen: message, context, Discuss panel, Re-queue button

### M7 — Chat-Driven Task Management
- Extend Orchestrator Chat handler: intent detection for task create/edit/delete/list
- NL → `Schedule` parsing via model invocation
- Confirmation prompt flow before persisting
- Tests for approval gate (bot cannot bypass without human confirmation)

### M8 — Integration and Quality Pass
- Full test suite; coverage ≥ 90% on domain and application layers
- `go vet`, `golangci-lint`, `go test -race`
- Manual acceptance criteria verification

---

## Critical Path

```
M1 (domain) → M2 (scheduler) → M4 (DB) → M5 (API) → M6 (UI)
M1 (domain) → M3 (notifications) → M4 (DB) → M5 (API) → M6 (UI)
M6 (UI) → M7 (Chat)
M7 → M8 (quality)
```

M1 is the root dependency. M2 and M3 can proceed in parallel after M1. M4 can start in parallel with M2/M3 once domain types are stable.

---

## Testing Strategy

- **Unit tests:** All domain and application logic; mock all interfaces at adapter seams
- **Integration tests:** DB repos against real schema (tagged `//go:build integration`); scheduling loop with in-memory task store
- **Manual / acceptance:** Spin up orchestrator, verify each acceptance criterion from spec.md
- **Coverage gate:** ≥ 90% on `internal/domain/...` and `internal/application/...`

---

## Rollout Strategy

- Feature is entirely behind `orchestrator.enabled` config flag — no impact on non-orchestrator bots
- DB migration is non-breaking (new columns with safe defaults, new table)
- Existing tasks without `schedule_mode` default to ASAP behaviour

---

## Success Metrics

- All 11 acceptance criteria pass
- Coverage gate met
- Scheduling loop demonstrated firing within 30s in local dev
- Notifications survive a simulated UI disconnect and reconnect
