# Tasks: Code Review Fixes — Task Scheduling and Notifications

**Created:** 2026-05-11
**Status:** Planning

---

## Progress Summary

0/16 tasks complete

---

## Phase 1 — P0 Fixes

### P1.1 — FR-001: processTask re-fetch after RunNow
- **ID:** P1.1
- **Dependencies:** none
- **Acceptance:** `store.Get(id).Status == running` after processTask on recurring task; no double-dispatch
- **Files:** `scheduler_service.go`, `scheduler_service_test.go`

### P1.2 — FR-002: RunNow dispatching guard
- **ID:** P1.2
- **Dependencies:** none
- **Acceptance:** `RunNow` returns early for `dispatching` status; test under `-race`
- **Files:** `task_dispatcher.go`, `task_dispatcher_test.go`

---

## Phase 2 — P1 Fixes

### P2.1 — FR-003: RecurrenceRule.Validate frequency check
- **ID:** P2.1
- **Dependencies:** none
- **Acceptance:** `Validate()` errors for unknown/empty frequency; HTTP → 400

### P2.2 — FR-004: HH:MM range validation
- **ID:** P2.2
- **Dependencies:** P2.1 (shares Validate path)
- **Acceptance:** `"25:00"` and `"9:99"` → 400; `"23:59"` → 200

### P2.3 — FR-005: Config.Notifications interface
- **ID:** P2.3
- **Dependencies:** none
- **Acceptance:** `Config.Notifications` is interface; stub usable in HTTP tests

### P2.4 — FR-006: ScheduledTaskDispatcher consolidated
- **ID:** P2.4
- **Dependencies:** none
- **Acceptance:** Single definition in domain; compile-time assertion

### P2.5 — FR-007: Remove dispatchAt goroutine
- **ID:** P2.5
- **Dependencies:** none
- **Acceptance:** No goroutine spawned for future tasks; future tasks stored as ScheduleModeFuture

### P2.6 — FR-008: AppendDiscuss TOCTOU fix
- **ID:** P2.6
- **Dependencies:** none
- **Acceptance:** Concurrent test with 50 parallel calls passes under `-race`; no entries lost

---

## Phase 3 — P2 Fixes

### P3.1 — FR-009: persist() logging
### P3.2 — FR-010: pendingMap TTL
### P3.3 — FR-011: Author from JWT claims
### P3.4 — FR-012: processAllDue helper
### P3.5 — FR-013: Remove unused digits
### P3.6 — FR-014: MonthDay > 31 validation
### P3.7 — FR-015: Table-driven parse tests
### P3.8 — FR-016: Empty IDs → 400
