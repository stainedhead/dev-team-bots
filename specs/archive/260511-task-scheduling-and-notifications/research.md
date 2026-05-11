# Research: Task Scheduling and Notifications

**Feature:** Task Scheduling and Notifications
**Created:** 2026-05-11
**Source PRD:** `specs/260511-task-scheduling-and-notifications/task-scheduling-and-notifications-PRD.md`

---

## Research Questions

1. **Recurrence rule representation** — What is the most suitable internal representation for recurrence rules? Options include iCalendar RRULE format, a custom bitmask struct, or a cron expression. What are the tradeoffs for each in terms of parsing, storage, and next-run calculation?

2. **Natural language parsing strategy (FR-005)** — Should the Orchestrator parse natural language recurrence descriptions using a rules-based approach (regex/keyword matching) or by delegating to the model at task-creation time? What are the failure modes, and how should ambiguous input be handled?

3. **Scheduling loop architecture** — How should the scheduling loop integrate with the existing `TeamManager` goroutine model? Should it be a dedicated goroutine, a ticker within the orchestrator service, or driven by the existing event bus? What is the safest approach for avoiding missed ticks under load?

4. **Notification persistence and delivery** — What schema and query pattern best supports durable notification storage with efficient unread-count queries? Should notifications be polled by the UI (like tasks) or pushed via SSE/WebSocket?

5. **Bot-to-bot subagent context sharing (FR-016)** — How does the existing worker/task context model need to extend to support injecting a parent task's context into a subagent thread? What isolation guarantees are needed to prevent context bleed between unrelated tasks?

---

## Industry Standards

[TBD — document relevant standards, e.g. iCalendar RRULE RFC 5545, cron expression syntax]

---

## Existing Implementations

[TBD — review existing scheduling and notification patterns in the codebase; review `internal/domain/orchestrator.go` Task type and existing `scheduled_at` field handling]

---

## API Documentation

[TBD — document current task API request/response shapes; define new `schedule` object shape]

---

## Best Practices

[TBD — scheduling loop reliability patterns; notification fan-out patterns; optimistic UI for badge counts]

---

## Open Questions

[TBD — populate during Phase 2 research]

---

## References

[TBD]
