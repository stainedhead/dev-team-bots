# PRD: Orchestrator Tech-Lead Pool Management

**Created:** 2026-05-07
**Jira:** N/A
**Status:** Draft

## Problem Statement

The orchestrator has a single static tech-lead bot, so only one kanban item can have an active tech-lead at a time. When multiple items are In Progress simultaneously, there is no mechanism to give each one a dedicated tech-lead, and no lifecycle management to spawn or clean up tech-lead instances as board status changes. The orchestrator must become the monitor and allocator of a dynamic tech-lead pool, with each In Progress item receiving its own dedicated instance.

## Goals

1. Each kanban item that moves to In Progress gets a dedicated tech-lead instance allocated to it by the orchestrator
2. The orchestrator spawns a new tech-lead only when needed — idle instances are assigned first
3. Tech-lead instances are cleaned up when their associated item leaves In Progress, with the last remaining instance kept warm as a standby for the next task

## Non-Goals

- The orchestrator does not manage the tech-lead's internal sub-team (spawned implementers, reviewers, etc.) — that is handled by the tech-lead itself per the Tech-Lead Dynamic Subteam Spawning PRD
- Tech-lead instances are not shared across multiple kanban items — the 1:1 allocation is strict
- No manual UI control for spawning or terminating tech-leads — the orchestrator manages the pool automatically based on board state
- The pool does not pre-warm more than one idle tech-lead — no speculative spawning ahead of demand

## Functional Requirements

**FR-001:** The orchestrator monitors kanban board item status transitions in real time.

**FR-002:** When an item transitions to `in_progress`, the orchestrator checks the tech-lead pool for an idle instance. If one exists, it is allocated to that item. If none exists, a new tech-lead instance is spawned and allocated.

**FR-003:** Each tech-lead instance is allocated to exactly one kanban item at a time. The association (item ID → tech-lead instance name) is recorded in a persistent pool state file.

**FR-004:** Spawned tech-lead instances are named distinctly (e.g. `tech-lead-1`, `tech-lead-2`) and appear in the team roster with their current status and associated item ID.

**FR-005:** When an item transitions out of `in_progress` (to `done`, `blocked`, or `backlog`), the orchestrator signals the associated tech-lead to shut down. The tech-lead finishes in-flight work, tears down its sub-team, and exits cleanly.

**FR-006:** If the cleaned-up tech-lead was not the last instance, it is removed from the roster. If it was the last instance, it transitions to idle and remains in the roster as the warm standby.

**FR-007:** The orchestrator always maintains at least one idle tech-lead in the pool — the warm standby is never cleaned up.

**FR-008:** On orchestrator startup, it reads the pool state file and reconciles against any tech-lead instances still running, re-establishing allocations for any items still In Progress.

**FR-009:** The orchestrator exposes the current pool state (instance name, status, associated item ID) via the existing `/api/v1` REST API so the web UI can reflect it.

**FR-010:** Pool allocation is serialized — if two items transition to In Progress near-simultaneously, the orchestrator processes them sequentially to prevent duplicate spawns for the same item.

## Non-Functional Requirements

- **Performance:** A new tech-lead instance must be spawned and ready to receive its first task within 1s of the item transitioning to In Progress
- **Reliability:** A crashed tech-lead instance must not affect the orchestrator or other tech-lead instances; the orchestrator detects the crash via missed heartbeats and marks the associated item as `blocked`
- **Consistency:** The pool state file must be updated atomically — a process kill mid-write must not corrupt the allocation map
- **Observability:** All pool events (spawn, allocate, deallocate, clean up) are logged by the orchestrator with item ID, instance name, and timestamp; pool state is visible in the REST API
- **Scalability:** No hard cap on concurrent tech-lead instances beyond the existing heap watchdog limit; orchestrator logs a warning when the pool size crosses a soft threshold

## Acceptance Criteria

- [ ] When a kanban item moves to In Progress and no tech-lead is idle, a new instance is spawned and allocated to that item within 1s
- [ ] When a kanban item moves to In Progress and an idle tech-lead exists, it is allocated without spawning a new instance
- [ ] Two items In Progress simultaneously each have their own dedicated tech-lead instance with no shared state between them
- [ ] When an item leaves In Progress, its allocated tech-lead shuts down cleanly — sub-team torn down, session records removed
- [ ] The last remaining tech-lead instance is never cleaned up — it transitions to idle and stays in the roster
- [ ] The team roster reflects all active tech-lead instances with correct status and associated item ID
- [ ] On orchestrator restart, the pool state file is read and any tech-leads still running are reconnected to their items without re-spawning
- [ ] If a tech-lead instance crashes (detected via heartbeat timeout), the orchestrator marks its associated kanban item as `blocked` and logs the event
- [ ] Pool state file writes are atomic — a simulated kill mid-write leaves the previous valid state intact
- [ ] `/api/v1` returns current pool state including all tech-lead instances, their statuses, and item associations

## Dependencies and Risks

| Item | Type | Notes |
|------|------|-------|
| Tech-Lead Dynamic Subteam Spawning PRD | Dependency | Tech-lead instances must support the spawn/terminate/heartbeat/session-file lifecycle defined there — this PRD builds directly on top of it |
| Board status transition events | Dependency | Orchestrator must receive reliable notifications when item status changes — currently via polling; may need an internal event hook for low-latency detection |
| Pool state file | Dependency | Persistent allocation map keyed by item ID; must support atomic writes |
| `local/bus` scoped bus | Dependency | One private bus per tech-lead instance; orchestrator must be able to address each independently |
| Item status change races | Risk | Two items could transition to In Progress near-simultaneously — pool allocation must be serialized (FR-010) to prevent duplicate spawns |
| Tech-lead crash leaves item stuck In Progress | Risk | Mitigated by heartbeat monitoring — orchestrator marks item `blocked` and logs; operator must manually re-trigger or reassign |
| Pool state file drift on hard kill | Risk | Mitigated by atomic writes and startup reconciliation — stale entries for dead instances are pruned when reconnect fails |
| Heap exhaustion with many concurrent items | Risk | Each tech-lead plus its sub-team consumes heap; heap watchdog is the backstop; orchestrator warns when pool size crosses soft threshold |

## Decisions

- **Naming scheme:** Tech-lead instances are named `tech-lead-<n>` where `n` increments per session; the warm standby retains the name of the last active instance
- **Blocked item handling:** On tech-lead crash, the item is marked `blocked` automatically — no attempt is made to auto-reassign without operator intervention
- **Pool state file location:** Stored in the orchestrator's memory directory alongside other persistent state
