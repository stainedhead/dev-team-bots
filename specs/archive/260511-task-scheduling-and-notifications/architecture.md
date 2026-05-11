# Architecture: Task Scheduling and Notifications

**Feature:** Task Scheduling and Notifications
**Created:** 2026-05-11
**Status:** Draft

---

## Architecture Overview

This feature adds three orthogonal but connected subsystems inside the `boabot` module, all behind the `orchestrator.enabled` config flag:

1. **Scheduling subsystem** — domain value objects for `Schedule` and `RecurrenceRule`, an application-layer `SchedulerService`, and an infrastructure-layer goroutine that ticks every ~10 seconds to enqueue due tasks.
2. **Notification subsystem** — domain `Notification` entity, `NotificationStore` interface, application-layer `NotificationService`, DB adapter, and REST/UI layer.
3. **Chat-driven task management** — extension of the Orchestrator bot's Chat handler to recognise task management intent, parse scheduling from natural language, and gate on human confirmation.

All three subsystems respect Clean Architecture: domain knows nothing of infrastructure; application layer orchestrates domain logic; infrastructure implements domain interfaces.

---

## Component Architecture

```
cmd/boabot/main.go
  └── OrchestratorServer
        ├── SchedulerService (application)        ← NEW
        │     └── SchedulerLoop goroutine          ← NEW
        ├── NotificationService (application)      ← NEW
        │     └── NotificationStore (DB adapter)   ← NEW
        ├── TaskService (application, extended)    ← MODIFIED
        │     └── TaskRepo (DB adapter, extended)  ← MODIFIED
        └── HTTPServer (infrastructure)
              ├── /api/v1/tasks (extended)         ← MODIFIED
              ├── /api/v1/notifications (new)      ← NEW
              └── Web UI (server.go, extended)     ← MODIFIED
```

---

## Layer Responsibilities

### Domain (`internal/domain/`)

- `schedule.go`: `ScheduleMode`, `RecurrenceFrequency`, `RecurrenceRule`, `Schedule` value objects. Pure logic — no I/O.
  - `RecurrenceRule.NextAfter(t time.Time) time.Time` — core next-run calculation
  - `Schedule.NextRunAt(now time.Time) *time.Time` — delegates to rule or returns RunAt
- `notification.go`: `Notification`, `DiscussEntry`, `NotificationStatus`, `NotificationStore` interface, `NotificationFilter`
- `orchestrator.go` (modified): `Task` gains `Schedule Schedule` and `NextRunAt *time.Time` fields

### Application (`internal/application/`)

- `scheduling/scheduler_service.go`:
  - `Tick(now time.Time)` — queries tasks with `next_run_at <= now`, enqueues them, recalculates next run for recurring tasks
  - `RecalcNextRun(task Task) Task` — updates `NextRunAt` from `Schedule`
  - `CatchUpMissedRuns(now time.Time)` — called on startup; finds tasks whose `next_run_at` is in the past and enqueues them (deduplicating multiple missed occurrences of the same recurring task)
- `notifications/notification_service.go`:
  - `RaiseNotification(botName, taskID, message, contextSummary)` — creates and persists notification
  - `ListNotifications(filter)` — delegates to store
  - `UnreadCount()` — for badge
  - `AppendDiscuss(id, author, message)` — appends to thread
  - `ActionNotification(id)` — marks actioned, sets `actioned_at`
  - `RequeueTask(notificationID)` — appends discuss context to task, re-queues
  - `DeleteNotifications(ids)` — bulk delete

### Infrastructure (`internal/infrastructure/`)

- `db/` — migration + `TaskRepo` extended + new `NotificationRepo`
- `http/server.go` — UI + API (see Data Flow)

---

## Data Flow

### Scheduling loop (steady state)
```
SchedulerLoop goroutine
  │  every 10s
  ▼
SchedulerService.Tick(now)
  │  SELECT tasks WHERE next_run_at <= now AND status != running
  ▼
For each due task:
  ├── Enqueue task for execution (existing worker machinery)
  └── If recurring: RecalcNextRun → UPDATE tasks SET next_run_at = ...
```

### Scheduling loop (startup catch-up)
```
OrchestratorServer.Start()
  └── SchedulerService.CatchUpMissedRuns(now)
        │  SELECT tasks WHERE next_run_at < now AND status != running
        ▼
        For each missed recurring task: collapse to one run, recalc next
        For each missed future task: run once
```

### Agent raises notification
```
Worker goroutine (any bot)
  └── NotificationService.RaiseNotification(...)
        └── NotificationStore.Save(notification)

UI polls GET /api/v1/notifications/count  (badge)
UI polls GET /api/v1/notifications         (list)
```

### User Discusses
```
User submits message in Discuss panel
  └── POST /api/v1/notifications/:id/discuss  {message}
        └── NotificationService.AppendDiscuss(id, "user", message)

User clicks Re-queue
  └── POST /api/v1/notifications/:id/requeue
        └── NotificationService.RequeueTask(id)
              ├── Append discuss thread to task context
              └── Set task status → queued
```

---

## Sequence Diagrams

### Creating a Recurring Task
```
User → UI: Fill Add Task dialog, select Recurring, set rule
UI → POST /api/v1/bots/:bot/tasks  {schedule: {mode: "recurring", recurrence: {...}}}
API → TaskService.CreateTask(...)
  TaskService → Schedule.Validate()
  TaskService → Schedule.NextRunAt(now)  → stores next_run_at
  TaskService → TaskRepo.Save(task)
API → UI: 201 Created
```

### Scheduling Loop Tick
```
SchedulerLoop → SchedulerService.Tick(now)
  SchedulerService → TaskRepo.ListDueTasks(now)
  for each task:
    SchedulerService → WorkerQueue.Enqueue(task)
    if recurring:
      SchedulerService → RecurrenceRule.NextAfter(now) → UPDATE next_run_at
```

---

## Integration Points

| System | Integration | Notes |
|---|---|---|
| Existing worker queue | `SchedulerService` enqueues tasks | Uses existing enqueue path; no new protocol |
| Task DB repo | Extended with scheduling columns | Migration adds columns with safe defaults |
| HTTP API | New notification endpoints; extended task endpoints | Backwards-compatible |
| Web UI | Add Task dialog extended; Notifications tab added | All in `server.go` |
| Orchestrator Chat handler | Extended to parse task-management intent | New intent classifier in Chat path |

---

## Architectural Decisions

### ADR-001: In-process scheduler loop (not a separate service)
**Decision:** Run the scheduling loop as a goroutine inside the existing Orchestrator process.
**Rationale:** Consistent with the no-AWS-required design principle; simpler operational model; the 30-second SLA does not require distributed coordination.
**Consequence:** Scheduling only runs when the Orchestrator is up. Catch-up on restart handles gaps.

### ADR-002: RecurrenceRule stored as JSON in DB
**Decision:** Persist `RecurrenceRule` as a JSON blob in a `recurrence_rule` column.
**Rationale:** Avoids a separate `recurrence_rules` table join on every task query; rule is always read with its task.
**Consequence:** Cannot SQL-query individual rule fields directly; acceptable since all rule logic is in the domain layer.

### ADR-003: Natural language parsing via model at task-creation time
**Decision:** When the user enters free-text recurrence, the Orchestrator invokes the model to parse it into a `RecurrenceRule`, then presents a confirmation preview.
**Rationale:** Richer parsing than regex; leverages existing model infrastructure; confirmation prevents silent mis-parses.
**Consequence:** Task creation with NL input requires a model round-trip; acceptable UX cost given the preview.
