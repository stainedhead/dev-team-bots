# Status: Buzz DM Support and Full Threaded-Reply Support

**Created:** 2026-08-14
**Last Updated:** 2026-08-14

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

- [x] Spec directory created (`specs/260814-buzz-dm-and-thread-support/`)
- [x] PRD reviewed (`/review-prd`) — verdict: Ready for spec (one gap fixed: missing team-dependency row for FR-204's decision)
- [x] Research questions identified (see `research.md`, seeded from PRD Open Questions + prior audit findings)
- [x] Phase files initialized (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md)

## Phases 2-4 Task Checklist

- [x] RQ1 (conversation continuation) resolved — reuse `ChatStore` history replay, exactly matching the web-UI chat path's pattern; no new session machinery.
- [x] RQ2 (`nip44`/`nip59` API shape) resolved — use the higher-level `nip17` package the vendored library already provides, not raw `nip44`/`nip59` calls.
- [x] RQ4 (`ThreadID` keying fix) resolved — one-argument fix at `monitor.go:484` (pass `root` not `channelUUID`), no `pendingMap` restructure.
- [x] RQ5 (thread-continuation trigger flow) resolved — new `triggerThreadReply` kind, `BuzzTaskBridge.KnownThread` state tracking.
- [x] RQ3 (relay NIP-17 support) explicitly deferred to implementation-time verification — documented as non-blocking.
- [x] `data-dictionary.md`, `architecture.md` populated with concrete design and 5 recorded architectural decisions.
- [x] `tasks.md` populated with 10-task Phase 1-3 breakdown, TDD-first, dependency-ordered.
- [x] `spec.md` Scope of Changes updated with real file paths.

## Blockers

- None currently. FR-204's unauthorized-DM-handling decision (silent ignore vs. decline reply) remains an open operator call — does not block starting implementation, only finalizing that one FR's exact behavior.

## Recent Activity

- 2026-08-14: Spec directory created from `buzz-dm-and-thread-support-PRD.md`; PRD moved into spec directory. This PRD followed a live code audit (channel mentions confirmed working, threading confirmed partially implemented with specific gaps, DM confirmed unimplemented with architecture ready) plus two explicit product decisions from the user (DM tasks visible on shared board; in-thread replies continue same task).
- 2026-08-14: `/review-spec` run; codebase research resolved RQ1/RQ2/RQ4/RQ5 concretely (biggest finding: a ready-made `nip17` package already exists in the vendored dependency, and conversation continuation can reuse the existing `ChatStore` pattern with zero new session machinery). research.md, data-dictionary.md, architecture.md, tasks.md, plan.md, spec.md updated with findings. Spec now implementation-ready.
