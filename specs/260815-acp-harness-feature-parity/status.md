# Status: ACP Harness Feature Parity

**Created:** 2026-08-15
**Last Updated:** 2026-08-15

## Overall Progress

| Phase | Description | Status |
|-------|-------------|--------|
| 0 | Initial Research (PRD/Feature Research) | In Progress |
| 1 | Specification (spec.md) | Complete |
| 2 | Research & Data Modeling | Not Started |
| 3 | Architecture & Planning | Not Started |
| 4 | Task Breakdown | Not Started |
| 5 | Implementation | Not Started |
| 6 | Completion & Archival | Not Started |

## Phase 0 Task Checklist

- [x] Spec directory created (`specs/260815-acp-harness-feature-parity/`)
- [x] PRD reviewed (`/review-prd`) — verdict: Ready for spec, no gaps found
- [x] Research questions identified (see `research.md`, seeded from PRD Open Questions + prior code-comparison research)
- [x] Phase files initialized (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md)

## Blockers

- None currently. FR-402's persistence design and the `orchestrator.enabled` reuse question are open design items to resolve during research, not blockers to starting.

## Recent Activity

- 2026-08-15: Spec directory created from `acp-harness-feature-parity-PRD.md`; PRD moved into spec directory. This PRD followed a direct code comparison between ACP mode and native mode's task execution (triggered by observing repeated `exceeded max tool iterations (50)` failures on the live orchestrator persona), which found the two modes share the identical execution loop but differ in provider selection, tool-set wiring, and mid-task question support (the last confirmed infeasible without upstream protocol support and scoped as a Non-Goal).
