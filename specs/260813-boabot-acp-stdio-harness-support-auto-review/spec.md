# Spec: BaoBot ACP Stdio Harness Support — Auto-Review Fixes

**Created:** 2026-08-13
**Status:** Draft

## Executive Summary

An independent code review of `feat/boabot-acp-stdio-harness` found a reproducible data race and cross-session message-routing bug in the ACP turn-execution path, a real (undisclosed) behavior gap versus native daemon mode, missing operational logging against an explicit spec NFR, and several smaller correctness/hygiene issues. This spec tracks fixing all nine findings before the branch merges.

## Problem Statement

See `boabot-acp-stdio-harness-support-auto-review-PRD.md` (this directory) for the full review findings this spec implements.

## Goals / Non-Goals

**Goals:** eliminate the data race (FR-001); close the `RulesTracker` gap or explicitly document it (FR-004); bring logging in line with the spec NFR (FR-002); fix the session-cancellation re-entrancy bug (FR-005); bound session-map growth (FR-006); and clear the smaller hygiene items (FR-003, FR-007, FR-008, FR-009).

**Non-Goals:** re-litigating design decisions the review endorsed; wiring real budget/cost enforcement (confirmed out of scope by the original feature's spec).

## User Requirements

Carried verbatim from the review PRD — see `boabot-acp-stdio-harness-support-auto-review-PRD.md`'s Functional Requirements section (FR-001 through FR-009, with priorities P0/P1/P2).

## Non-Functional Requirements

- **Concurrency:** all fixes touching `internal/infrastructure/acp` pass `go test -race`, including new concurrent-session tests.
- **Regression safety:** `go build`/`go vet`/`golangci-lint`/`go test ./...` (and `-race -gcflags=all=-d=checkptr=0 ./...`) stay green throughout.
- **TDD:** every fix is red-green-refactor.

## System Architecture

No new packages. Touches `internal/infrastructure/acp/{agent,session,turn}.go` and their tests (FR-001, FR-002, FR-005, FR-006), `internal/application/execute_task.go` (FR-001, if that's the chosen fix location), `cmd/boabot/acp.go` (FR-004, FR-009), `user-docs/ACP-Harness-Adoption-Config.md` (FR-003), `go.mod`/`go.sum` (FR-007), `.github/workflows/boabot.yml` (FR-008).

## Scope of Changes

See tasks.md for the concrete task-to-file mapping.

## Breaking Changes

None expected. If FR-001's fix changes `Worker` construction semantics (e.g. one `Worker` per turn instead of per `Agent`) that's an internal implementation change, not a public API change.

## Success Criteria and Acceptance Criteria

Carried verbatim from the review PRD's Acceptance Criteria section.

## Risks and Mitigation

| Risk | Mitigation |
|------|------------|
| FR-001's fix approach isn't predetermined (3 valid options) | Pick one during implementation, document the choice and rationale in implementation-notes.md (see review PRD's Open Questions). |
| FR-004 may surface further native-mode parity gaps | Treat any newly-surfaced gap as a follow-up finding, not scope creep into this fix cycle. |

## Timeline and Milestones

Single implementation pass, P0 → P1 → P2, per AGENTS.md ("P0 findings that remain open block the PR").

## References

- Review PRD: `specs/260813-boabot-acp-stdio-harness-support-auto-review/boabot-acp-stdio-harness-support-auto-review-PRD.md`
- Original feature spec (archived): `specs/archive/260813-boabot-acp-stdio-harness-support/`
