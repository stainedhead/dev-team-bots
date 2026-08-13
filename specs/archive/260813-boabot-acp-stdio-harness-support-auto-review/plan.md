# Plan: BaoBot ACP Stdio Harness Support — Auto-Review Fixes

**Created:** 2026-08-13
**Status:** Planning

## Development Approach

TDD (red-green-refactor) for every fix. P0 first, then P1, then P2, per AGENTS.md.

## Phase Breakdown

1. **P0 fixes** (FR-001, FR-002, FR-003) — the data race, missing logging, and the doc claim it invalidates. FR-002/FR-003 are naturally done together (adding the log line both satisfies FR-002 and makes FR-003's doc claim true).
2. **P1 fixes** (FR-004, FR-005, FR-006) — independent of each other and of the P0 fixes; good candidates for parallel agent teammates per the review PRD's process guidance.
3. **P2 fixes** (FR-007, FR-008, FR-009) — independent hygiene items; also good parallel-teammate candidates.
4. **Final verification** — full regression suite, coverage check, lint.

## Critical Path

FR-001 is the only fix with real design risk (three valid approaches); everything else is comparatively mechanical. Start there.

## Testing Strategy

- FR-001: a new test driving two concurrent `session/prompt` calls on separate sessions of one `Agent`, run under `-race`, proving no race and correct per-session progress routing.
- FR-002: assert on log output (or at minimum that the code path is exercised) for turn start/end/cancel/panic.
- FR-005: a same-session-ID overlapping-turn test.
- FR-006: a test proving the session map doesn't grow unboundedly (exact mechanism depends on the fix — e.g. an LRU bound or explicit eviction).
- FR-009: a real subprocess test closing stdin and asserting clean exit.

## Rollout Strategy

Same branch (`feat/boabot-acp-stdio-harness`), no separate rollout — these are pre-merge fixes.

## Success Metrics

See spec.md's Success Criteria and Acceptance Criteria.
