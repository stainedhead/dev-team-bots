# Feature Spec: Tech-Lead Dynamic Subteam and Orchestrator Pool Management

**Created:** 2026-05-07
**Source PRD:** [tech-lead-dynamic-subteam-PRD.md](./tech-lead-dynamic-subteam-PRD.md)
**Status:** Draft

---

## Executive Summary

This feature enables two closely related capabilities: (1) the tech-lead bot can dynamically spawn and manage named sub-agent instances (implementers, reviewers, etc.) with isolated context windows and private message bus communication, and (2) the orchestrator dynamically manages a pool of tech-lead instances, allocating one per In Progress kanban item and maintaining a warm standby. Together these capabilities unlock true parallel workstreams at both the team and sub-team level.

---

## Problem Statement

Two related gaps exist in the current system:

1. **Tech-lead subteam spawning:** The tech-lead bot has no way to delegate work to isolated sub-agents. It communicates with the static team roster but cannot spin up multiple parallel workers of the same bot type, cannot give them isolated context windows, and cannot keep their traffic off the main orchestrator channel. This limits the tech-lead to sequential single-threaded delegation.

2. **Orchestrator pool management:** The orchestrator has a single static tech-lead bot, so only one kanban item can have an active tech-lead at a time. There is no lifecycle management to spawn or clean up tech-lead instances as board status changes.

---

## Goals

1. Tech-lead can spawn named bot instances on demand from any available bot type, with caller-chosen names
2. Spawned bots operate on a private scoped message bus — isolated from the orchestrator and the main team channel
3. Multiple instances of the same bot type can run in parallel, each with its own context window and work directory
4. Each kanban item that moves to In Progress gets a dedicated tech-lead instance allocated by the orchestrator
5. The orchestrator spawns a new tech-lead only when needed — idle instances are assigned first
6. Tech-lead instances are cleaned up when their item leaves In Progress; the last instance is kept warm as a standby

## Non-Goals

- Spawned sub-agent bots are not visible to or managed by the orchestrator Kanban board
- Spawned sub-agent bots are ephemeral — no persistence across tech-lead work sessions (beyond crash recovery)
- The orchestrator does not get a spawn tool for sub-agents — that belongs to the tech-lead only
- No UI for managing spawned sub-agents — purely tool-driven
- The orchestrator does not manage the tech-lead's internal sub-team
- Tech-lead instances are not shared across multiple kanban items — strict 1:1 allocation
- No manual UI control for spawning or terminating tech-leads
- The pool does not pre-warm more than one idle tech-lead — no speculative spawning

---

## User Requirements / Functional Requirements

### Tech-Lead Subteam Spawning

**FR-001** (FR-TL-001): Tech-lead has a `spawn_agent` tool accepting:
- `type` — bot type name matching an entry in `boabot-team/bots/`
- `name` — caller-chosen instance name (unique within the session)
- `work_dir` (optional) — working directory assigned to the spawned bot

**FR-002** (FR-TL-002): Each spawned bot runs as an isolated goroutine with its own context window; it does not share state with other spawned instances or with the tech-lead's own context.

**FR-003** (FR-TL-003): All spawned bots in a session connect to a private scoped message bus. Traffic on this bus is invisible to the orchestrator and the main team channel.

**FR-004** (FR-TL-004): Tech-lead addresses spawned bots by their instance name using existing `send_message` / `assign_task` tools, routed through the private bus.

**FR-005** (FR-TL-005): Multiple instances of the same bot type can be active simultaneously with distinct names.

**FR-006** (FR-TL-006): Each spawned bot operates exclusively within its assigned `work_dir` when set at spawn time.

**FR-007** (FR-TL-007): Tech-lead has a `terminate_agent` tool to explicitly shut down a named spawned bot. The bot finishes or safely checkpoints its current unit of work before stopping.

**FR-008** (FR-TL-008): All spawned bots in a session are automatically torn down when the tech-lead's task context ends — no orphaned goroutines.

**FR-009** (FR-TL-009): Spawned bots reply only to the tech-lead via the private bus — they do not broadcast to the wider team or orchestrator channel.

**FR-010** (FR-TL-010): The `type` argument to `spawn_agent` is validated against available bot configs in `boabot-team/bots/` at spawn time. An invalid type returns a clear error without spawning anything.

**FR-011** (FR-TL-011): Each spawned bot monitors heartbeat messages from the tech-lead on the private bus. If no heartbeat is received within the configured timeout, the spawned bot: (1) finishes its current unit of work if possible within a reasonable bound, (2) commits or checkpoints any in-progress state, (3) self-terminates cleanly and releases all resources.

**FR-012** (FR-TL-012): On spawn, each bot writes a session record to a persistent session file in the tech-lead's memory directory, containing: instance name, bot type, work_dir, private bus ID, and status.

**FR-013** (FR-TL-013): On tech-lead startup, it checks for an existing session file. If active spawned bots are found still running, it reconnects to the private bus and queries each bot for its current status. The tech-lead uses this to decide how to proceed.

**FR-014** (FR-TL-014): On clean shutdown, the session record for the terminated bot is removed from the session file.

### Orchestrator Pool Management

**FR-015** (FR-ORC-001): The orchestrator monitors kanban board item status transitions in real time.

**FR-016** (FR-ORC-002): When an item transitions to `in_progress`, the orchestrator checks the tech-lead pool for an idle instance. If one exists, it is allocated. If none exists, a new tech-lead instance is spawned and allocated.

**FR-017** (FR-ORC-003): Each tech-lead instance is allocated to exactly one kanban item at a time. The association (item ID → tech-lead instance name) is recorded in a persistent pool state file.

**FR-018** (FR-ORC-004): Spawned tech-lead instances are named distinctly (e.g. `tech-lead-1`, `tech-lead-2`) and appear in the team roster with their current status and associated item ID.

**FR-019** (FR-ORC-005): When an item transitions out of `in_progress`, the orchestrator signals the associated tech-lead to shut down. The tech-lead finishes in-flight work, tears down its sub-team, and exits cleanly.

**FR-020** (FR-ORC-006): If the cleaned-up tech-lead was not the last instance, it is removed from the roster. If it was the last instance, it transitions to idle and remains as the warm standby.

**FR-021** (FR-ORC-007): The orchestrator always maintains at least one idle tech-lead in the pool.

**FR-022** (FR-ORC-008): On orchestrator startup, it reads the pool state file and reconciles against any tech-lead instances still running, re-establishing allocations for items still In Progress.

**FR-023** (FR-ORC-009): The orchestrator exposes current pool state via the existing `/api/v1` REST API.

**FR-024** (FR-ORC-010): Pool allocation is serialized — near-simultaneous transitions to In Progress are processed sequentially to prevent duplicate spawns.

---

## Non-Functional Requirements

| Category | Requirement |
|----------|-------------|
| Performance | Spawned sub-agent goroutine ready within 500ms of `spawn_agent` completing |
| Performance | New tech-lead instance ready within 1s of item transitioning to In Progress |
| Reliability | Panic in spawned bot goroutine recovered and logged; does not affect tech-lead or other bots |
| Reliability | Crashed tech-lead detected via heartbeat; orchestrator marks associated item `blocked` |
| Isolation | Private bus guarantees no message leakage to orchestrator or main team channel; verified by tests |
| Consistency | Pool state file updated atomically; process kill mid-write must not corrupt the allocation map |
| Observability | Spawn/terminate events logged with instance name, bot type, work_dir, item ID, timestamp |
| Observability | Pool state visible via REST API |
| Resource | All spawned bots count against host process heap; heap watchdog applies; soft-threshold warnings logged |

---

## System Architecture

### Affected Layers

- **Domain:** New interfaces — `SubTeamManager`, `TechLeadPool`; new entities — `SpawnedAgent`, `PoolEntry`
- **Application:** New use cases — `SpawnAgent`, `TerminateAgent`, `HeartbeatMonitor` (tech-lead side); `AllocateTechLead`, `DeallocateTechLead`, `ReconcilePool` (orchestrator side)
- **Infrastructure:** New `local/bus` scoped bus constructor; session file persistence; pool state file persistence; routing table extensions in `local/queue`

### New / Modified Components

| Component | Type | Location |
|-----------|------|----------|
| `ScopedBus` | New | `boabot/internal/infrastructure/local/bus/` |
| `SubTeamManager` interface | New | `boabot/internal/domain/` |
| `SubTeamManagerImpl` | New | `boabot/internal/application/` |
| `TechLeadPool` interface | New | `boabot/internal/domain/` |
| `TechLeadPoolImpl` | New | `boabot/internal/application/` |
| `SessionFile` | New | `boabot/internal/infrastructure/` |
| `PoolStateFile` | New | `boabot/internal/infrastructure/` |
| `spawn_agent` tool | New | tech-lead bot tool handler |
| `terminate_agent` tool | New | tech-lead bot tool handler |
| `local/queue` router | Modified | `boabot/internal/infrastructure/local/queue/` |
| Orchestrator board watcher | Modified | orchestrator bot application layer |
| `/api/v1` REST handler | Modified | orchestrator infrastructure layer |

---

## Scope of Changes

### Files to Create

- `boabot/internal/domain/subteam.go` — `SubTeamManager` interface, `SpawnedAgent` entity
- `boabot/internal/domain/pool.go` — `TechLeadPool` interface, `PoolEntry` entity
- `boabot/internal/application/subteam_manager.go` — spawn/terminate/heartbeat use cases
- `boabot/internal/application/tech_lead_pool.go` — allocate/deallocate/reconcile use cases
- `boabot/internal/infrastructure/local/bus/scoped_bus.go` — `NewScopedBus()` constructor
- `boabot/internal/infrastructure/session_file.go` — session persistence
- `boabot/internal/infrastructure/pool_state_file.go` — atomic pool state persistence
- Test files for all of the above

### Files to Modify

- `boabot/internal/infrastructure/local/queue/router.go` — per-session routing tables
- Orchestrator bot: board watcher → emit status-change events
- Orchestrator bot: REST API handler → pool state endpoint
- Tech-lead bot: tool registry → register `spawn_agent`, `terminate_agent`

### Dependencies

- No new external dependencies expected — all new components use stdlib and existing internal packages

### Breaking Changes

- REST API: new fields added to team roster entries (`status`, `item_id`) — additive, not breaking
- Pool state file: new file created in orchestrator memory directory — no migration needed

---

## Success Criteria and Acceptance Criteria

### Tech-Lead Subteam Spawning

- [ ] `spawn_agent(type="implementer", name="impl-auth", work_dir="...")` starts bot within 500ms
- [ ] Two implementer instances run concurrently with no message traffic crossing
- [ ] Messages between tech-lead and spawned bots do not appear on orchestrator or main channels
- [ ] A spawned bot with `work_dir` set operates exclusively within that directory
- [ ] `terminate_agent(name="impl-auth")` finishes current work unit and stops cleanly; session record removed
- [ ] All spawned bots torn down when tech-lead context ends — confirmed by goroutine count
- [ ] Panic in spawned bot goroutine recovered and logged; tech-lead and other bots unaffected
- [ ] `spawn_agent` with invalid `type` returns clear error and spawns nothing
- [ ] Bot receiving no heartbeat within timeout finishes current task, checkpoints, and self-terminates
- [ ] Restarted tech-lead discovers still-running bots via session file and reconnects without re-spawning

### Orchestrator Pool Management

- [ ] Item to In Progress with no idle tech-lead → new instance spawned and allocated within 1s
- [ ] Item to In Progress with idle tech-lead → allocated without spawning a new instance
- [ ] Two items In Progress → each has its own dedicated tech-lead with no shared state
- [ ] Item leaves In Progress → tech-lead shuts down cleanly, sub-team torn down, session records removed
- [ ] Last tech-lead instance never cleaned up — transitions to idle, stays in roster
- [ ] Team roster reflects all active tech-lead instances with correct status and item ID
- [ ] On restart, pool state file read and surviving tech-leads reconnected without re-spawning
- [ ] Tech-lead crash (heartbeat timeout) → orchestrator marks item `blocked` and logs event
- [ ] Pool state file writes are atomic — simulated kill mid-write leaves previous valid state intact
- [ ] `/api/v1` returns pool state with all tech-lead instances, statuses, and item associations

### Quality Gates

- Coverage ≥ 90% on domain and application layers
- `go vet` and `golangci-lint` pass with zero warnings
- All tests pass with `-race` flag

---

## Risks and Mitigation

| Risk | Mitigation |
|------|------------|
| Goroutine leak on abnormal tech-lead exit | FR-011 heartbeat monitoring + FR-008 parent context cancellation as secondary guard |
| Private bus message isolation breach | Explicit isolation test coverage on the bus routing boundary |
| Item status change races (duplicate spawns) | Serialized pool allocation (FR-024) |
| Tech-lead crash leaves item stuck | Heartbeat detection → auto-mark `blocked`; operator re-triggers manually |
| Pool state file drift on hard kill | Atomic writes + startup reconciliation prunes stale dead-instance entries |
| Session file stale records on SIGKILL | Reconnect logic verifies each discovered bot is actually alive before rejoining |
| Heap exhaustion from large sessions | Heap watchdog is backstop; soft-threshold warnings from both tech-lead and orchestrator |

---

## Timeline and Milestones

| Phase | Milestone |
|-------|-----------|
| Phase 2 | Domain interfaces and entities defined; data dictionary complete |
| Phase 3 | Architecture design complete; sequence diagrams for spawn/terminate/heartbeat flows |
| Phase 4 | Task breakdown complete with estimates |
| Phase 5 | Implementation complete; all acceptance criteria passing |
| Phase 6 | Docs updated; spec archived |

---

## References

- Source PRD: [tech-lead-dynamic-subteam-PRD.md](./tech-lead-dynamic-subteam-PRD.md)
