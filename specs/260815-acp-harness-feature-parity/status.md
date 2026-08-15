# Status: ACP Harness Feature Parity

**Created:** 2026-08-15
**Last Updated:** 2026-08-15

## Overall Progress

| Phase | Description | Status |
|-------|-------------|--------|
| 0 | Initial Research (PRD/Feature Research) | Complete |
| 1 | Specification (spec.md) | Complete |
| 2 | Research & Data Modeling | Complete |
| 3 | Architecture & Planning | Complete |
| 4 | Task Breakdown | Complete |
| 5 | Implementation | Not Started |
| 6 | Completion & Archival | Not Started |

## Phase 0 Task Checklist

- [x] Spec directory created (`specs/260815-acp-harness-feature-parity/`)
- [x] PRD reviewed (`/review-prd`) — verdict: Ready for spec, no gaps found
- [x] Research questions identified (see `research.md`, seeded from PRD Open Questions + prior code-comparison research)
- [x] Phase files initialized (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md)

## Phases 2-4 Task Checklist

- [x] RQ1 (board persistence path) resolved — reuse `memPath` already computed in `acp.go:103`, board store at `<memPath>/board.json`.
- [x] RQ2 (config flag reuse) resolved — do NOT reuse `orchestrator.enabled`; gate each feature on its own granular field instead.
- [x] RQ3 (native mode's exact wiring conditions) resolved — `Bot.Type != "tech-lead"` (board), `Orchestrator.Plugins.InstallDir != ""` (plugin), unconditional runner + per-tool `Enabled` (CLI).
- [x] RQ4 (`RulesTracker` precedent) resolved — confirms this pattern is already established/shipped in `acp.go`, not a new idea.
- [x] `spec.md` FR-402/404/405 updated with concrete gating conditions; Breaking Changes and Risks table corrected.
- [x] `architecture.md` populated with concrete design and 3 recorded architectural decisions.
- [x] `tasks.md` populated with 6-task, 5-phase breakdown.

## Blockers

- None currently. All research questions resolved; ready for implementation.

## Recent Activity

- 2026-08-15: Spec directory created from `acp-harness-feature-parity-PRD.md`; PRD moved into spec directory. This PRD followed a direct code comparison between ACP mode and native mode's task execution (triggered by observing repeated `exceeded max tool iterations (50)` failures on the live orchestrator persona), which found the two modes share the identical execution loop but differ in provider selection, tool-set wiring, and mid-task question support (the last confirmed infeasible without upstream protocol support and scoped as a Non-Goal).
- 2026-08-15: `/review-spec` run; codebase research resolved all 4 research questions concretely, most notably determining `orchestrator.enabled` should NOT be reused as the tool-wiring signal (it only means "start the dashboard" and doesn't even gate board wiring in native mode). Spec now implementation-ready with exact, precedent-matched gating conditions for every FR.
