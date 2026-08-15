# Plan: ACP Harness Feature Parity — Code Review Fixes

**Created:** 2026-08-15
**Status:** Planning

## Development Approach

TDD for FR-1 (the P0 code fix) — failing cross-process regression test first, per `research.md`'s resolved design. Documentation-only fixes (FR-2/FR-3/FR-4) verified against current code before being written. Brief self-review after each fix.

## Phase Breakdown

1. FR-1 (P0) — must close before this branch is mergeable. Extract lock helper → add re-read-merge to `persist()` → TDD cross-process test.
2. FR-2 (P1) — documentation accuracy correction across 5 locations.
3. FR-3/FR-4 (P2) — independent, low-risk documentation fixes.
4. FR-5 (P2, optional) — decision recorded, not required.

## Critical Path

FR-1 is the only code change and the only true blocker — do it first, in isolation, with its own full quality-gate pass before touching anything else. FR-2 through FR-5 are all independent of FR-1 and of each other (different files), can be done in any order or parallel via worktrees/agent teammates afterward.

## Testing Strategy

- FR-1: new cross-process test (two `InMemoryBoardStore` instances, shared `persistPath`, concurrent `Create`/`Update`, deterministic race-forcing hook per `lock_race_test.go`'s template) — fails before the fix, passes after. Existing single-process board tests must keep passing unchanged (regression check for native mode's usage).
- FR-2–FR-5: no test changes (documentation-only), except FR-5 if pursued.

## Rollout Strategy

Same branch, same PR (not yet opened) as this dev-flow run's feature.

## Success Metrics

All FR-1–FR-4 acceptance criteria in spec.md met (FR-5 optional).
