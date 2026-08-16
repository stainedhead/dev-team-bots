# Implementation Notes: ACP/Native Shared-State Parity — Review Fixes

**Created:** 2026-08-17

## Purpose

Running log of technical decisions, edge cases, and deviations discovered during implementation of the review-fix cycle.

## Technical Decisions

- FR-R2's warning distinguishes the malformed-content case from the unmarshal-succeeded-but-empty-owner case only by log message text ("malformed owner marker encountered"), not by a separate code path — both are tolerated identically (treated as unclaimed) since the practical operator action is the same either way: check why the marker file looks wrong.

## Edge Cases & Solutions

- The new test (`TestEnsureOwner_MalformedMarker_LogsDistinctWarningAndReclaims`) seeds genuinely invalid JSON (`"not valid json"`) rather than an empty-owner-but-valid-JSON marker, since that's the more realistic corruption scenario (a torn write) and exercises the `unmarshalErr != nil` branch directly.

## Deviations from Plan

None — both findings implemented exactly as scoped in spec.md.

## Lessons Learned

- A review-fix cycle this small (two independent P2 items, no shared files beyond one package) is fastest done sequentially by one agent — matches the review PRD's own guidance against unnecessary worktree/teammate parallelization for small fix sets.
