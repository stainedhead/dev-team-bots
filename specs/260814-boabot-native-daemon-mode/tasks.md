# Tasks: Boabot Native Daemon Mode (Multi-Agent Buzz Support)

**Created:** 2026-08-14
**Status:** Planning

## Progress Summary

0/0 tasks complete (task breakdown pending Phase 4, after research/architecture finalize).

## Phase 1 (stub — expand during Phase 4 task breakdown)

### P1.1 — Per-bot Buzz monitor factory

- **ID:** P1.1
- **Depends on:** Research Question 2 resolved
- **Estimated duration:** [TBD]
- **Acceptance criteria:** `main.go` constructs one `buzzinfra.Monitor` per Buzz-enabled persona (from `team.yaml`/`config.yaml`), each registered via its own `WithChannelMonitor(...)` call on the shared `TeamManager`; existing single-identity behavior preserved for a team with exactly one Buzz-enabled bot (regression safety).
