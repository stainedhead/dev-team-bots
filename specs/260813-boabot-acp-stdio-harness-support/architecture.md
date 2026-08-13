# Architecture: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13
**Status:** Draft

## Architecture Overview

`boabot acp -config <persona-config.yaml>` starts a **long-lived** process (not spawn-per-turn — confirmed by research.md's analysis of `buzz-acp`'s pooled-agent lifecycle) that:

1. Loads exactly one persona's config the same way native daemon mode does (`config.Load`, `WorkerFactory` construction, `BudgetTracker` construction) — reusing existing wiring code from `cmd/boabot/main.go`, not duplicating it.
2. Constructs an `internal/infrastructure/acp.Agent` implementing `coder/acp-go-sdk`'s `acp.Agent` interface.
3. Wraps it with `acp.NewAgentSideConnection(agent, os.Stdout, os.Stdin)` and blocks, letting the SDK own the JSON-RPC/stdio transport for the process's entire lifetime — across many sessions and many turns per session, per `buzz-acp`'s pooled-process model.

This directly satisfies ADR-B020's original objection: BaoBot's own `Worker`, `BudgetTracker`, and autonomy-gate logic execute exactly as they do in native mode — the ACP package is a thin adapter over them, not a second runtime. There is no dependency on `TeamManager`, `ChannelMonitor`, or `MessageQueue` — ACP mode services exactly one persona, one process, driven synchronously by `buzz-acp`'s requests rather than an async multi-bot queue, so those abstractions don't fit and aren't used (confirms FR-006 as originally scoped).

## Component Architecture

```
cmd/boabot/main.go
  └─ new "acp" mode: same config/provider/worker-factory/budget wiring as daemon mode,
     but instead of mgr.WithChannelMonitor(...).Run(ctx), constructs internal/infrastructure/acp.Agent
     and calls acp.NewAgentSideConnection(agent, os.Stdout, os.Stdin) — then blocks on that connection's lifetime.

internal/infrastructure/acp/
  ├─ agent.go       — Agent struct implementing acp.Agent (coder/acp-go-sdk), holds WorkerFactory + BudgetTracker
  ├─ session.go      — per-session state: sessionId, active turn cancel func, turn counter
  ├─ turn.go         — session/prompt handling: builds domain.Task, calls Worker.Execute, runs the
  │                     keep-alive ticker concurrently, maps domain.TaskResult -> ACP response/updates
  └─ *_test.go        — unit tests against a mocked acp.Client-side connection and a mocked domain.Worker

(no changes to internal/domain, internal/application/team, or existing infrastructure packages)
```

## Layer Responsibilities

- **Domain:** No new interfaces required. `Worker`, `Task`, `TaskResult`, `WorkerFactory`, `BudgetTracker` are reused as-is (see data-dictionary.md). This is a deliberate minimization — introducing a parallel domain seam here would risk re-creating exactly the duplicated-logic problem ADR-B020 flagged.
- **Application:** No new use-case package needed for v1 — `internal/infrastructure/acp`'s `turn.go` can call `WorkerFactory`/`BudgetTracker` directly, mirroring how `cmd/boabot/main.go` already wires workers for native mode, since ACP mode has no multi-bot orchestration to coordinate (non-goal, one process = one persona). If a genuine cross-cutting use case emerges during implementation (e.g. shared turn-budget-check logic factored out of `main.go`'s native wiring), extract it then — do not pre-build an application-layer abstraction speculatively.
- **Infrastructure:** New `internal/infrastructure/acp/` package — the ACP protocol adapter. This is the only new package.

## Data Flow

1. `buzz-acp` spawns `boabot acp -config bots/tech-lead/config.yaml` once; the process stays alive for the pool's lifetime.
2. `buzz-acp` sends `initialize` → `Agent` responds with capabilities (SDK handles version negotiation).
3. `buzz-acp` sends `session/new` for a channel/thread → `Agent` allocates a `session`, returns `sessionId`.
4. On a mention, `buzz-acp` sends `session/prompt` with fully-assembled prompt content (platform context + team instructions + memory + persona system prompt + user message, all pre-assembled by `buzz-acp` per research.md — BaoBot treats it as opaque `Instruction` text).
5. `Agent` builds a `domain.Task`, starts a goroutine calling `WorkerFactory.New().Execute(ctx, task)`, and **concurrently** emits periodic `session/update` (`acp::thought`) keep-alive notifications until it returns — required so `buzz-acp`'s `--idle-timeout` (silence-based) doesn't kill a long tool-using turn.
6. On completion, `Agent` emits a final `session/update` (`acp::stream`) with the full output, then responds to `session/prompt` with `stopReason: EndTurn` (or an error-mapped reason) plus usage figures sourced from `BudgetTracker`.
7. `session/cancel` (if received mid-turn) cancels that session's active turn context; `Worker.Execute` must return promptly on context cancellation — verify this is already true of existing `Worker` implementations during implementation (if not, that's a pre-existing gap surfaced by this feature, to be logged in implementation-notes.md, not silently patched around).
8. Process continues serving further `session/new`/`session/prompt` calls for the pool's lifetime; exits cleanly on stdin EOF (FR-007).

## Sequence Diagrams

`[TBD]` — a simple sequence diagram (initialize → session/new → session/prompt with concurrent keep-alive → session/update final → response) can be added during implementation if useful; the Data Flow section above covers the same ground in prose and is sufficient to start implementation.

## Integration Points

- `cmd/boabot/main.go` — new mode routing; reuses existing config/provider/worker-factory/budget construction code (refactor into a shared helper if `main.go`'s existing wiring isn't already factored for reuse — check during implementation rather than assuming).
- `internal/domain/worker.go` — `Worker`/`Task`/`TaskResult`/`WorkerFactory`, unchanged.
- `internal/domain/budget.go` — `BudgetTracker`, unchanged.
- External dependency: `github.com/coder/acp-go-sdk` (new `go.mod` entry).
- External: `buzz-acp` binary (host/spawner) — validated against the actual bundled binary via an integration test, per plan.md.

## Architectural Decisions

- **AD-1:** Reuse `Worker`/`BudgetTracker` directly rather than introducing a new domain seam — the ACP adapter is intentionally "thin." This is the concrete answer to ADR-B020's objection and must be reflected in the new/superseding ADR entry (spec.md acceptance criteria).
- **AD-2:** `boabot acp` is a long-lived, multi-session, multi-turn process (not spawn-per-turn), per research.md's confirmed `buzz-acp` pooled-process behavior. Process lifecycle design (FR-007) targets clean shutdown on stdin EOF, not a single-turn-then-exit model.
- **AD-3:** Keep-alive `session/update` notifications during a turn are a **correctness requirement** (idle-timeout compatibility), not just a UX nicety for FR-003 — `Worker.Execute` has no incremental output today, so true token-level streaming is out of scope for v1; this must be stated explicitly in the ADR and in FR-003's acceptance criteria language rather than left implicit.
- **AD-4:** `buzz-acp --mcp-command` semantics remain unresolved (research.md) and are not required for FR-001–FR-008; not a blocking dependency for this feature's v1.
