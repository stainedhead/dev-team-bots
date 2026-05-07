# Research: Tech-Lead Dynamic Subteam and Orchestrator Pool Management

**Feature:** tech-lead-dynamic-subteam
**Created:** 2026-05-07
**Source PRD:** [tech-lead-dynamic-subteam-PRD.md](./tech-lead-dynamic-subteam-PRD.md)

---

## Research Questions

The following questions are derived from the PRD's dependencies and open risks. They must be answered before architecture is finalised.

1. **Scoped bus implementation:** ✅ Answered — `bus.New()` already returns an independent `*Bus` with its own `subscribers` map. Two `*Bus` values share no state. `NewScopedBus()` is simply an alias or thin wrapper for `bus.New()`. No changes to broadcast algorithm needed. See `boabot/internal/infrastructure/local/bus/bus.go`.

2. **Queue router extensibility:** ✅ Answered — `queue.NewRouter()` already returns an independent `*Router` with its own channel map. Per-session isolation is achieved by giving each session its own `*Router`. The only required change is adding a `Deregister(botName string)` method to `Router` for session teardown. See `boabot/internal/infrastructure/local/queue/queue.go`.

3. **TeamManager bot lifecycle:** Open — need to confirm whether `TeamManager` supports dynamic add/remove of bots at runtime without restart. See `boabot/internal/application/team/team_manager.go`.

4. **Board status transition latency:** Open — `InMemoryBoardStore` currently has no event emission mechanism; the board is mutated by HTTP handlers. Need to determine how the pool manager will be notified of status changes within the 500ms detection requirement. Options: callback at construction, internal channel, or short-interval poll. See `boabot/internal/infrastructure/local/orchestrator/board.go`.

5. **Atomic file writes on the target platform:** ✅ Answered — `InMemoryBoardStore.persist()` already uses `os.WriteFile(path+".tmp") + os.Rename(tmp, path)`. This pattern is proven in the codebase and is atomic on Linux/macOS (both use POSIX `rename(2)`). ECS uses Linux — no constraint. Pool state file and session file will use the identical pattern.

6. **Tech-lead tool registry:** ✅ Answered — `spawn_agent` and `terminate_agent` are **not** LLM function-call tools. The `codeagent` provider runs `claude` CLI as a subprocess; tool injection via MCP server is out of scope. Instead, they are implemented as new `MessageType` values (`spawn.agent`, `terminate.agent`) that the tech-lead's `RunAgentUseCase.handle()` switch handles in `boabot/internal/application/run_agent.go`. A `SubTeamManager` is injected into `RunAgentUseCase` during wiring in `boabot/internal/application/team/team_manager.go` (in `startBot()`, detecting `entry.Type == "tech-lead"`). The tech-lead's model output triggers spawning via the message dispatch system — the orchestrator or operator sends a `spawn.agent` message to the tech-lead's queue.

---

## Industry Standards

[TBD — fill in during Phase 2 research]

---

## Existing Implementations

[TBD — explore `boabot/internal/infrastructure/local/bus/`, `local/queue/`, `domain/`, and the orchestrator bot source]

---

## API Documentation

[TBD — document the existing `local/bus` and `local/queue` interfaces; document the existing `TeamManager` interface; document the `/api/v1` REST endpoints]

---

## Best Practices

[TBD — goroutine lifecycle management in Go; context cancellation patterns; atomic file I/O; heartbeat/watchdog patterns]

---

## Open Questions

[TBD — track questions that arise during implementation research]

---

## References

[TBD — links to relevant Go stdlib docs, internal code, prior ADR entries]
