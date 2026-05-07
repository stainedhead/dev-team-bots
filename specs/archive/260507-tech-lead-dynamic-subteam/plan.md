# Plan: Tech-Lead Dynamic Subteam and Orchestrator Pool Management

**Feature:** tech-lead-dynamic-subteam
**Created:** 2026-05-07
**Status:** Planning — populate during Phase 3

---

## Development Approach

- Clean Architecture throughout — domain interfaces defined first, infrastructure implementations second
- TDD (Red → Green → Refactor) for every component
- Parallel workstreams where possible: subteam spawning and pool management are largely independent and can be implemented concurrently

---

## Phase Breakdown

[TBD — populate during Phase 3]

| Phase | Description | Key Deliverables |
|-------|-------------|-----------------|
| Phase 2 | Research & Data Modeling | Research questions answered; interfaces and entities finalised in data-dictionary.md |
| Phase 3 | Architecture & Planning | Sequence diagrams complete; component boundaries confirmed; plan.md and tasks.md populated |
| Phase 4 | Task Breakdown | tasks.md: all tasks with estimates, dependencies, acceptance criteria |
| Phase 5a | Domain & Interfaces | `SubTeamManager`, `TechLeadPool`, entities, enums |
| Phase 5b | Infrastructure — ScopedBus | `NewScopedBus()`, isolation tests |
| Phase 5c | Infrastructure — Persistence | `SessionFile`, `PoolStateFile`, atomic write tests |
| Phase 5d | Application — Tech-lead | `SpawnAgent`, `TerminateAgent`, `HeartbeatMonitor` use cases + tests |
| Phase 5e | Application — Orchestrator | `AllocateTechLead`, `DeallocateTechLead`, `ReconcilePool` use cases + tests |
| Phase 5f | Wire-up & Integration | Tool registration, queue router, REST API, board watcher |
| Phase 6 | Docs & Archival | Docs updated; spec archived |

---

## Critical Path

[TBD — identify the longest dependency chain once tasks are broken down]

---

## Testing Strategy

- Unit tests: all domain and application layer components mocked at adapter seams; ≥90% coverage
- Isolation tests: explicit test coverage on ScopedBus to verify no message leakage between sessions
- Atomic write tests: simulate process kill mid-write; verify previous valid state preserved
- Race tests: all tests run with `-race` flag
- Integration tests: [TBD — identify integration test surface, e.g. real queue router behaviour]

---

## Rollout Strategy

- No feature flags — the pool management and subteam spawning are wired in together
- Orchestrator will start with one warm standby tech-lead instance on startup
- No data migration required — pool state file and session files are created fresh on first use

---

## Success Metrics

- All acceptance criteria in spec.md pass
- Coverage ≥ 90% on domain and application packages
- `go vet` and `golangci-lint` clean
- All tests pass with `-race`
- No goroutine leaks confirmed by test goroutine count assertions
