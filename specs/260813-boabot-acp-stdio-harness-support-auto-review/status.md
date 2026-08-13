# Status: BaoBot ACP Stdio Harness Support — Auto-Review Fixes

**Feature:** boabot-acp-stdio-harness-support-auto-review
**Created:** 2026-08-13

## Overall Progress

| Phase | Status |
|---|---|
| Phase 0 — Initial Research | Complete (review findings are the research) |
| Phase 1 — Specification | Complete |
| Phase 2 — Research & Data Modeling | Complete |
| Phase 3 — Architecture & Planning | Complete |
| Phase 4 — Task Breakdown | Complete |
| Phase 5 — Implementation | In Progress (4/8 tasks) |
| Phase 6 — Completion & Archival | Not Started |

## Phase 5 Task Checklist

- [x] RT1 (FR-001, P0) — data race fixed via `turnMu` serialization; proven with `TestAgent_Prompt_ConcurrentSessionsDoNotRace`.
- [x] RT2 (FR-002 + FR-003, P0) — turn-level `slog` logging added; doc's log-line claim now real, verified against the compiled binary.
- [ ] RT3 (FR-004, P1) — `RulesTracker` wiring.
- [x] RT4 (FR-005, P1) — resolved as a side effect of RT1's serialization; proven with a dedicated test.
- [ ] RT5 (FR-006, P1) — session-map bound.
- [ ] RT6 (FR-007, P2) — `go mod tidy`.
- [ ] RT7 (FR-008, P2) — integration tests in CI, or documented gap.
- [ ] RT8 (FR-009, P2) — stdin-EOF exit test.

## Blockers

None.

## Recent Activity

- 2026-08-13: RT1/RT2/RT4 (all P0, plus one P1 resolved as a side effect) complete. Moving to remaining P1s (RT3, RT5).
- 2026-08-13: Spec directory created from the auto-review PRD (9 findings: 3 P0, 3 P1, 3 P2).
