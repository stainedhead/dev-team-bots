# Status: Buzz DM/Thread Support — Code Review Fixes

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
| 5 | Implementation | Complete (6/6 tasks) |
| 6 | Completion & Archival | Complete (archived at dev-flow Step 10) |

## Phase 0 Task Checklist

- [x] Spec directory created (`specs/260814-buzz-dm-and-thread-support-auto-review/`)
- [x] Review PRD reviewed (`/review-prd`) — verdict: Ready to proceed (no gaps found; guidance and P0 calibration both pass)
- [x] Research questions identified (see `research.md`) — findings are already precisely located by file/function in the review PRD.
- [x] Phase files initialized (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md)

## Phase 5 Task Checklist

- [x] T-FR301 (P1) — Buzz task reply duplicate chat-store write fixed (option a). TDD: failing test first, then fix. `go test -race`/`golangci-lint`/`go vet` clean.
- [x] T-FR302 (P2) — `dispatchedThreads` unbounded growth — documented as accepted, no code change.
- [x] T-FR303 (P2) — DM pre-gate crypto-cost ordering — documented as accepted, no code change.
- [x] T-FR304 (P2) — startup warning for fail-open DM gate — fixed. TDD: failing test first, then fix.
- [x] T-FR305 (P2) — FR-207 wording tightening (docs, done) + `publishReply` single-`p`-tag scope (documented as accepted, no code change).
- [x] T-FR306 (P2) — dispatch-failure rollback asymmetry documented in the archived spec's implementation-notes.md.

## Blockers

- None. All 6 findings are precisely scoped by the review; the two open design branches (FR-301's option a/b, FR-303's fix-vs-document) are resolvable during implementation without further owner input.

## Recent Activity

- 2026-08-15: Spec directory created from `buzz-dm-and-thread-support-auto-review-PRD.md`; PRD moved into spec directory.
- 2026-08-15: T-FR301 (P1) closed — `chatMessageThreadID` now passes the real Buzz ThreadID through for `DirectTaskSourceBuzz` tasks; `Monitor.recordOutbound` moved off the task-completion path (HandleResult no longer double-writes). TDD: failing test first (`TestHandleSharedTaskResult_BuzzTask_RecordsExactlyOneMessage`, `TestMonitor_HandleResult_DoesNotRecordChatOutbound`), then fix. Full suite green, `-race`, `golangci-lint`, `go vet` clean. Domain+application aggregate coverage 92.16% (up from 91.4% baseline, no regression).
- 2026-08-15: P2 batch closed (T-FR302 through T-FR306). FR-304 fixed with TDD (`TestMonitor_StartDMSubscription_GateInactive_WarnsFailOpen`/`_GateActive_NoWarning`); FR-302/303/305(b) documented as accepted (matched the PRD's own "default toward documenting as accepted" framing — no concrete reason found to deviate); FR-305(a) FR-207 wording tightened in the archived spec.md; FR-306 rollback-asymmetry sentence added to the archived implementation-notes.md. Full suite green, `-race`, `golangci-lint`, `go vet` clean. Domain+application aggregate coverage unchanged at 92.16% (FR-304's fix is in `internal/infrastructure/buzz`, outside the measured aggregate). All 6 findings from the review PRD now have a corresponding commit or documented-no-code-change note, checked off against the commit log per AGENTS.md's dev-flow Step 9 instruction.
- 2026-08-15: Self-review follow-up (post-advisor pass) — found and closed two items before declaring done: (1) FR-301's fix silently changed a failed Buzz task's recorded ChatStore content from `Monitor.HandleResult`'s normalized text (falls back to `p.Error`/"(no output)") to raw `p.Output` verbatim (empty on this exact input shape). Investigated and accepted as-is, out of FR-301's scope, since normalizing in the shared `handleSharedTaskResult` would change recording behavior for every `DirectTaskSource`, not just Buzz (the exact risk spec.md's own risk table flags) — pinned by a new regression test (`TestHandleSharedTaskResult_BuzzTask_ErrorOnly_RecordsRawOutputVerbatim`) and documented in implementation-notes.md so it's a recorded decision, not a silent regression. (2) The archived spec's Deviation 5 (`specs/archive/260814-buzz-dm-and-thread-support/implementation-notes.md`) still asserted the pre-fix duplicate-write behavior as current; annotated both the finding and its "accepted as-is" conclusion as superseded, with a pointer to this spec's resolution, leaving the original historical reasoning intact. Full gates re-run clean after both fixes; coverage unchanged at 92.16%.
