# Spec: ACP/Native Shared-State Parity — Review Fixes

**Created:** 2026-08-17
**Status:** Draft
**Source PRD:** [acp-native-shared-state-auto-review-PRD.md](./acp-native-shared-state-auto-review-PRD.md)

## Executive Summary

Implements the two P2 (non-blocking) findings from the code and design review of `feat/acp-native-shared-state` (Step 5/6 of the parent dev-flow run). No P0/P1 findings exist — this is a small, optional hardening pass, not a defect-fix cycle.

## Problem Statement

The review of the ACP/native shared-state parity feature found two minor items worth addressing before final merge: (1) the acceptance criteria around end-to-end conversation continuity are verified only by automated tests, not a live deployment, and that scoping should be explicit in the durable record; (2) `sharedstate.EnsureOwner` silently overwrites a malformed/corrupt ownership marker without a distinct warning log, unlike the identity-mismatch case which already logs one.

## Goals

- Make the "automated verification only, not live-deployment-verified" scoping explicit in `implementation-notes.md`.
- Add a distinct warning log when `EnsureOwner` encounters and overwrites a malformed marker, so an operator has a trail if it ever happens in practice.

## Non-Goals

- Not re-opening or re-scoping any FR-501–505 behavior — this is observability/documentation hardening only, not new functionality.
- Not performing live end-to-end deployment verification as part of this fix cycle (FR-R1 asks only that the record state this clearly, not that verification happen now).

## User Requirements / Functional Requirements

**FR-R1:** `implementation-notes.md`'s "Deviations from Plan" section states explicitly that live end-to-end verification (AC2–AC4 against a real shared deployment) is deferred to first real operator deployment, not part of this automated dev-flow run.

**FR-R2:** `sharedstate.EnsureOwner` logs a distinct `slog.Warn` when it encounters a malformed/unparseable marker file and overwrites it with the current process's identity — distinguishable in logs from the existing "directory already claimed by a different identity" warning.

## Non-Functional Requirements

- **Observability:** FR-R2's new log line must not fire on the normal "no marker exists yet" path (first claim) — only on a marker that exists but fails to parse.

## System Architecture

No new components. FR-R2 modifies `internal/infrastructure/local/sharedstate/sharedstate.go`'s existing `EnsureOwner` function only.

## Scope of Changes

- Files to modify: `boabot/internal/infrastructure/local/sharedstate/sharedstate.go`, `boabot/internal/infrastructure/local/sharedstate/sharedstate_test.go`, `specs/archive/260816-acp-native-shared-state/implementation-notes.md` (FR-R1, a documentation-only edit to an already-archived spec's file).
- No new files expected.

## Breaking Changes

None.

## Success Criteria and Acceptance Criteria

- [ ] A malformed/corrupt `.shared-state-owner` file logs a specific warning distinct from the identity-mismatch warning, before being overwritten (FR-R2).
- [ ] The normal first-claim (no marker yet) path logs nothing new (FR-R2's negative case).
- [ ] `implementation-notes.md` explicitly states live end-to-end verification is deferred (FR-R1).
- [ ] Existing `sharedstate` package tests continue to pass unchanged; a new test covers the malformed-marker warning.

**Quality gates:** `go fmt`, `go vet`, `golangci-lint run`, `go test -race -gcflags=all=-d=checkptr=0 ./...` all clean; domain+application aggregate coverage stays ≥90% (unaffected — `sharedstate` is an infrastructure package outside the gate, but existing coverage must not regress).

## Risks and Mitigation

| Item | Type | Notes |
|------|------|-------|
| None identified | — | Both findings are additive/observability-only; no behavior change to any acceptance-criteria-bearing code path. |

## Timeline and Milestones

Single pass, TDD per finding, expected to complete in one implementation session.

## References

- Source PRD: [acp-native-shared-state-auto-review-PRD.md](./acp-native-shared-state-auto-review-PRD.md)
- Reviewed feature: `specs/archive/260816-acp-native-shared-state/`
