# PRD: Tech-Lead Dynamic Subteam and Orchestrator Pool Management

**Created:** 2026-05-07
**Jira:** N/A
**Status:** Draft

## Problem Statement

Two related gaps exist in the current system:

1. **Tech-lead subteam spawning:** The tech-lead bot has no way to delegate work to isolated sub-agents. It can communicate with the static team roster but cannot spin up multiple parallel workers of the same bot type, cannot give them isolated context windows, and cannot keep their work traffic off the main orchestrator channel. This limits the tech-lead to sequential single-threaded delegation and prevents parallel workstreams on independent branches or worktrees.

2. **Orchestrator pool management:** The orchestrator has a single static tech-lead bot, so only one kanban item can have an active tech-lead at a time. When multiple items are In Progress simultaneously, there is no mechanism to give each one a dedicated tech-lead, and no lifecycle management to spawn or clean up tech-lead instances as board status changes.

## Goals

1. Tech-lead can spawn named bot instances on demand from any available bot type, with caller-chosen names (e.g. `impl-feature-auth`, `reviewer-payments`)
2. Spawned bots operate on a private scoped message bus — isolated from the orchestrator and the main team channel
3. Multiple instances of the same bot type can run in parallel, each with its own context window and work directory (git worktree or branch)
4. Each kanban item that moves to In Progress gets a dedicated tech-lead instance allocated to it by the orchestrator
5. The orchestrator spawns a new tech-lead only when needed — idle instances are assigned first
6. Tech-lead instances are cleaned up when their associated item leaves In Progress, with the last remaining instance kept warm as a standby for the next task

## Non-Goals

- Spawned sub-agent bots are not visible to or managed by the orchestrator Kanban board
- Spawned sub-agent bots are ephemeral — they do not persist across tech-lead work sessions (beyond crash recovery)
- The orchestrator does not get a spawn tool for sub-agents — that capability belongs to the tech-lead only
- No UI for managing spawned sub-agents — capability is purely tool-driven from the tech-lead's context
- The orchestrator does not manage the tech-lead's internal sub-team (spawned implementers, reviewers, etc.) — that is handled by the tech-lead itself
- Tech-lead instances are not shared across multiple kanban items — the 1:1 allocation is strict
- No manual UI control for spawning or terminating tech-leads — the orchestrator manages the pool automatically based on board state
- The pool does not pre-warm more than one idle tech-lead — no speculative spawning ahead of demand

---

## Functional Requirements — Tech-Lead Subteam Spawning

**FR-TL-001:** Tech-lead has a `spawn_agent` tool accepting:
- `type` — bot type name matching an entry in `boabot-team/bots/`
- `name` — caller-chosen instance name (e.g. `impl-feature-auth`); must be unique within the session
- `work_dir` (optional) — working directory assigned to the spawned bot (git worktree or branch path)

**FR-TL-002:** Each spawned bot runs as an isolated goroutine with its own context window; it does not share state with other spawned instances or with the tech-lead's own context.

**FR-TL-003:** All spawned bots in a session connect to a private scoped message bus. Traffic on this bus is invisible to the orchestrator and the main team channel.

**FR-TL-004:** Tech-lead addresses spawned bots by their instance name using existing `send_message` / `assign_task` tools, routed through the private bus.

**FR-TL-005:** Multiple instances of the same bot type can be active simultaneously with distinct names (e.g. `impl-feature-1` and `impl-feature-2` both of type `implementer`).

**FR-TL-006:** Each spawned bot operates exclusively within its assigned `work_dir` when set at spawn time.

**FR-TL-007:** Tech-lead has a `terminate_agent` tool to explicitly shut down a named spawned bot. The bot finishes or safely checkpoints its current unit of work before stopping.

**FR-TL-008:** All spawned bots in a session are automatically torn down when the tech-lead's task context ends — no orphaned goroutines.

**FR-TL-009:** Spawned bots reply only to the tech-lead via the private bus — they do not broadcast to the wider team or the orchestrator channel.

**FR-TL-010:** The `type` argument to `spawn_agent` is validated against available bot configs in `boabot-team/bots/` at spawn time. An invalid type returns a clear error to the tech-lead without spawning anything.

**FR-TL-011:** Each spawned bot monitors heartbeat messages from the tech-lead on the private bus. If no heartbeat is received within a configurable timeout, the spawned bot:
1. Finishes its current unit of work if it can complete within a reasonable bound
2. Commits or checkpoints any in-progress state
3. Self-terminates cleanly and releases all resources

**FR-TL-012:** On spawn, each bot writes a session record to a persistent session file in the tech-lead's memory directory, containing: instance name, bot type, work_dir, private bus ID, and status.

**FR-TL-013:** On tech-lead startup, it checks for an existing session file. If active spawned bots are found still running, it reconnects to the private bus and queries each bot for its current status (idle, working, current task details). The tech-lead uses this information to decide how to proceed — wait for completion, issue new instructions, or terminate.

**FR-TL-014:** On clean shutdown (explicit `terminate_agent` call or tech-lead context end), the session record for the terminated bot is removed from the session file.

---

## Functional Requirements — Orchestrator Pool Management

**FR-ORC-001:** The orchestrator monitors kanban board item status transitions in real time.

**FR-ORC-002:** When an item transitions to `in_progress`, the orchestrator checks the tech-lead pool for an idle instance. If one exists, it is allocated to that item. If none exists, a new tech-lead instance is spawned and allocated.

**FR-ORC-003:** Each tech-lead instance is allocated to exactly one kanban item at a time. The association (item ID → tech-lead instance name) is recorded in a persistent pool state file.

**FR-ORC-004:** Spawned tech-lead instances are named distinctly (e.g. `tech-lead-1`, `tech-lead-2`) and appear in the team roster with their current status and associated item ID.

**FR-ORC-005:** When an item transitions out of `in_progress` (to `done`, `blocked`, or `backlog`), the orchestrator signals the associated tech-lead to shut down. The tech-lead finishes in-flight work, tears down its sub-team, and exits cleanly.

**FR-ORC-006:** If the cleaned-up tech-lead was not the last instance, it is removed from the roster. If it was the last instance, it transitions to idle and remains in the roster as the warm standby.

**FR-ORC-007:** The orchestrator always maintains at least one idle tech-lead in the pool — the warm standby is never cleaned up.

**FR-ORC-008:** On orchestrator startup, it reads the pool state file and reconciles against any tech-lead instances still running, re-establishing allocations for any items still In Progress.

**FR-ORC-009:** The orchestrator exposes the current pool state (instance name, status, associated item ID) via the existing `/api/v1` REST API so the web UI can reflect it.

**FR-ORC-010:** Pool allocation is serialized — if two items transition to In Progress near-simultaneously, the orchestrator processes them sequentially to prevent duplicate spawns for the same item.

---

## Non-Functional Requirements

- **Performance (subteam):** A spawned bot goroutine must be ready to receive its first task within 500ms of the `spawn_agent` call completing.
- **Performance (pool):** A new tech-lead instance must be spawned and ready to receive its first task within 1s of the item transitioning to In Progress.
- **Reliability (subteam):** A panic in a spawned bot goroutine must be recovered and logged; it must not affect the tech-lead or other spawned bots. Each goroutine is guarded with `recover()`.
- **Reliability (pool):** A crashed tech-lead instance must not affect the orchestrator or other tech-lead instances; the orchestrator detects the crash via missed heartbeats and marks the associated item as `blocked`.
- **Isolation:** The private bus must guarantee no message leakage to the orchestrator channel or main team roster. This must be verified by explicit test coverage.
- **Consistency:** The pool state file must be updated atomically — a process kill mid-write must not corrupt the allocation map.
- **Observability:** Spawn and terminate events are logged by both the tech-lead and the orchestrator with instance name, bot type, work_dir, item ID, and timestamp. Pool state is visible via the REST API.
- **Resource:** Spawned bots count against the host process heap. The existing heap watchdog applies. Both the tech-lead (for sub-agents) and the orchestrator (for tech-lead pool size) log a warning when spawning beyond a soft threshold.

---

## Acceptance Criteria

### Tech-Lead Subteam Spawning

- [ ] Tech-lead can call `spawn_agent(type="implementer", name="impl-auth", work_dir="...")` and the named bot begins accepting tasks within 500ms
- [ ] Two implementer instances (`impl-feature-1`, `impl-feature-2`) can run concurrently; each receives only its own tasks and their message traffic does not cross
- [ ] Messages between tech-lead and spawned bots do not appear on the orchestrator channel or in any other bot's message queue
- [ ] A spawned bot assigned a `work_dir` operates exclusively within that directory
- [ ] Tech-lead can call `terminate_agent(name="impl-auth")`; the bot finishes its current work unit, then the goroutine stops cleanly with its session record removed
- [ ] All spawned bots in a session are torn down automatically when the tech-lead's context ends — confirmed by goroutine count before and after
- [ ] A panic in a spawned bot goroutine is recovered and logged; the tech-lead and other spawned bots continue running unaffected
- [ ] `spawn_agent` with an invalid `type` returns a clear error and spawns nothing
- [ ] If a spawned bot receives no heartbeat from tech-lead within the configured timeout, it finishes its current task, checkpoints state, and self-terminates
- [ ] A restarted tech-lead discovers still-running spawned bots via the session file, reconnects to the private bus, queries each bot's current status, and can issue further instructions without re-spawning

### Orchestrator Pool Management

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

---

## Dependencies and Risks

| Item | Type | Notes |
|------|------|-------|
| `local/bus` in-process event bus | Dependency | Must support scoped/private bus instances per session; likely requires a new `NewScopedBus()` constructor; orchestrator must be able to address each tech-lead's private bus independently |
| `local/queue` message queue | Dependency | Router must support per-session routing tables alongside the main team routing |
| `TeamManager` | Dependency | Dynamic spawn/terminate of both sub-agents and tech-lead pool instances must integrate with existing bot lifecycle without disrupting the main team |
| `boabot-team/bots/` configs | Dependency | Bot type resolution at spawn time — configs are read at spawn, not at process start |
| Board status transition events | Dependency | Orchestrator must receive reliable notifications when item status changes — currently via polling; may need an internal event hook for low-latency detection |
| Pool state file | Dependency | Persistent allocation map keyed by item ID; must support atomic writes |
| Goroutine leak on abnormal tech-lead exit | Risk | Mitigated by FR-TL-011 (heartbeat monitoring triggers clean self-shutdown) and FR-TL-008 (parent context cancellation as secondary guard) |
| Private bus message isolation | Risk | A routing bug could leak spawned-bot traffic to the main channel — requires explicit test coverage on isolation boundary |
| Item status change races | Risk | Two items could transition to In Progress near-simultaneously — pool allocation must be serialized (FR-ORC-010) to prevent duplicate spawns |
| Tech-lead crash leaves item stuck In Progress | Risk | Mitigated by heartbeat monitoring — orchestrator marks item `blocked` and logs; operator must manually re-trigger or reassign |
| Pool state file drift on hard kill | Risk | Mitigated by atomic writes and startup reconciliation — stale entries for dead instances are pruned when reconnect fails |
| Session file consistency | Risk | If the process is killed hard (SIGKILL), the session file may contain stale records — reconnect logic must verify that discovered bots are actually alive before attempting to rejoin |
| Heap exhaustion | Risk | Each tech-lead plus its sub-team consumes heap; heap watchdog is the backstop; both tech-lead and orchestrator warn when their respective spawn counts cross a soft threshold |

---

## Decisions

- **Heartbeat cadence:** 30s interval, 90s timeout (3 missed heartbeats triggers self-shutdown)
- **Heartbeat timeout notification:** Spawned bot logs the timeout event with its instance name and last known task, then exits silently — no attempt to notify tech-lead (the lead is presumed unreachable)
- **Max concurrent spawned sub-agents:** No explicit cap enforced by the tech-lead — the existing heap watchdog is the backstop; tech-lead logs a warning when a new spawn would bring the count above a soft threshold (suggested: 5)
- **Tech-lead naming scheme:** Instances are named `tech-lead-<n>` where `n` increments per session; the warm standby retains the name of the last active instance
- **Blocked item handling:** On tech-lead crash, the item is marked `blocked` automatically — no attempt is made to auto-reassign without operator intervention
- **Pool state file location:** Stored in the orchestrator's memory directory alongside other persistent state
