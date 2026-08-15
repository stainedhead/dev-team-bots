# Tasks: ACP Harness Feature Parity — Code Review Fixes

**Created:** 2026-08-15
**Status:** Planning

## Progress Summary

5/5 tasks complete (T-FR5 closed via recorded defer decision, per its own "not required to close" acceptance criterion).

## Phase 1 — P0 fix (must close before mergeable)

### T-FR1 — Fix `board.json` cross-process concurrency hazard — COMPLETE

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-1. TDD: failing cross-process test first (two `InMemoryBoardStore` instances, shared `persistPath`, concurrent `Create`/`Update` of distinct items via goroutines with a deterministic race-forcing hook per `lock_race_test.go`'s template, asserting the final on-disk file has both items — fails against current `persist()`), then: (1) extract `lock.go`'s atomic-publish/stale-check primitive into a small reusable helper; (2) wrap it in retry-with-backoff (not fail-fast); (3) modify `persist()` to acquire the lock, re-read `persistPath` from disk, merge by item ID (union, caller's own touched item(s) win), write, release. Existing single-process board tests must keep passing unchanged. `Reorder`'s true concurrent-conflict case documented as an accepted limitation, not solved.
- **Done:** new `internal/infrastructure/local/filelock` package (`AcquireWait`, retry-with-backoff); `board.go`'s `persist()` now acquires `persistPath+".lock"`, re-reads disk, merges by item ID, writes; RED test (`board_concurrency_test.go`) confirmed failing 5/5 against unfixed code, then passing 10/10 after the fix; additional hook-based lock-contention test (`board_race_test.go`) added per the explicit lock_race_test.go-mirroring instruction. All existing single-process board tests pass unchanged. See implementation-notes.md for full detail and recorded deviations.

## Phase 2 — P1/P2 documentation fixes (independent, parallelizable)

### T-FR2 — Correct the "exact mirror" claim's scope inaccuracy — COMPLETE

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-2. Confirm exact current wording in each of 5 locations (archived spec.md, architecture.md, research.md, ADR-B030, adoption doc) before editing; correct all 5 to state ACP mode reads the running persona's own config vs. native mode's team-wide sourcing; add new adoption-doc bullet with remediation guidance (copy settings into the persona's own config).
- **Done:** all 5 locations corrected (`specs/archive/260815-acp-harness-feature-parity/{spec,architecture,research}.md`, `boabot/docs/architectural-decision-record.md` ADR-B030, `boabot/user-docs/ACP-Harness-Adoption-Config.md`); new adoption-doc bullet added with explicit copy-into-persona's-own-config remediation guidance; matching comments added at each gate site in `cmd/boabot/acp.go`.

### T-FR3 — Soften board-gate "exact condition" language — COMPLETE

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-3. Either reword to "equivalent by convention" or add a code comment on `buildACPMCPOptions` explaining the `cfg.Bot.BotType`-matches-directory-name convention.
- **Done:** both — archived spec.md/architecture.md language softened to "equivalent by convention", and a code comment added on `buildACPMCPOptions`'s board-store gate in `cmd/boabot/acp.go` explaining the `cfg.Bot.BotType`-vs-`entry.Type` distinction.

### T-FR4 — Reword adoption-doc overclaiming heading — COMPLETE

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-4. Reword "No filesystem/tool differences from native mode" to not overclaim before its own caveats.
- **Done:** reworded to "Same tool/provider mechanisms as native mode, with scope differences (see below)." — count-free phrasing since T-FR2 adds a third caveat bullet in the same pass.

### T-FR5 — Record decision on gating-logic duplication (optional) — COMPLETE (deferred)

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-5. Not required to close — record the decision (defer, matching precedent) in implementation-notes.md, or implement the shared-function extraction if it turns out to be a small, contained change.
- **Done:** decision recorded in implementation-notes.md — deferred, matching the review's own precedent framing (`main.go`'s `buildBuzzMonitor`/`newBuzzMonitorBuilder`); not implemented, per the reasoning recorded there.
