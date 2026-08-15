# Status: ACP Harness Feature Parity — Code Review Fixes

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

- [x] Spec directory created (`specs/260815-acp-harness-feature-parity-auto-review/`)
- [x] Review PRD reviewed (`/review-prd`) — verdict: Ready to proceed, no gaps found; P0 calibration confirmed justified
- [x] Research questions identified (see `research.md`) — findings are already precisely located by file/function in the review PRD.
- [x] Phase files initialized (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md)

## Phases 2-4 Task Checklist

- [x] RQ1 (fix mechanism) resolved — reuse `lock.go`'s atomic-publish primitive, retry-with-backoff (not fail-fast), combined with re-read-before-persist.
- [x] RQ2 (merge semantics) resolved — union by item ID, own touched item(s) win, `Reorder`'s true-concurrency case documented as an accepted limitation.
- [x] RQ3 (test template) resolved — `lock_race_test.go`'s deterministic-race-forcing pattern.
- [x] `spec.md` FR-1 updated with concrete design; Risks table corrected.
- [x] `architecture.md` populated with concrete design and 4 recorded architectural decisions.
- [x] `tasks.md` populated with 5-task, 2-phase breakdown.

## Blockers

- None. FR-1's design is fully resolved and concrete; ready for implementation.

## Recent Activity

- 2026-08-15: Spec directory created from `acp-harness-feature-parity-auto-review-PRD.md`; PRD moved into spec directory. This is the first P0 finding in this session's three feature-review cycles — a real, "reachable by construction" cross-process concurrency hazard on shared board state, confirmed against this session's own observation of the live deployment running 10 concurrent ACP-mode processes.
- 2026-08-15: `/review-spec` run; codebase research resolved the fix mechanism concretely — reuse `lock.go`'s existing atomic-publish/stale-check primitive (not a new `flock` dependency), wrap in retry-with-backoff, combine with re-read-and-merge-by-item-ID before each persist. Spec now implementation-ready.
