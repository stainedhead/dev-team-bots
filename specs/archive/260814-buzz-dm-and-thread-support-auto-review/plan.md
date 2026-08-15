# Plan: Buzz DM/Thread Support — Code Review Fixes

**Created:** 2026-08-15
**Status:** Planning

## Development Approach

TDD for every code fix (FR-301, FR-302 if eviction chosen, FR-303 if pre-filter chosen, FR-304). Documentation-only fixes (FR-303/document option, FR-305, FR-306) verified against current code before being written. Brief self-review after each fix before moving to the next, per the review PRD's Implementation Guidance.

## Phase Breakdown

Given the review PRD already precisely locates every finding, this spec's Research/Architecture phases are intentionally thin. Most work is direct implementation.

1. FR-301 (P1) — must close before done.
2. FR-302 through FR-306 (P2) — independent, parallelizable per the review PRD's own guidance.

## Critical Path

FR-301 is independent of all P2 items. FR-302, FR-303, FR-304 touch disjoint files and can proceed in parallel. FR-305, FR-306 are documentation-only and independent of everything else.

## Testing Strategy

- FR-301: end-to-end test driving a Buzz-dispatched task through `HandleResult`, asserting exactly one chat-store entry.
- FR-304: test asserting the warning is emitted when DM activates with an inactive gate, absent when a gate is configured.
- FR-302/FR-303: test only if a code-level fix is chosen over documentation.
- Full suite + `-race -gcflags=all=-d=checkptr=0` + `golangci-lint` + `gofmt` + `go vet` after each fix.

## Rollout Strategy

Same branch, same PR (not yet opened) as the original feature.

## Success Metrics

All FR-301–FR-306 acceptance criteria in spec.md met.
