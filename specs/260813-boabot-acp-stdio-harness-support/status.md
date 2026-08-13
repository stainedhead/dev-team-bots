# Status: BaoBot ACP Stdio Harness Support

**Feature:** boabot-acp-stdio-harness-support
**Created:** 2026-08-13

## Overall Progress

| Phase | Status |
|---|---|
| Phase 0 — Initial Research | Complete |
| Phase 1 — Specification | Complete |
| Phase 2 — Research & Data Modeling | Complete |
| Phase 3 — Architecture & Planning | Complete |
| Phase 4 — Task Breakdown | Complete |
| Phase 5 — Implementation | In Progress (0/8 tasks) |
| Phase 6 — Completion & Archival | Not Started |

## Phase 5 Task Checklist

- [ ] T1 — Dependency + skeleton
- [ ] T2 — Handshake (`initialize`, `session/new`)
- [ ] T3 — Core turn execution (`session/prompt`)
- [ ] T4 — Keep-alive + `session/cancel`
- [ ] T5 — Budget/usage wiring
- [ ] T6 — Process lifecycle + panic recovery
- [ ] T7 — Real `buzz-acp` integration test
- [ ] T8 — Documentation (ADR + docs)

## Blockers

None currently. `buzz-acp --mcp-command` semantics remain unresolved (research.md) but are non-blocking for T1–T8.

## Recent Activity

- 2026-08-13: PRD authored, spec directory created, all phase files initialized.
- 2026-08-13: Research completed — confirmed `github.com/coder/acp-go-sdk` as the Go ACP SDK, confirmed `buzz-acp`'s real method set and persistent pooled-process lifecycle via `strings` on the actual binary.
- 2026-08-13: Architecture and data-dictionary finalized — thin adapter over existing `Worker`/`BudgetTracker`, no new domain interfaces. FR-003 refined from "incremental streaming" (not buildable — `Worker.Execute` has no streaming callback) to "keep-alive updates + final result," which also turned out to be a correctness requirement for `buzz-acp`'s idle-timeout, not just a UX nicety.
- 2026-08-13: Task breakdown (T1–T8) written in tasks.md. Beginning implementation.
