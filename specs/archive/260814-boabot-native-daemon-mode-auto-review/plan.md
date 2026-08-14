# Plan: Boabot Native Daemon Mode — Code Review Fixes

**Created:** 2026-08-14
**Status:** Planning

## Development Approach

TDD (Red → Green → Refactor) for every code fix (FR-101, FR-102, FR-103), per AGENTS.md. Documentation fixes (FR-104, FR-105, FR-106, FR-108, FR-109) are verified against current code/CI before being written, matching the original review's own verification discipline. A brief code/design self-review after each fix before moving to the next, per the review PRD's Implementation Guidance.

## Phase Breakdown

Given the review PRD already precisely locates every finding, this spec's Research/Data Modeling/Architecture phases are intentionally thin (see research.md, data-dictionary.md, architecture.md) — most of the work is direct implementation against already-known locations.

1. P1 fixes (FR-101, FR-102, FR-103) — TDD code fixes.
2. P1 doc fix (FR-104) — Breaking Changes disclosure.
3. P2 fixes — FR-105 through FR-110, independent, can run in parallel via worktrees/agent teammates per the review PRD's guidance.

## Critical Path

FR-101 and FR-102 can proceed in parallel (same file, different functions, per the review PRD's own note). FR-103 is fully independent. FR-104–FR-109 (docs) are independent of all code fixes and each other. FR-110 is optional.

## Testing Strategy

- FR-101: regression test — dispatch fails, redispatch same event ID, assert retried not `Duplicate`.
- FR-102: table test covering mid-rune boundary, empty input, ASCII-only — assert `utf8.ValidString`.
- FR-103: `nil`-builder-for-one-persona test at the `TeamManager.Run()` integration point.
- Full suite + `-race -gcflags=all=-d=checkptr=0` + `golangci-lint` + `gofmt` + `go vet` after each fix, per the review PRD's Implementation Guidance.

## Rollout Strategy

Same branch, same PR (not yet opened) as the original feature — these are fixes to unreleased work, not a separate release.

## Success Metrics

All FR-101–FR-109 acceptance criteria in spec.md met (FR-110 optional, per review PRD).
