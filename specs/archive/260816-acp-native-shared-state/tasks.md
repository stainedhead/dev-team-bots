# Tasks: ACP/Native Shared State and Task-Layer Parity

**Created:** 2026-08-16
**Status:** Planning

## Progress Summary

5/5 tasks complete

## Phase 1 — Shared-State Config (FR-501/FR-502)

- **P1.1** — [x] Fix pre-existing cross-process clobber bug in `ChatStore`/`DirectTaskStore` (found during research, blocking dependency for FR-502/503/504a since this feature makes both stores genuinely shared). Commit `2c0ea24`.
- **P1.2** — [x] Add shared-state owner marker (`sharedstate.EnsureOwner`), wired into both native mode and ACP mode. FR-501 redesigned per implementation-notes.md — see spec.md's revised FR-501/AC1. Commit `8758dc3`.

## Phase 2 — ChatStore Wiring (FR-503)

- **P2.1** — [x] Wire `ChatStore` construction into `acp.go`, gated/degrading like the existing board/plugin store pattern. Commit `9c2b3b1`.
- **P2.2** — [x] Wire history replay into `turn.go`'s `Prompt` handler, keyed by a conversation ID derived from `extractChannelID` with a session-ID fallback for DM-scoped turns. Commit `818cff8`.

## Phase 3 — Scheduling + DirectTask Creation (FR-504/FR-504a)

- **P3.1** — [x] Add `domain.DirectTaskSourceACP` constant. Commit `818cff8`.
- **P3.2** — [x] Wire `ChatTaskManager.DetectAndHandle` as a narrow pre-check in `turn.go`'s `Prompt` handler (confirmed synchronous, no live Dispatcher required). `NoImmediateDispatchQueue` handles the one unsupported case (ASAP/immediate delegation) gracefully. Commits `818cff8`, `9c2b3b1`.
- **P3.3** — [x] Create a `DirectTask`/board item for every ACP-dispatched task (not just scheduled ones), updated to its final status/output when `worker.Execute` completes. Commit `818cff8`.

## Phase 4 — Heap Watchdog (FR-505)

- **P4.1** — [x] Wire `watchdog.New` into ACP mode's process lifecycle from persona `heap_warn_mb`/`heap_hard_mb` config, mirroring `team_manager.go`'s gate/shutdown-via-cancel pattern. Commit `9c2b3b1`.

All Step 3 (Implement Product) work complete: full module test suite green (including `-race`), 92.1% domain+application coverage (unchanged from 92.2% baseline), `golangci-lint` clean. Also fixed two pre-existing bugs found as blocking dependencies (ChatStore/DirectTaskStore concurrency, TeamManager.cancel race) — see implementation-notes.md.
