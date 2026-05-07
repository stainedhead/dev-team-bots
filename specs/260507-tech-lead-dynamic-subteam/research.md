# Research: Tech-Lead Dynamic Subteam and Orchestrator Pool Management

**Feature:** tech-lead-dynamic-subteam
**Created:** 2026-05-07
**Source PRD:** [tech-lead-dynamic-subteam-PRD.md](./tech-lead-dynamic-subteam-PRD.md)

---

## Research Questions

The following questions are derived from the PRD's dependencies and open risks. They must be answered before architecture is finalised.

1. **Scoped bus implementation:** Does the existing `local/bus` implementation support multiple independent bus instances in the same process, or does it rely on a single global event loop? What changes are required for `NewScopedBus()` to guarantee isolation?

2. **Queue router extensibility:** How does `local/queue` currently route messages to bot instances? Does it use a single shared routing table, or does it already support per-session namespacing? What is the minimal change to support parallel routing tables?

3. **TeamManager bot lifecycle:** What is the current contract for registering and deregistering bots with `TeamManager`? Can bots be dynamically added and removed at runtime without restarting the manager, and without disrupting existing registered bots?

4. **Board status transition latency:** How does the orchestrator currently detect item status changes — polling interval or event-driven? What is the current worst-case latency, and is it sufficient for the 1s spawn-ready requirement?

5. **Atomic file writes on the target platform:** What is the correct pattern for atomic file replacement in Go on Linux/macOS (write to temp file, `os.Rename`)? Are there any ECS/container filesystem constraints that would prevent `os.Rename` from being atomic?

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
