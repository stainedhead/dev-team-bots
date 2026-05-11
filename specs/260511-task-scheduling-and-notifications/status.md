# Status: Task Scheduling and Notifications

**Feature:** Task Scheduling and Notifications
**Created:** 2026-05-11
**Branch:** feat/repeative-tasks-scheduling

---

## Overall Progress

| Phase | Name | Status | Notes |
|---|---|---|---|
| 0 | Initial Research & Spec Creation | ✅ Complete | |
| 1 | Specification | ✅ Complete | |
| 2 | Research & Data Modeling | ✅ Complete | |
| 3 | Architecture & Planning | ✅ Complete | |
| 4 | Task Breakdown | ✅ Complete | |
| 5 | Implementation | 🔄 In Progress | M8 quality pass complete; docs pending |
| 6 | Completion & Archival | ⬜ Not Started | |

---

## Phase 5 Tasks

### M1 — Domain Layer
- [x] `domain/schedule.go` — Schedule, RecurrenceRule, ScheduleMode types
- [x] `domain/agent_notification.go` — AgentNotification, AgentNotificationStore interface
- [x] `domain/direct_task.go` extended — Schedule, NextRunAt, DirectTaskStatusDispatching, ListDue, ClaimDue
- [x] `domain/mocks/mock_agent_notification_store.go` — InMemoryAgentNotificationStore test double
- [x] Domain tests — schedule, agent_notification, direct_task

### M2 — Scheduling Application Layer
- [x] `application/scheduling/scheduler_service.go` — Tick, CatchUpMissedRuns, StartLoop
- [x] `application/scheduling/scheduler_service_test.go` — ≥90% coverage

### M3 — Notification Application Layer
- [x] `application/notifications/notification_service.go` — RaiseNotification, AppendDiscuss, RequeueTask, etc.
- [x] `application/notifications/notification_service_test.go` — ≥90% coverage

### M4 — Infrastructure
- [x] `infrastructure/local/orchestrator/agent_notification_store.go` — file-backed InMemoryAgentNotificationStore
- [x] `infrastructure/local/orchestrator/direct_task_store.go` — ListDue, ClaimDue added
- [x] `infrastructure/local/orchestrator/task_dispatcher.go` — DispatchWithSchedule added

### M5 — HTTP API
- [x] `infrastructure/http/notifications.go` — 5 notification endpoints + schedule parsing
- [x] `infrastructure/http/server.go` — routes registered, Config extended

### M6 — UI
- [x] Notifications tab in kanban board (list, detail, discuss panel)
- [x] Task scheduling UI — recurrence builder, NL input, schedule display

### M7 — ChatTaskManager
- [x] `application/orchestrator/chat_task_manager.go` — intent detection, confirmation flow, ParseScheduleNL
- [x] `application/team/team_manager.go` — SchedulerService.StartLoop goroutine, ChatTaskManager wired

### M8 — Final Quality Pass
- [x] notifications package — 94.6% coverage (was 83.9%)
- [x] scheduling package — 91.5% coverage (was 87.2%)
- [x] orchestrator package — 95.3% coverage (was 89.0%)
- [x] golangci-lint — 0 issues
- [x] go test -race ./... — all pass
- [x] Fixed flaky chat_store_test.go timing bug

---

## Blockers

None.

---

## Recent Activity

- 2026-05-11: Spec directory created from PRD; all phase files initialized
- 2026-05-11: Spec review complete; added 3 missing ACs (FR-005, FR-012, FR-016), 5 edge cases, 2 open questions
- 2026-05-11: M1–M7 implementation complete
- 2026-05-11: M8 quality pass complete — all target packages ≥90%, lint clean, all tests pass
