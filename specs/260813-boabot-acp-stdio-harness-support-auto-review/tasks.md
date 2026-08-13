# Tasks: BaoBot ACP Stdio Harness Support — Auto-Review Fixes

**Created:** 2026-08-13
**Status:** Planning

## Progress Summary

0/9 tasks complete.

## P0 — Blockers

**RT1 (FR-001)** — Fix the data race / cross-session progress-routing bug.
- **Dependencies:** None
- **Duration:** Medium
- **Acceptance criteria:** A new concurrent-two-sessions test reproduces the race pre-fix and passes clean under `-race` post-fix; existing tests still pass.

**RT2 (FR-002 + FR-003)** — Add turn-level `slog` logging (start/end/cancel/panic-recovery); correct or fulfill the doc's `starting boabot acp mode` claim.
- **Dependencies:** None (independent of RT1)
- **Duration:** Small
- **Acceptance criteria:** `slog` calls present at the specified points; the user-doc's claim matches real observable behavior (verified by running `boabot -acp` and checking stderr).

## P1

**RT3 (FR-004)** — Wire `WithRulesTracker` in `cmd/boabot/acp.go`, mirroring `team_manager.go:836-837`; or, if deferred, correct spec.md/user-doc claims and get explicit user sign-off.
- **Dependencies:** None
- **Duration:** Small
- **Acceptance criteria:** Either wired + tested, or docs corrected and the deferral explicitly confirmed by the user.

**RT4 (FR-005)** — Fix `session.cancel` re-entrancy for overlapping same-session-ID turns.
- **Dependencies:** None
- **Duration:** Small
- **Acceptance criteria:** A same-session-ID overlapping-turn test proves the second turn's cancellation is no longer clobberable.

**RT5 (FR-006)** — Bound/evict `Agent.sessions`.
- **Dependencies:** None
- **Duration:** Small
- **Acceptance criteria:** A test proves the session map doesn't grow without bound over many `session/new` calls.

## P2

**RT6 (FR-007)** — `go mod tidy` to fix `coder/acp-go-sdk`'s `// indirect` mislabeling.
- **Dependencies:** None
- **Duration:** Trivial
- **Acceptance criteria:** `go.mod` shows it as a direct dependency; `go build`/`go test` unaffected.

**RT7 (FR-008)** — Wire `//go:build integration` tests into CI, or document the gap explicitly.
- **Dependencies:** None
- **Duration:** Small
- **Acceptance criteria:** Either a CI job runs `-tags=integration` tests, or ADR-B026/status docs explicitly note they're manual-only.

**RT8 (FR-009)** — Test proving clean exit on stdin EOF at the `boabot` binary level.
- **Dependencies:** None
- **Duration:** Small
- **Acceptance criteria:** A real-subprocess test closes stdin (not kill) and asserts the process exits cleanly.
