# PRD: Tech-Lead Dynamic Subteam Spawning

**Created:** 2026-05-07
**Jira:** N/A
**Status:** Draft

## Problem Statement

The tech-lead bot currently has no way to delegate work to isolated sub-agents. It can communicate with the static team roster, but cannot spin up multiple parallel workers of the same bot type, cannot give them isolated context windows, and cannot keep their work traffic off the main orchestrator channel. This limits the tech-lead to sequential single-threaded delegation and prevents parallel workstreams on independent branches or worktrees.

## Goals

1. Tech-lead can spawn named bot instances on demand from any available bot type, with caller-chosen names (e.g. `impl-feature-auth`, `reviewer-payments`)
2. Spawned bots operate on a private scoped message bus — isolated from the orchestrator and the main team channel
3. Multiple instances of the same bot type can run in parallel, each with its own context window and work directory (git worktree or branch)

## Non-Goals

- Spawned bots are not visible to or managed by the orchestrator Kanban board
- Spawned bots are ephemeral — they do not persist across tech-lead work sessions (beyond crash recovery)
- The orchestrator does not get a spawn tool — this capability belongs to the tech-lead only
- No UI for managing spawned sub-agents — capability is purely tool-driven from the tech-lead's context

## Functional Requirements

**FR-001:** Tech-lead has a `spawn_agent` tool accepting:
- `type` — bot type name matching an entry in `boabot-team/bots/`
- `name` — caller-chosen instance name (e.g. `impl-feature-auth`); must be unique within the session
- `work_dir` (optional) — working directory assigned to the spawned bot (git worktree or branch path)

**FR-002:** Each spawned bot runs as an isolated goroutine with its own context window; it does not share state with other spawned instances or with the tech-lead's own context.

**FR-003:** All spawned bots in a session connect to a private scoped message bus. Traffic on this bus is invisible to the orchestrator and the main team channel.

**FR-004:** Tech-lead addresses spawned bots by their instance name using existing `send_message` / `assign_task` tools, routed through the private bus.

**FR-005:** Multiple instances of the same bot type can be active simultaneously with distinct names (e.g. `impl-feature-1` and `impl-feature-2` both of type `implementer`).

**FR-006:** Each spawned bot operates exclusively within its assigned `work_dir` when set at spawn time.

**FR-007:** Tech-lead has a `terminate_agent` tool to explicitly shut down a named spawned bot. The bot finishes or safely checkpoints its current unit of work before stopping.

**FR-008:** All spawned bots in a session are automatically torn down when the tech-lead's task context ends — no orphaned goroutines.

**FR-009:** Spawned bots reply only to the tech-lead via the private bus — they do not broadcast to the wider team or the orchestrator channel.

**FR-010:** The `type` argument to `spawn_agent` is validated against available bot configs in `boabot-team/bots/` at spawn time. An invalid type returns a clear error to the tech-lead without spawning anything.

**FR-011:** Each spawned bot monitors heartbeat messages from the tech-lead on the private bus. If no heartbeat is received within a configurable timeout, the spawned bot:
1. Finishes its current unit of work if it can complete within a reasonable bound
2. Commits or checkpoints any in-progress state
3. Self-terminates cleanly and releases all resources

**FR-012:** On spawn, each bot writes a session record to a persistent session file in the tech-lead's memory directory, containing: instance name, bot type, work_dir, private bus ID, and status.

**FR-013:** On tech-lead startup, it checks for an existing session file. If active spawned bots are found still running, it reconnects to the private bus and queries each bot for its current status (idle, working, current task details). The tech-lead uses this information to decide how to proceed — wait for completion, issue new instructions, or terminate.

**FR-014:** On clean shutdown (explicit `terminate_agent` call or tech-lead context end), the session record for the terminated bot is removed from the session file.

## Non-Functional Requirements

- **Performance:** A spawned bot goroutine must be ready to receive its first task within 500ms of the `spawn_agent` call completing.
- **Reliability:** A panic in a spawned bot goroutine must be recovered and logged; it must not affect the tech-lead or other spawned bots. Each goroutine is guarded with `recover()`.
- **Isolation:** The private bus must guarantee no message leakage to the orchestrator channel or main team roster. This must be verified by explicit test coverage.
- **Observability:** Spawn and terminate events are logged in the tech-lead's output with instance name, bot type, and work_dir. Spawned bot lifecycle is visible in tech-lead's context.
- **Resource:** Spawned bots count against the host process heap. The existing heap watchdog applies — no separate per-session limit is introduced.

## Acceptance Criteria

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

## Dependencies and Risks

| Item | Type | Notes |
|------|------|-------|
| `local/bus` in-process event bus | Dependency | Must support scoped/private bus instances per session; likely requires a new `NewScopedBus()` constructor |
| `local/queue` message queue | Dependency | Router must support per-session routing tables alongside the main team routing |
| `TeamManager` | Dependency | Dynamic spawn/terminate must integrate with existing bot lifecycle without disrupting the main team |
| `boabot-team/bots/` configs | Dependency | Bot type resolution at spawn time — configs are read at spawn, not at process start |
| Goroutine leak on abnormal tech-lead exit | Risk | Mitigated by FR-011 (heartbeat monitoring triggers clean self-shutdown) and FR-008 (parent context cancellation as secondary guard) |
| Private bus message isolation | Risk | A routing bug could leak spawned-bot traffic to the main channel — requires explicit test coverage on isolation boundary |
| Heap exhaustion from many spawned bots | Risk | Each spawned bot loads a model provider and memory store; large sessions could approach the heap hard limit — tech-lead should log a warning when spawning beyond a soft threshold |
| Session file consistency | Risk | If the process is killed hard (SIGKILL), the session file may contain stale records — reconnect logic must verify that discovered bots are actually alive before attempting to rejoin |

## Decisions

- **Heartbeat cadence:** 30s interval, 90s timeout (3 missed heartbeats triggers self-shutdown)
- **Max concurrent spawned bots:** No explicit cap enforced by the tech-lead — the existing heap watchdog is the backstop; tech-lead logs a warning when a new spawn would bring the count above a soft threshold (suggested: 5)
- **Heartbeat timeout notification:** Spawned bot logs the timeout event with its instance name and last known task, then exits silently — no attempt to notify tech-lead (the lead is presumed unreachable)
