# Dev-Flow Process Analysis Report

**Feature:** Task Scheduling and Notifications
**Branch:** feat/repeative-tasks-scheduling
**PRD:** specs/archive/260511-task-scheduling-and-notifications/task-scheduling-and-notifications-PRD.md
**Review PRD:** specs/archive/260511-task-scheduling-and-notifications-auto-review/task-scheduling-and-notifications-auto-review-PRD.md
**Generated:** 2026-05-11

---

## Summary

14-step dev-flow run implementing task scheduling (ASAP/Future/Recurring), in-app agent notifications, a scheduling loop, and a chat-driven task manager. The implementation covered 7 modules (M1–M7) across domain, application, infrastructure, HTTP API, and UI layers, plus a quality pass (M8) and 16 code review fixes.

---

## Step Timings

| Step | Name | Status | Runtime |
|------|------|--------|---------|
| 1  | Create Spec from PRD            | ✅ Complete | 15 min |
| 2  | Review Spec                     | ✅ Complete | 15 min |
| 3  | Implement Product               | ✅ Complete | ~510 min (multi-session) |
| 4  | Documentation and User Docs     | ✅ Complete | 30 min |
| 5  | Code and Design Review          | ✅ Complete | 60 min |
| 6  | Prepare Review PRD              | ✅ Complete | 15 min |
| 7  | Archive Original Spec           | ✅ Complete | 15 min |
| 8  | Spec Review Fixes               | ✅ Complete | 15 min |
| 9  | Implement Review Fixes          | ✅ Complete | 165 min |
| 10 | Archive Fixes Spec              | ✅ Complete | 5 min |
| 11 | Final Quality Pass              | ✅ Complete | 10 min |
| 12 | Process Analysis Report         | ✅ Complete | — |
| 13 | Archive Spec                    | ✅ Complete | (done at Step 7) |
| 14 | Open Pull Request               | ⬜ Pending | — |

---

## Commit Log (this branch)

| Commit | Message |
|--------|---------|
| 734b3e6 | chore(spec): create spec for task-scheduling-and-notifications |
| 8ec70ce | chore(spec): resolve review findings |
| 9a7b85b | feat(domain): add scheduling and agent notification domain types [M1] |
| 8ab335b | feat(scheduling): add SchedulerService with tick and catch-up [M2] |
| aa96bf9 | feat(notifications): add NotificationService [M3] |
| 9c5e5f7 | feat(infra): add AgentNotificationStore and wire scheduler loop [M4] |
| ed3b53b | feat(api): add notification endpoints and scheduling fields on task create [M5] |
| 5ab27a3 | feat(ui): add scheduling mode to task dialog and Notifications tab [M6] |
| 2716500 | feat(chat): add ChatTaskManager for intent detection and confirmation gate [M7] |
| c3852b7 | test: M8 quality pass — close coverage gaps and fix flaky timing test |
| bdf4d01 | docs: update product and technical docs |
| 2c2c117 | review: code and design review (16 findings) |
| fb3d34a | chore: create review-fixes spec |
| 5de0352 | chore: archive original spec |
| 3323a10 | fix(scheduling): P0 — prevent stale status overwrite and double-dispatch |
| 86a9aec | fix(scheduling): P1 — validate frequencies/times, consolidate interfaces, fix race |
| 67dc3c1 | fix(scheduling): P2 — quality improvements |
| 7ae3d16 | chore(spec): mark all 16 review findings complete |
| 8badb6b | chore: remove stale auto-review PRD |
| 076a74d | chore: archive review-fixes spec; advance to final quality pass |
| 35b4128 | chore: Step 11 final quality pass — all tests pass, lint clean |

---

## Implementation Milestones

### M1 — Domain Layer
New types: `Schedule`, `RecurrenceRule`, `ScheduleMode`, `AgentNotification`, `AgentNotificationStore`. Extended `DirectTask` with `Schedule`, `NextRunAt`, `DirectTaskStatusDispatching`, `ListDue`, `ClaimDue`. Test double: `InMemoryAgentNotificationStore`.

### M2 — Scheduling Application Layer
`SchedulerService`: `Tick` (10s cadence), `CatchUpMissedRuns` (startup), `StartLoop`. Atomic `ClaimDue` (mutex-guarded pending→dispatching) guards against concurrent double-dispatch.

### M3 — Notification Application Layer
`NotificationService`: `RaiseNotification`, `AppendDiscuss` (100-entry cap, unread→read), `RequeueTask` (prepends discuss context, resets to ASAP), `ActionNotification`, `Delete`.

### M4 — Infrastructure
`InMemoryAgentNotificationStore` (file-backed JSON), `ListDue`/`ClaimDue` on `InMemoryDirectTaskStore`, `DispatchWithSchedule` on `LocalTaskDispatcher`.

### M5 — HTTP API (5 new endpoints)
`GET /api/v1/notifications`, `GET /api/v1/notifications/count`, `POST /api/v1/notifications/:id/discuss`, `POST /api/v1/notifications/:id/requeue`, `DELETE /api/v1/notifications`. Extended task create with `schedule` JSON object.

### M6 — UI
Notifications tab in kanban board. Schedule builder in Assign Task dialog (mode selector, day checkboxes, time picker, NL input). Task card schedule display.

### M7 — ChatTaskManager
Keyword-based intent detection (action + time + bot = 2+ signals). Confirmation flow with per-thread pending map (10-minute TTL). `ParseScheduleNL` converts natural language to `RecurrenceRule`.

### M8 — Quality Pass
- notifications: 94.6% coverage (from 83.9%)
- scheduling: 91.5% (from 87.2%)
- orchestrator: 95.3% (from 89.0%)
- Fixed flaky `TestInMemoryChatStore_ListThreads_ReturnsSortedByUpdatedAt` timing bug

---

## Code Review Findings (16 total)

| Priority | Count | Key Issues |
|----------|-------|-----------|
| P0 | 2 | Stale status overwrite causing double-dispatch; RunNow dispatching guard |
| P1 | 6 | Frequency/time validation; interface duplication; goroutine leak; TOCTOU race; concrete type in Config |
| P2 | 8 | Silent persist errors; pendingMap TTL; hardcoded author; dead code; missing validation/tests |

All 16 findings resolved before PR.

---

## Key Design Decisions

1. **File-backed JSON store for notifications** — follows the existing `InMemoryDirectTaskStore` pattern. No SQL migration, no new dependencies.
2. **`ClaimDue` atomic mutex guard** — uses Go's `sync.RWMutex` for the pending→dispatching transition rather than a DB `UPDATE … WHERE status='pending'`. Safe for single-process deployment.
3. **`ScheduledTaskDispatcher` in domain layer** — consolidated from two duplicate local interface definitions to a single `domain.ScheduledTaskDispatcher`.
4. **`ChatTaskManager` keyword scoring** — two of three signals (action, time, bot name) required to detect intent. No model calls. Pending intents expire after 10 minutes.
5. **`processTask` re-fetch after `RunNow`** — re-fetches the fresh record (with `Status = running`) from the store before writing `NextRunAt`, preserving the running status.

---

## What Worked Well

- Progressive spec documentation kept design intent clear through implementation
- Domain-first TDD caught the TOCTOU and double-dispatch issues early (via review)
- Per-module milestones kept the feature coherent with clear boundaries
- The review PRD's granularity (per-function with TDD guidance) made Step 9 mechanical

## What to Improve Next Time

- The `application/team` wiring is hard to unit test (77% coverage) — constructor injection would help
- Spec's initial architecture assumed MariaDB; resolving this in research.md earlier would have saved spec iteration
- The `dispatchAt` goroutine leak was a pre-existing issue caught by review, not implementation — earlier audit of the legacy path would be more efficient
