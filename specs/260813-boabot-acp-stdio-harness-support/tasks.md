# Tasks: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13
**Status:** Planning

## Progress Summary

0/8 tasks complete.

## Phase 1 — Dependency + Skeleton

**T1** — Add `github.com/coder/acp-go-sdk` dependency; create `internal/infrastructure/acp/` package skeleton with a no-op `Agent`; wire a `-acp` flag/mode into `cmd/boabot/main.go` that constructs it and blocks on `acp.NewAgentSideConnection`.
- **Dependencies:** None
- **Duration:** Small
- **Acceptance criteria:** `go build` succeeds with the new dependency; `boabot -acp -config <persona>.yaml` starts and doesn't crash; existing daemon mode (`boabot -config <persona>.yaml`, no `-acp`) is unaffected — full `go test ./...` green.

## Phase 2 — Handshake

**T2** — Implement `initialize` and `session/new` handling.
- **Dependencies:** T1
- **Duration:** Small
- **Acceptance criteria:** Unit test drives `initialize` then `session/new` against the `Agent` and asserts a valid `sessionId` is returned; TDD (failing test first).

## Phase 3 — Core Turn Execution

**T3** — Implement `session/prompt`: build `domain.Task` from prompt content, call `WorkerFactory.New().Execute`, map `domain.TaskResult` to the ACP response (`stopReason`, output).
- **Dependencies:** T2
- **Duration:** Medium
- **Acceptance criteria:** Unit test with a mocked `Worker` returning a canned `TaskResult` asserts the correct ACP response shape and `stopReason` mapping (success → `EndTurn`, error → an appropriate reason).

## Phase 4 — Keep-Alive and Cancellation

**T4** — Add the concurrent keep-alive ticker (`acp::thought` updates) during an in-flight turn; wire `session/cancel` to the turn's `context.CancelFunc`.
- **Dependencies:** T3
- **Duration:** Medium
- **Acceptance criteria:** Unit test with an artificially slow mocked `Worker` asserts at least one keep-alive `session/update` is emitted before completion; a `session/cancel` call during that delay causes the turn's context to be cancelled and the call to return promptly.

## Phase 5 — Budget/Usage Wiring

**T5** — Source `acp::usage` updates and the `session/prompt` response's usage block from `domain.BudgetTracker`, per FR-005.
- **Dependencies:** T3
- **Duration:** Small
- **Acceptance criteria:** Unit test asserts usage figures reflect `BudgetTracker` state after a turn, and that a budget-exceeded condition maps to the correct `stopReason` rather than a raw error.

## Phase 6 — Process Lifecycle and Panic Recovery

**T6** — Clean shutdown on stdin EOF; `recover()` around turn execution per FR-008.
- **Dependencies:** T4, T5
- **Duration:** Small
- **Acceptance criteria:** Unit/integration test confirms a panicking `Worker` surfaces as a protocol error response, not a process crash; process exits cleanly (exit code 0) on stdin EOF with no pending turns.

## Phase 7 — Real `buzz-acp` Integration Test

**T7** — `//go:build integration` test spawning the real bundled `buzz-acp` binary against `boabot acp`.
- **Dependencies:** T6
- **Duration:** Medium
- **Acceptance criteria:** `initialize` handshake succeeds against the real binary; a `session/prompt` round-trip returns `stopReason: EndTurn`; a deliberately slow turn with a short test `--idle-timeout` is NOT killed (proves the keep-alive mechanism works against the real host, not just a mock).

## Phase 8 — Documentation

**T8** — New/superseding ADR entry vs. ADR-B020; update `docs/technical-details.md`, `docs/product-summary.md`, `README.md`, and add ACP-mode guidance to `boabot/user-docs/`.
- **Dependencies:** T3 (can start once core turn execution is stable; does not block on T4–T7)
- **Duration:** Small
- **Acceptance criteria:** New ADR entry explicitly explains how this design avoids ADR-B020's original control-inversion/duplicated-logic objections (per architecture.md AD-1); all four docs updated and consistent with actual shipped behavior.
