# Architecture: Tech-Lead Dynamic Subteam and Orchestrator Pool Management

**Feature:** tech-lead-dynamic-subteam
**Created:** 2026-05-07
**Status:** Draft — populate during Phase 3

---

## Architecture Overview

[TBD — high-level description of how subteam spawning and pool management fit into the existing boabot architecture]

---

## Component Architecture

[TBD — component diagram or description showing:
- Tech-lead ↔ ScopedBus ↔ Spawned sub-agents
- Orchestrator ↔ TechLeadPool ↔ Tech-lead instances
- Relationship to existing TeamManager and main message bus]

---

## Layer Responsibilities

| Layer | Responsibility |
|-------|---------------|
| Domain | `SubTeamManager` and `TechLeadPool` interfaces; `SpawnedAgent` and `PoolEntry` entities; `AgentStatus` and `PoolEntryStatus` enums |
| Application | `SpawnAgent`, `TerminateAgent`, `HeartbeatMonitor` use cases (tech-lead); `AllocateTechLead`, `DeallocateTechLead`, `ReconcilePool` use cases (orchestrator) |
| Infrastructure | `ScopedBus` implementation; `SessionFile` persistence; `PoolStateFile` atomic persistence; queue router extension |

---

## Data Flow

[TBD — describe message flow for:
1. Tech-lead spawning a sub-agent and routing a task to it
2. Orchestrator allocating a tech-lead when an item moves to In Progress
3. Heartbeat flow and self-shutdown on timeout
4. Startup reconciliation for both session file and pool state file]

---

## Sequence Diagrams

[TBD — sequence diagrams for key flows:
- spawn_agent call → bot ready
- terminate_agent call → clean shutdown
- Heartbeat timeout → self-shutdown
- Item → In Progress → pool allocation
- Item → Done → tech-lead deallocation
- Orchestrator restart → pool reconciliation]

---

## Integration Points

| Integration | Description |
|-------------|-------------|
| `local/bus` | New `NewScopedBus()` constructor; each tech-lead session gets its own isolated bus instance |
| `local/queue` router | Extended to support per-session routing tables; existing main-team routing unaffected |
| `TeamManager` | Dynamic registration/deregistration of tech-lead pool instances |
| Orchestrator board watcher | Must emit internal events on item status transitions for low-latency pool allocation |
| `/api/v1` REST API | New `/api/v1/pool` endpoint; existing team roster entries extended with `status` and `item_id` fields |

---

## Architectural Decisions

[TBD — record decisions made during Phase 3, referencing the Decisions section of the PRD:
- Heartbeat cadence: 30s interval, 90s timeout
- No explicit cap on concurrent spawned sub-agents
- Tech-lead naming: `tech-lead-<n>`
- Warm standby: last idle instance never cleaned up
- Atomic file writes: write to temp → os.Rename
- Pool allocation serialisation mechanism]
