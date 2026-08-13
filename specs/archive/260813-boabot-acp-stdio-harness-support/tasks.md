# Tasks: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13
**Status:** Planning

## Progress Summary

8/8 tasks complete.

## Phase 1 — Dependency + Skeleton

**T1** — Add `github.com/coder/acp-go-sdk` dependency; create `internal/infrastructure/acp/` package skeleton with a no-op `Agent`; wire a `-acp` flag/mode into `cmd/boabot/main.go` that constructs it and blocks on `acp.NewAgentSideConnection`. Includes the small export-rename `newLocalProviderFactory` → `NewLocalProviderFactory` in `internal/application/team/provider_factory.go` (2 call sites: `team_manager.go`, `export_test.go`) so the ACP package can construct providers the same way native mode does — already confirmed safe/isolated during research, just needs doing.
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

## Phase 5 — Usage Field (Scoped Down)

**T5** — Leave `PromptResponse.Usage` `nil` per corrected FR-005 (cost enforcement exists in this codebase but isn't wired into the task path in either mode — discovered during implementation, grep-verified). Confirm this doesn't break `buzz-acp`'s handling of `session/prompt` responses.
- **Dependencies:** T3
- **Duration:** Trivial
- **Acceptance criteria:** Unit test asserts a `session/prompt` response with `Usage: nil` is accepted/handled correctly; no fabricated usage numbers appear anywhere in the response.

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
