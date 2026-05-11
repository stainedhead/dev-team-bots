# Feature Spec: Task Scheduling and Notifications

**Created:** 2026-05-11
**Status:** Draft
**Source PRD:** `specs/260511-task-scheduling-and-notifications/task-scheduling-and-notifications-PRD.md`

---

## Executive Summary

This feature delivers three tightly coupled capabilities that together reduce user friction and expand autonomous agent operation:

1. **Expressive task scheduling** — tasks can be ASAP, Future (one-time at a specified time), or Recurring (daily/weekly/monthly with day-level inclusion/exclusion or natural language input). The Orchestrator manages a scheduling loop that fires within 30 seconds and catches up missed runs on restart.
2. **Agent notifications** — agents can surface blockers or decisions to users via a durable notification system. A new Notifications tab (with unread badge) provides a list and detail view; the Discuss panel lets users respond in context, re-queuing the task with enriched information.
3. **Chat-driven task management** — users can instruct the Orchestrator bot via Chat to create, edit, and manage tasks (including recurring ones); the Orchestrator requires explicit human confirmation before persisting any change.

---

## Problem Statement

Operators can currently only schedule a task to run once — either immediately or at a single future time. There is no way to express recurring work, forcing manual re-entry. The Orchestrator has no recurrence awareness and cannot proactively queue upcoming runs. Additionally, when an agent encounters a blocker requiring human input, there is no structured channel to surface that — work stalls silently.

---

## Goals

- Reduce user friction by eliminating manual re-entry of repetitive tasks
- Enable more fully autonomous agent operation for lower-value, trusted, repeating work
- Allow users to focus on high-value decisions by surfacing agent blockers as structured, actionable notifications

## Non-Goals

- External calendar sync (Google Calendar, Outlook, etc.)
- Agent-initiated recurring task creation without human involvement
- Cross-bot recurring task coordination beyond the subagent thread model described here

---

## User Requirements

### Functional Requirements

**FR-001:** The Add Task dialog presents three scheduling modes: **ASAP**, **Future**, and **Recurring**.

**FR-002:** ASAP mode queues the task for the next available execution slot with no date/time input required.

**FR-003:** Future mode accepts a date and time; the task runs at the next available slot on or after that time.

**FR-004:** Recurring mode presents a visual recurrence builder with frequency options (Daily, Weekly, Monthly) and day-level inclusion/exclusion (e.g. Monday and Wednesday only).

**FR-005:** Recurring mode also accepts a plain-text natural language input as an alternative to the visual builder (e.g. "every Monday and Wednesday at 9am"); the Orchestrator parses this into a structured recurrence rule and presents a confirmation preview before saving.

**FR-006:** Terminology is consistent across all UI surfaces and the codebase: **ASAP**, **Future**, **Recurring** — not "immediate", "scheduled", "repeating", or other variants.

**FR-007:** The Task detail screen reflects the scheduling mode. For Future and Recurring tasks, the displayed date is the **next scheduled run time**, not a static creation or last-run date.

**FR-008:** On save or edit of any task with a Future or Recurring schedule, the Orchestrator immediately recalculates and persists the next scheduled run time.

**FR-009:** The Orchestrator runs a scheduling loop on a cadence that ensures no scheduled run is delayed more than **30 seconds** beyond its scheduled time.

**FR-010:** Agents can raise a notification to the user, attaching a message, a context summary, and a reference to the originating task or work item.

**FR-011:** A **Notifications** tab is added to the main navigation bar with a badge showing the count of unread/unactioned notifications.

**FR-012:** The Notifications list screen mirrors the Tasks list layout and includes: All / Single / Recurring filters, a bot filter, a search field, Refresh, and Delete Selected.

**FR-013:** Clicking a notification opens a detail screen showing the notification message, agent output context, and a **Discuss** panel (in place of "Ask") where the user can respond to the agent.

**FR-014:** Responses entered in Discuss are appended to the originating task's context; the task becomes eligible for re-queuing so the agent can continue with the enriched information.

**FR-015:** The Orchestrator bot can create, edit, and manage tasks when instructed via natural language in Chat (e.g. "schedule a weekly code review every Monday at 9am"). The Orchestrator must request explicit human confirmation before saving any task creation or modification.

**FR-016:** Bot-to-bot collaboration is supported via a subagent thread: the requesting bot shares relevant task context with the assisting bot, and the assisting bot's response is returned into the requester's active task context.

---

## Non-Functional Requirements

- **Performance:** Scheduling loop fires within **30 seconds** of any task's scheduled time.
- **Reliability:** Notifications are durably persisted; not lost if UI is offline. On Orchestrator restart, missed runs execute immediately (multiple missed occurrences of the same recurring task collapse into a single catch-up run).
- **Security:** Only `human` and `orchestrator` roles may create or modify tasks. The orchestrator bot requires explicit human approval before any task creation or edit takes effect. Attempts by other bots to create tasks directly are rejected.
- **Observability:** Scheduling loop execution, missed-run detection, and notification delivery events are logged and visible in Orchestrator telemetry.
- **Consistency:** The terms ASAP, Future, and Recurring are used consistently in UI labels, API field names, DB column names, and log messages.

---

## System Architecture

### Affected Layers

**Domain layer (`internal/domain/`)**
- New: `schedule.go` — `Schedule` value object (mode: ASAP/Future/Recurring), `RecurrenceRule` (frequency, days mask, time-of-day), `NextRun` calculation logic
- New: `notification.go` — `Notification` entity, `NotificationStore` interface
- Modified: `orchestrator.go` — extend `Task` with `Schedule`, `NextRunAt`, `RecurrenceRule`; add `SchedulerLoop` interface

**Application layer (`internal/application/`)**
- New: `scheduling/` — `SchedulerService` use case: loop tick, missed-run catch-up, next-run recalculation
- New: `notifications/` — `NotifyUser`, `AcknowledgeNotification`, `AppendDiscussContext`, `RequeueTask` use cases
- Modified: `orchestrator/` — extend task creation/update use cases to validate scheduling mode and persist `NextRunAt`; add Chat-driven task management flow with approval gate

**Infrastructure layer (`internal/infrastructure/`)**
- New: `http/` — Notifications API endpoints (`GET /api/v1/notifications`, `POST /api/v1/notifications/:id/discuss`, `DELETE`), scheduling fields on task endpoints
- Modified: `db/` — schema migration: add `schedule_mode`, `recurrence_rule`, `next_run_at` to `tasks` table; new `notifications` table
- Modified: `http/server.go` — Add Task dialog (scheduling mode UI), Notifications tab + badge, Notifications list + detail + Discuss panel, Task detail next-run display

### New/Modified Components

| Component | Type | Description |
|---|---|---|
| `Schedule` | Domain value object | Encapsulates ASAP/Future/Recurring mode + rule |
| `RecurrenceRule` | Domain value object | Frequency, days bitmask, time-of-day |
| `Notification` | Domain entity | Agent-raised notification with context ref |
| `NotificationStore` | Domain interface | Persist/query notifications |
| `SchedulerService` | Application use case | Scheduling loop, catch-up, next-run calc |
| `NotificationService` | Application use case | Raise, ack, discuss, requeue |
| Scheduling loop goroutine | Infrastructure | Sub-minute ticker in orchestrator startup |
| DB migration | Infrastructure | tasks + notifications schema changes |
| Web UI — Add Task | Infrastructure | Scheduling mode selector + recurrence builder |
| Web UI — Notifications tab | Infrastructure | Tab, badge, list, detail, Discuss panel |

---

## Scope of Changes

### Files to Create
- `boabot/internal/domain/schedule.go`
- `boabot/internal/domain/notification.go`
- `boabot/internal/domain/mocks/mock_notification_store.go`
- `boabot/internal/application/scheduling/scheduler_service.go`
- `boabot/internal/application/scheduling/scheduler_service_test.go`
- `boabot/internal/application/notifications/notification_service.go`
- `boabot/internal/application/notifications/notification_service_test.go`
- `boabot/internal/infrastructure/db/migrations/YYYYMMDD_add_scheduling_and_notifications.sql`

### Files to Modify
- `boabot/internal/domain/orchestrator.go` — extend Task type
- `boabot/internal/application/orchestrator/` — task creation/update use cases
- `boabot/internal/infrastructure/db/` — task repo, new notification repo
- `boabot/internal/infrastructure/http/server.go` — UI + API

### Dependencies
- No new external packages anticipated; recurrence parsing uses standard library time arithmetic
- Natural language parsing (FR-005): may use a lightweight rules-based parser or delegate to the model at runtime

---

## Breaking Changes

| Area | Change | Migration |
|---|---|---|
| `tasks` DB table | New columns: `schedule_mode`, `recurrence_rule`, `next_run_at` | Non-breaking migration with defaults |
| Task API | `scheduled_at` field extended to `schedule` object | Backwards-compatible with ASAP default |
| Task domain type | `ScheduledAt *time.Time` → `Schedule Schedule` + `NextRunAt *time.Time` | Internal only; no external API break if API layer maps correctly |

---

## Success Criteria and Acceptance Criteria

### Quality Gates
- 90%+ coverage on `internal/domain/` and `internal/application/` packages
- `go vet`, `golangci-lint`, and `go test -race` all pass
- All acceptance criteria below verified manually against a running Orchestrator

### Acceptance Criteria
- [ ] A task can be created with ASAP, Future, or Recurring scheduling from the Add Task dialog
- [ ] A recurring task configured for Monday and Wednesday runs on those days and not on others
- [ ] The Task detail screen shows "Next run: \<datetime\>" for Future and Recurring tasks
- [ ] Editing a recurrence rule immediately recalculates and updates the next scheduled run time
- [ ] The Orchestrator scheduling loop fires within 30 seconds of a task's scheduled time
- [ ] A notification raised by an agent is persisted and visible in the Notifications tab after a UI reconnect
- [ ] The Notifications tab badge increments when a new notification arrives and clears when actioned
- [ ] A Discuss response on a notification is appended to the originating task's context and the task can be re-queued
- [ ] A user can instruct the Orchestrator via Chat to create a recurring task; the Orchestrator requests confirmation before saving
- [ ] Only users with the human or orchestrator role can create tasks; attempts by other bots are rejected
- [ ] If the Orchestrator restarts and one or more scheduled runs were missed, those tasks execute immediately on restart (collapsing multiple missed occurrences of the same task into a single catch-up run)

---

## Risks and Mitigation

| Risk | Likelihood | Mitigation |
|---|---|---|
| Natural language recurrence parsing produces incorrect schedules | Medium | Confirmation preview before save; fallback to visual builder |
| Orchestrator task approval UX in Chat is complex to get right | Medium | Prototype the confirmation flow early; iterate |
| DB migration on existing deployments with many tasks | Low | Non-breaking defaults; run migration in transaction |
| Scheduling loop contention with task execution goroutines | Low | Loop only enqueues; workers consume — no shared mutable state beyond DB |

---

## Timeline and Milestones

| Milestone | Description |
|---|---|
| M1 — Domain model | `Schedule`, `RecurrenceRule`, `Notification` types + tests |
| M2 — Scheduler service | Loop, catch-up, next-run calc + tests |
| M3 — Notification service | Raise, ack, discuss, requeue + tests |
| M4 — DB migration | Schema changes, repo implementations |
| M5 — API layer | New/modified endpoints |
| M6 — Web UI | Add Task dialog, Notifications tab, detail + Discuss |
| M7 — Chat-driven task management | Orchestrator parsing + approval flow |
| M8 — Integration + quality pass | Full test suite, lint, coverage |

---

## References

- Source PRD: `specs/260511-task-scheduling-and-notifications/task-scheduling-and-notifications-PRD.md`
