# Tasks: Tech-Lead Dynamic Subteam and Orchestrator Pool Management

**Feature:** tech-lead-dynamic-subteam
**Created:** 2026-05-07
**Status:** Planning — populate during Phase 4

---

## Progress Summary

**0 / 0 tasks complete** (tasks to be broken down in Phase 4)

---

## Phase Structure (stub — expand in Phase 4)

### Phase 5a — Domain & Interfaces

| ID | Task | Dependencies | Est (h) | Status | Acceptance Criteria |
|----|------|-------------|---------|--------|---------------------|
| P5a.1 | Define `SubTeamManager` interface and `SpawnedAgent` entity | — | [TBD] | Not Started | Interface compiles; entity fields match data-dictionary.md |
| P5a.2 | Define `TechLeadPool` interface and `PoolEntry` entity | — | [TBD] | Not Started | Interface compiles; entity fields match data-dictionary.md |
| P5a.3 | Define `AgentStatus` and `PoolEntryStatus` enumerations | P5a.1, P5a.2 | [TBD] | Not Started | Enums defined; all values in data-dictionary.md present |

### Phase 5b — Infrastructure: ScopedBus

| ID | Task | Dependencies | Est (h) | Status | Acceptance Criteria |
|----|------|-------------|---------|--------|---------------------|
| P5b.1 | Implement `NewScopedBus()` constructor | P5a.1 | [TBD] | Not Started | Returns an isolated bus instance; two instances do not share events |
| P5b.2 | Write isolation tests for ScopedBus | P5b.1 | [TBD] | Not Started | Test proves no message leakage between two concurrent scoped buses |

### Phase 5c — Infrastructure: Persistence

| ID | Task | Dependencies | Est (h) | Status | Acceptance Criteria |
|----|------|-------------|---------|--------|---------------------|
| P5c.1 | Implement `SessionFile` (read/write/remove session records) | P5a.1 | [TBD] | Not Started | Session records survive process restart; stale records detectable |
| P5c.2 | Implement `PoolStateFile` with atomic writes | P5a.2 | [TBD] | Not Started | Simulated kill mid-write leaves previous valid state intact |
| P5c.3 | Write tests for session file and pool state file | P5c.1, P5c.2 | [TBD] | Not Started | All read/write/atomic scenarios covered |

### Phase 5d — Application: Tech-lead use cases

| ID | Task | Dependencies | Est (h) | Status | Acceptance Criteria |
|----|------|-------------|---------|--------|---------------------|
| P5d.1 | Implement `SpawnAgent` use case | P5a.1, P5b.1, P5c.1 | [TBD] | Not Started | Bot goroutine ready within 500ms; session record written |
| P5d.2 | Implement `TerminateAgent` use case | P5d.1 | [TBD] | Not Started | Bot finishes current work unit, goroutine stops cleanly, session record removed |
| P5d.3 | Implement `HeartbeatMonitor` (send heartbeats from tech-lead) | P5b.1 | [TBD] | Not Started | Heartbeat sent at 30s interval; spawned bots receive it |
| P5d.4 | Implement heartbeat watchdog in spawned bot (self-shutdown on timeout) | P5d.3 | [TBD] | Not Started | Bot self-terminates after 90s without heartbeat; checkpoints state first |
| P5d.5 | Implement startup reconnect from session file | P5c.1, P5b.1 | [TBD] | Not Started | Restarted tech-lead reconnects to still-running bots without re-spawning |
| P5d.6 | Implement `TearDownAll` on context end | P5d.2 | [TBD] | Not Started | All spawned goroutines stopped; confirmed by goroutine count |
| P5d.7 | Write tests for all tech-lead use cases | P5d.1–P5d.6 | [TBD] | Not Started | ≥90% coverage; panic recovery test; invalid type test |

### Phase 5e — Application: Orchestrator use cases

| ID | Task | Dependencies | Est (h) | Status | Acceptance Criteria |
|----|------|-------------|---------|--------|---------------------|
| P5e.1 | Implement `AllocateTechLead` use case | P5a.2, P5b.1, P5c.2 | [TBD] | Not Started | Idle instance allocated without spawn; new instance spawned when pool empty; within 1s |
| P5e.2 | Implement `DeallocateTechLead` use case | P5e.1 | [TBD] | Not Started | Tech-lead shuts down cleanly; last instance transitions to idle, not removed |
| P5e.3 | Implement `ReconcilePool` (startup reconciliation) | P5c.2, P5b.1 | [TBD] | Not Started | Surviving tech-leads reconnected; dead entries pruned |
| P5e.4 | Implement serialised pool allocation (mutex/channel guard) | P5e.1 | [TBD] | Not Started | Near-simultaneous In Progress transitions do not produce duplicate spawns |
| P5e.5 | Implement heartbeat crash detection → mark item blocked | P5d.3 | [TBD] | Not Started | Tech-lead crash detected via heartbeat timeout; item marked `blocked`; event logged |
| P5e.6 | Write tests for all orchestrator use cases | P5e.1–P5e.5 | [TBD] | Not Started | ≥90% coverage; race condition tests; crash detection test |

### Phase 5f — Wire-up & Integration

| ID | Task | Dependencies | Est (h) | Status | Acceptance Criteria |
|----|------|-------------|---------|--------|---------------------|
| P5f.1 | Register `spawn_agent` and `terminate_agent` tools in tech-lead bot | P5d.1, P5d.2 | [TBD] | Not Started | Tools callable from tech-lead context |
| P5f.2 | Extend queue router with per-session routing tables | P5b.1 | [TBD] | Not Started | Existing main-team routing unaffected; private bus messages do not leak |
| P5f.3 | Wire board watcher to emit status-change events | P5e.1 | [TBD] | Not Started | Item In Progress transition triggers AllocateTechLead within 1s |
| P5f.4 | Add `/api/v1/pool` REST endpoint | P5e.3 | [TBD] | Not Started | Endpoint returns current pool state with all instances, statuses, item IDs |
| P5f.5 | Extend team roster entries with `status` and `item_id` fields | P5e.1 | [TBD] | Not Started | Roster reflects all active tech-lead instances with correct metadata |
