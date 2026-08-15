# Tasks: ACP Harness Feature Parity — Code Review Fixes

**Created:** 2026-08-15
**Status:** Planning

## Progress Summary

0/5 tasks complete.

## Phase 1 — P0 fix (must close before mergeable)

### T-FR1 — Fix `board.json` cross-process concurrency hazard

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-1. TDD: failing cross-process test first (two `InMemoryBoardStore` instances, shared `persistPath`, concurrent `Create`/`Update` of distinct items via goroutines with a deterministic race-forcing hook per `lock_race_test.go`'s template, asserting the final on-disk file has both items — fails against current `persist()`), then: (1) extract `lock.go`'s atomic-publish/stale-check primitive into a small reusable helper; (2) wrap it in retry-with-backoff (not fail-fast); (3) modify `persist()` to acquire the lock, re-read `persistPath` from disk, merge by item ID (union, caller's own touched item(s) win), write, release. Existing single-process board tests must keep passing unchanged. `Reorder`'s true concurrent-conflict case documented as an accepted limitation, not solved.

## Phase 2 — P1/P2 documentation fixes (independent, parallelizable)

### T-FR2 — Correct the "exact mirror" claim's scope inaccuracy

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-2. Confirm exact current wording in each of 5 locations (archived spec.md, architecture.md, research.md, ADR-B030, adoption doc) before editing; correct all 5 to state ACP mode reads the running persona's own config vs. native mode's team-wide sourcing; add new adoption-doc bullet with remediation guidance (copy settings into the persona's own config).

### T-FR3 — Soften board-gate "exact condition" language

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-3. Either reword to "equivalent by convention" or add a code comment on `buildACPMCPOptions` explaining the `cfg.Bot.BotType`-matches-directory-name convention.

### T-FR4 — Reword adoption-doc overclaiming heading

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-4. Reword "No filesystem/tool differences from native mode" to not overclaim before its own caveats.

### T-FR5 — Record decision on gating-logic duplication (optional)

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-5. Not required to close — record the decision (defer, matching precedent) in implementation-notes.md, or implement the shared-function extraction if it turns out to be a small, contained change.
