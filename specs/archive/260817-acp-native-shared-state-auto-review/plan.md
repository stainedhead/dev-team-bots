# Plan: ACP/Native Shared-State Parity — Review Fixes

**Created:** 2026-08-17
**Status:** Planning

## Development Approach

TDD per finding, sequential (both findings are independent but small enough that parallelizing across agents/worktrees would add more coordination overhead than it saves — per the review PRD's own guidance).

## Phase Breakdown

1. FR-R2: write a failing test asserting a malformed marker logs a distinct warning, confirm red, implement, confirm green.
2. FR-R1: edit `implementation-notes.md` (documentation only, no test).

## Testing Strategy

`sharedstate_test.go` gains one new test case. No other package is affected.

## Rollout Strategy

Included in the same PR as the parent feature (single branch, single PR, per this repo's dev-flow convention).
