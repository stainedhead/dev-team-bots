# Data Dictionary: Task Scheduling and Notifications

**Feature:** Task Scheduling and Notifications
**Created:** 2026-05-11

---

## Purpose

This document defines all domain entities, value objects, interfaces, enumerations, and API types introduced or modified by this feature.

---

## Enumerations

### `ScheduleMode`
```go
type ScheduleMode string

const (
    ScheduleModeASAP      ScheduleMode = "asap"
    ScheduleModeFuture    ScheduleMode = "future"
    ScheduleModeRecurring ScheduleMode = "recurring"
)
```

### `RecurrenceFrequency`
```go
type RecurrenceFrequency string

const (
    RecurrenceFrequencyDaily   RecurrenceFrequency = "daily"
    RecurrenceFrequencyWeekly  RecurrenceFrequency = "weekly"
    RecurrenceFrequencyMonthly RecurrenceFrequency = "monthly"
)
```

---

## Value Objects

### `RecurrenceRule`
Encodes when a recurring task fires.

| Field | Type | Description |
|---|---|---|
| `Frequency` | `RecurrenceFrequency` | Daily, Weekly, or Monthly |
| `DaysMask` | `uint8` | Bitmask of weekdays (bit 0 = Sunday … bit 6 = Saturday) |
| `TimeOfDay` | `time.Duration` | Offset from midnight (e.g. 9h = 09:00) |
| `MonthDay` | `int` | Day of month (1–31) for Monthly frequency; 0 = not set |

Methods:
- `NextAfter(t time.Time) time.Time` — returns the next fire time strictly after `t`
- `Validate() error` — checks internal consistency

### `Schedule`
Encodes all scheduling information for a task.

| Field | Type | Description |
|---|---|---|
| `Mode` | `ScheduleMode` | ASAP, Future, or Recurring |
| `RunAt` | `*time.Time` | Requested run time for Future mode; nil for ASAP/Recurring |
| `Rule` | `*RecurrenceRule` | Recurrence rule for Recurring mode; nil for ASAP/Future |

Methods:
- `NextRunAt(now time.Time) *time.Time` — returns next scheduled execution time
- `Validate() error`

---

## Entities

### `Task` (extended)
Existing entity in `internal/domain/orchestrator.go` extended with:

| Field | Type | Description |
|---|---|---|
| `Schedule` | `Schedule` | Scheduling mode and rule |
| `NextRunAt` | `*time.Time` | Pre-computed next execution time; updated on save/edit |

### `Notification`
New entity representing an agent-raised notification.

| Field | Type | Description |
|---|---|---|
| `ID` | `string` | UUID |
| `BotName` | `string` | Name of the bot that raised the notification |
| `TaskID` | `*string` | Reference to originating task (nullable) |
| `WorkItemID` | `*string` | Reference to originating work item (nullable) |
| `Message` | `string` | Human-readable notification message |
| `ContextSummary` | `string` | Agent-provided context summary |
| `Status` | `NotificationStatus` | `unread`, `read`, `actioned` |
| `DiscussThread` | `[]DiscussEntry` | Ordered list of discuss messages |
| `CreatedAt` | `time.Time` | When the notification was raised |
| `ActionedAt` | `*time.Time` | When the user actioned it (nullable) |

### `DiscussEntry`
| Field | Type | Description |
|---|---|---|
| `Author` | `string` | `"user"` or bot name |
| `Message` | `string` | Message content |
| `Timestamp` | `time.Time` | |

---

## Interfaces

### `NotificationStore`
```go
type NotificationStore interface {
    Save(ctx context.Context, n Notification) error
    Get(ctx context.Context, id string) (Notification, error)
    List(ctx context.Context, filter NotificationFilter) ([]Notification, error)
    UnreadCount(ctx context.Context) (int, error)
    AppendDiscuss(ctx context.Context, id string, entry DiscussEntry) error
    MarkActioned(ctx context.Context, id string) error
    Delete(ctx context.Context, ids []string) error
}
```

### `SchedulerLoop` (addition to `ControlPlane`)
```go
type SchedulerLoop interface {
    Start(ctx context.Context) error
    Stop()
}
```

---

## API Types

### Task `schedule` object (new field on task create/update request)
```json
{
  "schedule": {
    "mode": "asap" | "future" | "recurring",
    "run_at": "<ISO8601 datetime>",       // future mode only
    "recurrence": {
      "frequency": "daily" | "weekly" | "monthly",
      "days": ["monday", "wednesday"],    // weekly mode
      "time": "09:00",                    // HH:MM, 24h
      "month_day": 1                      // monthly mode
    }
  }
}
```

### Notification response object
```json
{
  "id": "uuid",
  "bot_name": "maintainer",
  "task_id": "uuid",
  "work_item_id": null,
  "message": "...",
  "context_summary": "...",
  "status": "unread",
  "discuss_thread": [
    { "author": "maintainer", "message": "...", "timestamp": "..." }
  ],
  "created_at": "...",
  "actioned_at": null
}
```

---

## Database Schema

### `tasks` table additions
| Column | Type | Default | Notes |
|---|---|---|---|
| `schedule_mode` | `VARCHAR(16)` | `'asap'` | ASAP / future / recurring |
| `recurrence_rule` | `JSON` | `NULL` | Serialised `RecurrenceRule`; null for non-recurring |
| `next_run_at` | `DATETIME` | `NULL` | Pre-computed; null for ASAP |

### `notifications` table (new)
| Column | Type | Notes |
|---|---|---|
| `id` | `VARCHAR(36)` PK | UUID |
| `bot_name` | `VARCHAR(128)` | |
| `task_id` | `VARCHAR(36)` | Nullable FK → tasks.id |
| `work_item_id` | `VARCHAR(36)` | Nullable FK → board_items.id |
| `message` | `TEXT` | |
| `context_summary` | `TEXT` | |
| `status` | `VARCHAR(16)` | unread / read / actioned |
| `discuss_thread` | `JSON` | Array of DiscussEntry |
| `created_at` | `DATETIME` | |
| `actioned_at` | `DATETIME` | Nullable |
