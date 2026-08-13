# PRD: BaoBot ACP Stdio Harness Support — Auto-Review Fixes

**Created:** 2026-08-13
**Jira:** N/A
**Status:** Draft

## Problem Statement

An independent code review of `feat/boabot-acp-stdio-harness` (the branch implementing `specs/260813-boabot-acp-stdio-harness-support`) found a reproducible data race in the ACP turn-execution path, a real behavior gap contradicting the feature's own "identical to native mode" claim, missing operational logging against an explicit spec NFR, and several smaller correctness/hygiene issues. This PRD tracks fixing them before the branch merges. Overall review verdict: **Request Changes**.

## Goals

- Eliminate the reproducible data race and its associated cross-session progress-message routing bug in `internal/infrastructure/acp`.
- Close the undisclosed `RulesTracker` behavior gap between ACP mode and native daemon mode, or explicitly and correctly document it if left unfixed.
- Bring ACP mode's operational logging in line with spec.md's NFR.

## Non-Goals

- Re-litigating design decisions the review explicitly endorsed (the `progressReporter` type-assertion pattern itself, the thin-adapter architecture, the decision not to exercise the real `buzz-acp` binary).
- Wiring real budget/cost enforcement into the task path (confirmed out of scope by the original spec's corrected FR-005; the review independently re-verified this finding and did not challenge it).

## Functional Requirements

**FR-001 (P0):** Fix the data race and cross-session progress/keep-alive routing bug caused by a single shared `domain.Worker` instance having `WithProgressHandler` mutated per-turn across concurrent ACP sessions (`internal/infrastructure/acp/agent.go:42,68-79`, `turn.go:50-57`, `internal/application/execute_task.go:22,52-54`). Must be proven fixed with a test that exercises two concurrent sessions calling `session/prompt` simultaneously on one `Agent`, run under `-race`.

**FR-002 (P0):** Add turn-level operational logging (`slog`) to `internal/infrastructure/acp` for at minimum: turn start, turn end (success/refusal/cancelled), cancellation, and recovered panics — per spec.md's NFR ("Turn start/end, tool calls, and errors logged with the same structure as native daemon-mode task execution"), currently entirely unmet (zero `slog` calls in `agent.go`/`turn.go`).

**FR-003 (P0):** Correct `user-docs/ACP-Harness-Adoption-Config.md`'s Step 3 claim that boabot's stderr shows a `starting boabot acp mode` log line — no such line exists. Either add a real startup log line matching the doc, or correct the doc to describe actual observable behavior.

**FR-004 (P1):** Wire `domain.Worker.WithRulesTracker` (AGENTS.md/CLAUDE.md hierarchical loading) in `cmd/boabot/acp.go` when `cfg.Orchestrator.WorkDirs` is non-empty, mirroring `team_manager.go:836-837` exactly — or, if intentionally deferred, correct `spec.md` FR-004 and `ACP-Harness-Adoption-Config.md`'s "no filesystem/tool differences from native mode" claim to disclose the gap explicitly, and get the user's explicit sign-off on deferring it rather than fixing it silently.

**FR-005 (P1):** Fix the `session.cancel` re-entrancy issue where two overlapping `Prompt` calls on the *same* session ID can have the second call's turn silently un-cancellable (the first turn's deferred `s.cancel = nil` clobbers the second turn's active cancel function) (`internal/infrastructure/acp/session.go:8-10`, `turn.go`'s defer in `Prompt`). Must be proven fixed with a test.

**FR-006 (P1):** Add a bound or eviction path for `Agent.sessions` (`agent.go:116-122`) — currently grows unboundedly for the process's documented long-lived, pooled lifetime, since `CloseSession` is a `MethodNotFound` stub with no removal path.

**FR-007 (P2):** Run `go mod tidy` to correct `github.com/coder/acp-go-sdk`'s `go.mod` entry from `// indirect` to direct (it is directly imported by `agent.go`/`acp.go`).

**FR-008 (P2):** Wire the `//go:build integration` tests (`cmd/boabot/acp_integration_test.go`) into CI (`.github/workflows/boabot.yml`), or explicitly document in the ADR/status docs that this acceptance-criteria-satisfying test currently only runs manually.

**FR-009 (P2):** Add a test proving `runACP`/FR-007's "exits cleanly on stdin EOF" claim at the `boabot` level (currently unverified by any test; the SDK's own `Done()`-on-EOF behavior was confirmed correct by source inspection during review, but boabot's own wiring of it is untested).

## Non-Functional Requirements

- **Concurrency:** All fixes touching `internal/infrastructure/acp` must pass `go test -race` including any new concurrent-session tests.
- **Regression safety:** `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, and the full `go test ./...` (plus `-race -gcflags=all=-d=checkptr=0 ./...` per the documented `fiatjaf.com/nostr` workaround) must remain green throughout.
- **TDD:** Every fix follows red-green-refactor — a failing test first, per AGENTS.md.
- **Process:** Use git worktrees for parallel fix workstreams where fixes are independent (FR-001/002/003 in `internal/infrastructure/acp` + docs are somewhat coupled; FR-004 through FR-009 are largely independent of each other and of FR-001-003). Conduct a brief code and design review as each fix completes before moving to the next. P0 items first, then P1, then P2.

## Acceptance Criteria

- [ ] FR-001: a concurrent-session test reproduces the race pre-fix and passes clean under `-race` post-fix.
- [ ] FR-002: `slog` calls present for turn start/end/cancel/panic-recovery in the ACP package.
- [ ] FR-003: doc claim matches real observable behavior.
- [ ] FR-004: either `WithRulesTracker` wired and tested, or spec.md/user-doc explicitly corrected with the user's sign-off to defer.
- [ ] FR-005: a same-session-ID overlapping-turn test proves cancellation is no longer clobberable.
- [ ] FR-006: session map has a documented bound or eviction path.
- [ ] FR-007: `go.mod` shows `coder/acp-go-sdk` as a direct dependency.
- [ ] FR-008: integration tests run in CI, or the gap is explicitly documented.
- [ ] FR-009: a test proves clean-exit-on-stdin-EOF at the `boabot` binary level.
- [ ] All P0 items closed before this branch merges (AGENTS.md: "P0 findings that remain open block the PR").

## Dependencies and Risks

| Item | Type | Notes |
|------|------|-------|
| FR-001's fix approach | Risk | Multiple valid fixes exist (per-turn `Worker`, a mutex serializing turns per `Agent`, or session-scoped progress routing) — pick based on which preserves `buzz-acp`'s expected concurrent-session behavior best; document the choice in implementation-notes.md. |
| FR-004's scope | Risk | If `WithRulesTracker` wiring surfaces further native-mode parity gaps not caught by this review, treat as a new finding for a future pass, not scope creep into this fix cycle. |

## Open Questions

- None outstanding — all findings above are actionable as stated.
