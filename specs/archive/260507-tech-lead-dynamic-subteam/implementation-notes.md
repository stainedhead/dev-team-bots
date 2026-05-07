# Implementation Notes: Tech-Lead Dynamic Subteam and Orchestrator Pool Management

**Feature:** tech-lead-dynamic-subteam
**Created:** 2026-05-07

---

## Purpose

Record technical decisions, edge cases, deviations from the plan, and lessons learned as implementation progresses. Update this file whenever a non-obvious decision is made or an unexpected constraint is discovered.

---

## Technical Decisions

[TBD — record decisions as they are made during Phase 5, e.g.:
- Chosen synchronisation primitive for pool allocation serialisation
- Goroutine lifecycle pattern used for spawned bots
- Heartbeat implementation (ticker vs. time.AfterFunc)
- Session file location and naming convention]

---

## Edge Cases & Solutions

[TBD — document edge cases discovered during implementation, e.g.:
- Behaviour when spawn_agent is called with a name that matches a recently terminated bot
- Behaviour when tech-lead context is cancelled while a spawned bot is mid-task
- Behaviour when pool state file is missing on startup (first run)]

---

## Deviations from Plan

[TBD — record any deviations from spec.md or architecture.md, with rationale]

---

## Lessons Learned

[TBD — populate during Phase 6 retrospective]
