# Spec: Boabot Native Daemon Mode — Code Review Fixes

**Created:** 2026-08-14
**Status:** Draft
**Source PRD:** [boabot-native-daemon-mode-auto-review-PRD.md](./boabot-native-daemon-mode-auto-review-PRD.md)

## Executive Summary

Closes the 10 findings (0 P0 / 4 P1 / 6 P2) from the code-and-design review of the boabot native daemon mode multi-agent Buzz support feature (`specs/archive/260814-boabot-native-daemon-mode/`). Overall assessment was "Approve with minor comments" — nothing here blocks merge on its own, but the 4 P1 findings should be closed before this is considered done. Four findings are real code fixes (2 correctness bugs, 1 test gap, plus a doc-driven behavior-disclosure fix); six are documentation-only.

## Problem Statement

The independently-verified code review of the Buzz multi-agent feature found: a relay-replay dedup bug that can permanently and silently drop a user's Buzz instruction on transient dispatch failure (FR-101); a UTF-8 truncation bug that corrupts non-ASCII Kanban board titles (FR-102); an untested integration point for the exact "incomplete Buzz config fails in isolation" edge case spec.md names by name (FR-103); an undisclosed behavior change where the `chat_provider` config setting silently goes live for existing chat-sourced deployments as a side effect of this feature (FR-104); plus six lower-priority documentation-accuracy and completeness gaps (FR-105–FR-110).

## Goals

- Close all 4 P1 findings (FR-101–FR-104) with TDD-first fixes or documentation corrections as specified.
- Close as many of the 6 P2 findings (FR-105–FR-110) as practical; each is independent and low-risk.
- Resolve the 3 embedded open decisions (FR-106, FR-107, FR-108-item-1) per the review PRD's "Open Decisions" section recommendations.
- Every fix gets a failing test first (TDD), per AGENTS.md.

## Non-Goals

- Not re-litigating the review's severity calibration — 0 P0 was independently verified and justified; this spec does not second-guess that.
- Not expanding scope beyond the 10 findings — no new features, no unrelated refactoring.
- Not touching `internal/infrastructure/acp` (ACP mode) — untouched by the original feature, stays untouched here.
- Not implementing Buzz DM support — still out of scope per the original feature's Non-Goals; the review confirmed zero DM-related touches exist to undo.

## User Requirements / Functional Requirements

**FR-101 (P1):** `BuzzTaskBridge`'s relay-replay dedup (`markSeenIfDuplicate`) only marks a Nostr event ID as "seen" after the underlying dispatch has actually succeeded, not before. A dispatch that fails and is later redelivered by the relay must be retried, not silently dropped as a false "duplicate."

**FR-102 (P1):** `buzzBoardTitle`'s title truncation is rune-safe (uses `[]rune` or `utf8`-aware slicing), not byte-index slicing, so multi-byte Buzz instructions never produce invalid UTF-8 in a persisted `WorkItem.Title`.

**FR-103 (P1):** `TeamManager.Run()`'s Buzz-monitor-builder loop has a test where the builder returns `nil` for one Buzz-enabled persona and a working monitor for another, asserting the `nil` persona is skipped without affecting the other persona's monitor or `Run()`'s overall success — the exact integration point spec.md's Edge Cases section names by name.

**FR-104 (P1):** `spec.md`'s Breaking Changes section (in the archived original spec) states explicitly that operators with `models.chat_provider` configured will see it apply to chat-sourced tasks for the first time after this upgrade (previously inert due to a pre-existing dead-code bug this feature incidentally fixed).

**FR-105 (P2):** `implementation-notes.md` item #11's stated reason for testing Buzz concurrency outside `internal/infrastructure/buzz` is corrected to the accurate one (isolating the guarantee from relay-layer noise; CI's `-gcflags=all=-d=checkptr=0` already avoids the underlying crash).

**FR-106 (P2):** Coverage-gate framing clarified — per the review PRD's recommendation, document that AGENTS.md/README's coverage bar is an aggregate across domain+application packages (matching what CI actually enforces), not a strict per-package minimum.

**FR-107 (P2):** `LoadTeamConfig` reverted to unexported `loadTeamConfig` (per the review PRD's recommendation — the rationale for exporting it no longer applies to the final design).

**FR-108 (P2):** `spec.md`/`data-dictionary.md` (in the archived original spec) brought current with implementation-time deviations: AC checkboxes checked with test references or left unchecked with a reason; FR-008 text corrected to state actual shipped scope; `monitor.go` deviation from "Scope of Changes" acknowledged; `TaskPayload.Source` added to `data-dictionary.md`.

**FR-109 (P2):** `chat_provider` adoption docs (`Claude-Adoption-Config.md`, `OpenAI-Adoption-Config.md`) corrected to remove the inaccurate "Slack DMs" claim.

**FR-110 (P2):** `BuzzTaskBridge.markSeenIfDuplicate`'s full-map-scan-under-lock is either accepted as-is with a documenting comment, or changed to periodic eviction — reviewer's call, not required to close this spec.

## Non-Functional Requirements

- **Correctness:** FR-101 and FR-102 are data-integrity bugs (message loss, string corruption respectively) — fixes must be verified by tests that reproduce the original failure mode before the fix, per TDD.
- **Reliability:** FR-103's fix must not change `Run()`'s isolation behavior, only add test coverage proving it (unless the new test reveals an actual defect, in which case fix it).
- **Documentation accuracy:** FR-104, FR-105, FR-108, FR-109 are all "make the docs match reality" fixes — no code risk, but must be verified against actual current code/CI behavior, not just re-asserted.
- **No regressions:** All existing tests, `-race` (with CI's `-gcflags=all=-d=checkptr=0` flag), `golangci-lint`, `go vet`, `gofmt` must stay clean throughout.

## System Architecture

No new components. All fixes are localized to files already touched by the original feature:
- `boabot/internal/application/orchestrator/buzz_task_bridge.go` (FR-101, FR-102, FR-110)
- `boabot/internal/application/team/team_manager.go` (FR-103, FR-107)
- `boabot/internal/application/team/team_manager_test.go` or equivalent test file (FR-103, FR-107)
- `specs/archive/260814-boabot-native-daemon-mode/spec.md`, `data-dictionary.md`, `implementation-notes.md` (FR-104, FR-105, FR-108)
- `boabot/docs/architectural-decision-record.md` or `AGENTS.md`/`README.md` (FR-106, per chosen option)
- `boabot/user-docs/Claude-Adoption-Config.md`, `OpenAI-Adoption-Config.md` (FR-109)

## Scope of Changes

- Files to modify: see System Architecture above — no new files expected except possibly a new test file if `team_manager_test.go` doesn't already have room for FR-103's addition (TBD at task breakdown).
- Dependencies: none new — this is a fix-forward spec against existing, already-merged-to-branch code.

## Breaking Changes

None from these fixes themselves. FR-104 documents a breaking-adjacent behavior change that shipped in the *original* feature (not this fix spec) — this spec's job is to disclose it accurately, not to introduce or revert it.

## Success Criteria and Acceptance Criteria

- [ ] FR-101: new regression test dispatches a failing event ID, then redispatches the same ID, and asserts it is retried (not reported `Duplicate`).
- [ ] FR-102: new tests cover a mid-rune 80-byte truncation boundary (`utf8.ValidString` assertion), the empty-instruction fallback, and unchanged ASCII truncation; `buzzBoardTitle` at 100% coverage.
- [ ] FR-103: new test with a `nil`-returning builder for one persona and a working builder for another; `mon == nil` branch at 100% coverage.
- [ ] FR-104: archived spec.md's Breaking Changes section states the `chat_provider` behavior change explicitly.
- [ ] FR-105: implementation-notes.md item #11 corrected.
- [ ] FR-106: coverage-gate framing decision made and documented (recommendation: clarify docs, not chase coverage).
- [ ] FR-107: `LoadTeamConfig` reverted to unexported (recommendation), or rationale updated if kept exported.
- [ ] FR-108: all four sub-items addressed (AC checkboxes, FR-008 text, monitor.go deviation note, TaskPayload.Source dictionary entry).
- [ ] FR-109: both adoption-config docs corrected.
- [ ] FR-110: reviewer decision recorded (accept as-is with comment, or periodic eviction) — not required to close.

**Quality gates:** `go fmt`, `go vet`, `golangci-lint run`, `go test -race -gcflags=all=-d=checkptr=0 ./...` all clean; no coverage regression on `internal/domain`/`internal/application` aggregate (currently 91.2%).

## Risks and Mitigation

| Item | Type | Notes | Mitigation |
|------|------|-------|------------|
| FR-101/FR-102 fix touching shared bridge code | Risk | Both findings are in the same file (`buzz_task_bridge.go`) but different functions — low collision risk if done as separate commits. | TDD each independently; run full test suite between commits. |
| FR-103's `nil`-builder test revealing a real defect | Risk (low) | Review PRD notes the underlying `continue` logic "may reveal... needs no change at all" — but confirm, don't assume. | Write the test first; if it fails against current code, that's a real bug to fix, not just a coverage gap. |
| Doc-only fixes drifting further from code during this same spec's implementation | Risk (low) | Multiple doc files touched (FR-104, 105, 106, 108, 109). | Verify each doc claim against current code/CI config directly before writing, per the original review's own verification discipline. |

## Timeline and Milestones

[TBD] — tracked via `status.md`; expected to be a short spec given the review's own severity calibration (0 P0, narrow fixes).

## References

- Source PRD: [boabot-native-daemon-mode-auto-review-PRD.md](./boabot-native-daemon-mode-auto-review-PRD.md)
- Original feature spec (archived): `specs/archive/260814-boabot-native-daemon-mode/`
