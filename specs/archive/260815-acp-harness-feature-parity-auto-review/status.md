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
| 5 | Implementation | Complete (5/5 tasks) |
| 6 | Completion & Archival | Complete (archived at dev-flow Step 10) |

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
- 2026-08-15: T-FR1 (P0) implemented and verified — new `internal/infrastructure/local/filelock` package + `board.go` persist() lock+reread+merge. TDD followed: RED test written and confirmed failing 5/5 against unfixed `persist()` before any production code touched, then GREEN 10/10 after the fix, plus an additional hook-based lock-contention test. Full suite (`go test -race -gcflags=all=-d=checkptr=0 ./...`), `go vet`, `golangci-lint run` all clean; domain+application aggregate coverage re-measured at 92.2%, matching the pre-existing baseline exactly (no regression). Existing single-process board tests pass unchanged. Committed as `eba1ca7` and pushed.
- 2026-08-15: T-FR2/T-FR3/T-FR4 implemented (documentation-accuracy fixes) and T-FR5 closed via a recorded defer decision — all 5 tasks now complete. Full suite/vet/lint re-verified clean after the `cmd/boabot/acp.go` comment edits.
