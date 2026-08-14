# Tasks: Boabot Native Daemon Mode — Code Review Fixes

**Created:** 2026-08-14
**Status:** Planning

## Progress Summary

0/10 tasks complete.

## Phase 1 — P1 fixes (must close before done)

### T-FR101 — Fix relay-replay dedup marking "seen" before dispatch succeeds

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-101. TDD: failing test first (dispatch fails, redispatch same event ID, currently reports `Duplicate`), then fix `markSeenIfDuplicate`'s call site to only mark after success.

### T-FR102 — Fix `buzzBoardTitle` byte-index truncation

- **Depends on:** none (parallelizable with T-FR101, same file different function)
- **Acceptance criteria:** per spec.md FR-102. TDD: failing test first (mid-rune 80-byte instruction, `utf8.ValidString` fails), then rune-safe truncation fix.

### T-FR103 — Test `TeamManager.Run()`'s nil-builder isolation

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-103. TDD: write the `nil`-builder-for-one-persona test first; if it fails against current code, fix the underlying defect; if it passes, it's a pure coverage addition (per review PRD's own note this is the likely outcome).

### T-FR104 — Disclose `chat_provider` behavior change in archived spec's Breaking Changes section

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-104. Docs-only.

## Phase 2 — P2 fixes (independent, parallelizable)

### T-FR105 — Correct implementation-notes.md item #11's stated reason

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-105. Docs-only.

### T-FR106 — Clarify coverage-gate framing

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-106. Docs-only; recommended approach per architecture.md.

### T-FR107 — Revert `LoadTeamConfig` to unexported

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-107. TDD: confirm no external caller via grep first (research.md RQ5), then revert the export and its test.

### T-FR108 — Bring archived spec.md/data-dictionary.md current with implementation-time deviations

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-108, all 4 sub-items. Docs-only.

### T-FR109 — Correct "Slack DMs" claim in adoption-config docs

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-109. Docs-only.

### T-FR110 — Decide and act on `markSeenIfDuplicate`'s lock-scan cost (optional)

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-110. Not required to close this spec; do if time permits.
