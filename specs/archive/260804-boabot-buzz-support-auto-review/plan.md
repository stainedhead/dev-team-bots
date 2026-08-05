# Plan: Code Review Fixes — BaoBot Buzz Support

**Feature:** boabot-buzz-support-auto-review
**Created:** 2026-08-04
**Status:** Planning complete — see `tasks.md` for the executable breakdown

---

## Development Approach

TDD per finding (Red/Green/Refactor, per each finding's own guidance in the source PRD), one commit per FR, brief review after each fix before starting the next — per `AGENTS.md` and the review PRD's own Implementation Process section. Five workstreams (WS-A through WS-E), each in its own git worktree branched from `worktree-buzz-support-prd` (this feature is not yet merged to `main`), fixes landing back on this same branch as sequential commits, not separate PRs.

## Phase Breakdown

| Workstream | Findings | Parallelizable with | Primary files |
|---|---|---|---|
| WS-A | FR-001, FR-007 (reassigned — see `tasks.md` Progress Summary; both touch `config.go`) | WS-C, WS-D, WS-E | `cmd/boabot/main.go`, `config.go`, `keypair.go`/`token.go`, `user-docs/Buzz-Adoption-Config.md` |
| WS-B | FR-002 + FR-003 (together, not split) | WS-A, WS-C, WS-D, WS-E | `relay_client.go`, `reconnect.go` |
| WS-C | FR-004 | WS-A, WS-B, WS-D, WS-E | `internal/infrastructure/*/lock.go` |
| WS-D | FR-005 | WS-A, WS-B, WS-C, WS-E | `monitor.go`, possibly `discovery.go` |
| WS-E | FR-006 + FR-008 (doc-only, batched) | WS-A, WS-B, WS-C, WS-D | `nipoa.go`/`architecture.md`, `trigger.go`, archived original `spec.md`/`tasks.md` |

WS-B5 (ADR/technical-details collection, see `architecture.md` AD-2) runs *after* WS-A, WS-C, WS-D land — not parallel with them.

## Critical Path

WS-B (FR-002+FR-003) is the critical-path item: it is the most complex fix (concurrency correctness, requires a deterministic-timing test harness) and WS-B5's doc-collection task depends on WS-A/WS-C/WS-D finishing first, then WS-B closes the run. WS-A, WS-C, WS-D, WS-E have no interdependencies and may run fully in parallel with WS-B and each other.

## Testing Strategy

Per finding, per the source PRD's own Red/Green/Refactor guidance:
- FR-001: recording `dialFunc`/`fakeConn` seam (reuse Phase D test harness) asserting the AUTH event's `auth` tag presence/absence.
- FR-002/FR-003: a deterministic-timing test using an injected synchronization hook (via the existing `WithDial`/`WithSleep` seams) forcing the race window rather than relying on scheduler luck; verified additionally under `-race -gcflags=all=-d=checkptr=0 -count=20` for both new tests.
- FR-004: two goroutines racing `AcquireLock` against the same path with an injectable slow-writer seam.
- FR-005: `Monitor.handleChannelEvent`/`dispatch` test with multi-megabyte content, asserting rejection.
- FR-006/FR-007/FR-008: red/green on the acceptance criterion's checkable fact (a grep or doc statement), per `AGENTS.md`'s instruction to apply red/green discipline even to documentation-only changes.

Full repo-wide gate before Step 9 closes: `go fmt ./...`, `go vet ./...`, `golangci-lint run` (both modules), `go test -race -gcflags=all=-d=checkptr=0 ./...`, coverage check (≥90% domain+application, unregressed from 91.0%).

## Rollout Strategy

No phased rollout — all five workstreams' fixes land as commits on `worktree-buzz-support-prd` before Step 14 (Open Pull Request). No feature flag needed (all fixes are either wiring completions or internal correctness fixes, not new user-facing behavior).

## Success Metrics

- All 8 findings closed with a corresponding commit (checked against the commit log, not memory).
- Both P0s (FR-001, FR-002) resolved — merge-blocking per `AGENTS.md`.
- P1 (FR-003) resolved alongside FR-002 in one workstream, one combined test suite.
- Coverage unregressed (≥90% domain+application).
- Zero new `golangci-lint`/`go vet` issues.
