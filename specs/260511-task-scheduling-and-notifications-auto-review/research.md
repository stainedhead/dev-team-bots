# Research: Code Review Fixes — Task Scheduling and Notifications

**Created:** 2026-05-11
**Source PRD:** [task-scheduling-and-notifications-auto-review-PRD.md](task-scheduling-and-notifications-auto-review-PRD.md)

---

## Research Questions

1. **FR-001 — processTask re-fetch strategy:** What does `LocalTaskDispatcher.RunNow` return? Does it return the updated task (with `Status=running`) so we can use the return value directly, or must we call `store.Get` separately?

2. **FR-007 — Dispatch callers:** Are there any callers of `LocalTaskDispatcher.Dispatch` outside of `task_dispatcher.go` and the HTTP server that pass a non-nil `scheduledAt`? A full call-site audit is needed before removing `dispatchAt`.

3. **FR-008 — Store.AppendDiscuss atomic semantics:** `AgentNotificationStore.AppendDiscuss` exists in the interface and the in-memory store. Does it handle the 100-entry cap and status transition, or only the append? The cap and transition currently live in the service layer.

4. **FR-005 — Interface placement:** Should `NotificationService` interface go in `internal/infrastructure/http/` as a local interface (like `ScheduledTaskDispatcher` currently is), or in the domain layer? Check whether other infrastructure adapters will ever need to satisfy it.

5. **FR-006 — Domain import cycle risk:** Moving `ScheduledTaskDispatcher` to the domain layer requires that the domain package does not import infrastructure packages. Verify the import graph before making the change.

---

## Industry Standards

[TBD]

## Existing Implementations

[TBD — see current source files listed in spec.md Scope of Changes]

## API Documentation

[TBD]

## Best Practices

[TBD]

## Open Questions

- Whether `store.AppendDiscuss` should absorb the cap + status logic (making the fix simpler) or stay as a pure append (requiring additional store methods for cap and status).

## References

- Source PRD: `task-scheduling-and-notifications-auto-review-PRD.md`
- Original feature spec: `specs/archive/260511-task-scheduling-and-notifications/`
