# Architecture: Code Review Fixes — Task Scheduling and Notifications

**Created:** 2026-05-11
**Status:** Draft

---

## Architecture Overview

These are correctness fixes and refactors within the existing architecture. No new layers or components are introduced.

---

## Component Architecture

[TBD — derived from scope of changes in spec.md]

---

## Layer Responsibilities

All changes stay within the boundaries set by the original implementation. The only structural changes are:

- `ScheduledTaskDispatcher` moves from two local definitions to one domain-layer definition (FR-006)
- `Config.Notifications` changes from concrete type to interface (FR-005)
- `dispatchAt` goroutine is removed; `Dispatch` stores future tasks in the scheduler's pending state (FR-007)

---

## Data Flow

FR-001 change to recurring task status flow:

```
ListDue → ClaimDue (dispatching) → RunNow (running) → [re-fetch from store] → Update(NextRunAt only, preserve running)
```

FR-008 change to AppendDiscuss flow:

```
HTTP POST /discuss → service.AppendDiscuss → store.AppendDiscuss (atomic, mutex-guarded)
                                           → store.UpdateStatus (if unread → read)
```

---

## Sequence Diagrams

[TBD]

---

## Integration Points

[TBD]

---

## Architectural Decisions

- FR-005: `NotificationService` interface kept in `http` package (local interface) rather than domain, since no other infrastructure adapter needs it.
- FR-006: `ScheduledTaskDispatcher` moved to domain layer since both application (`orchestrator`) and infrastructure (`http`) layers use it.
- FR-007: Legacy `dispatchAt` goroutine removed entirely; callers that previously used `Dispatch` with a future time should use `DispatchWithSchedule`.
