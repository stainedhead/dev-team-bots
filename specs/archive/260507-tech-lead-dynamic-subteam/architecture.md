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
| `local/bus` (`boabot/internal/infrastructure/local/bus/bus.go`) | `NewScopedBus()` returns a brand-new `*Bus` value with its own empty `subscribers` map. Isolation is structural — two `*Bus` instances share no state. The main team bus and each session bus are distinct objects; there is no namespacing within a single shared bus. `Broadcast` on one cannot reach subscribers of the other. |
| `local/queue` (`boabot/internal/infrastructure/local/queue/queue.go`) | Each tech-lead session creates its own `*Router` (via `queue.NewRouter()`). Isolation is again structural — two `*Router` instances have independent channel maps. The main-team router is unaffected. A `Deregister` method will be added to `Router` to support session teardown cleanup. |
| `TeamManager` (`boabot/internal/application/team/team_manager.go`) | Dynamic registration/deregistration of tech-lead pool instances at runtime. Contract to be confirmed during Phase 2 research (Research Q3). |
| `InMemoryBoardStore` (`boabot/internal/infrastructure/local/orchestrator/board.go`) | Must emit an internal event (channel or callback) when an item's status changes, so pool allocation can react within 500ms without a fixed polling loop. The existing `persist()` already uses `write-tmp + os.Rename` — pool state file will follow the same pattern. |
| HTTP server (`boabot/internal/infrastructure/http/server.go`) | New `/api/v1/pool` endpoint added to `Server`. Existing team roster response extended with `status` and `item_id` fields (additive — not a breaking change). |

---

## Architectural Decisions

**AD-001 — ScopedBus isolation via distinct instances (resolved)**
Each session bus is a fresh `*Bus` value (`bus.New()`). No namespacing within a shared bus; no changes to the broadcast algorithm. Isolation is guaranteed by Go's value semantics — two `*Bus` pointers point to independent structs.

**AD-002 — Per-session Router via distinct instances (resolved)**
Each session queue is a fresh `*Router` value (`queue.NewRouter()`). The main-team router is never touched. A new `Deregister(botName string)` method will be added to `Router` to allow session teardown to clean up channels without leaking memory.

**AD-003 — Board watcher event emission mechanism (TBD — Phase 3)**
Options: (a) callback registered on `InMemoryBoardStore` at construction, (b) internal channel polled by pool manager, (c) polling loop with short interval. Decision deferred to Phase 3 after confirming `TeamManager` integration pattern.

From PRD decisions (already resolved):
- Heartbeat cadence: 30s interval, 90s timeout (3 missed → self-shutdown)
- No explicit cap on concurrent spawned sub-agents — heap watchdog is backstop
- Tech-lead pool naming: `tech-lead-<n>` incrementing per session; warm standby retains last active name
- Warm standby: last idle instance never cleaned up
- Atomic file writes: `os.WriteFile(path+".tmp") + os.Rename(tmp, path)` — same pattern as `InMemoryBoardStore.persist()`
- Pool allocation serialisation: mutex protecting the allocate/spawn critical section (FR-024)
