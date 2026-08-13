# Architecture: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13
**Status:** Draft

## Architecture Overview

`boabot acp -config <persona-config.yaml>` starts a **long-lived** process (not spawn-per-turn — confirmed by research.md's analysis of `buzz-acp`'s pooled-agent lifecycle) that:

1. Loads exactly one persona's config the same way native daemon mode does (`config.Load`, `WorkerFactory` construction) — reusing existing wiring code from `cmd/boabot/main.go`, not duplicating it. **No budget tracker to construct — nothing wires `internal/application/cost.EnforceBudgetUseCase` into the live task path in either mode; see the corrected FR-005 in spec.md.**
2. Constructs an `internal/infrastructure/acp.Agent` implementing `coder/acp-go-sdk`'s `acp.Agent` interface.
3. Wraps it with `acp.NewAgentSideConnection(agent, os.Stdout, os.Stdin)` and blocks, letting the SDK own the JSON-RPC/stdio transport for the process's entire lifetime — across many sessions and many turns per session, per `buzz-acp`'s pooled-process model.

This directly satisfies ADR-B020's original objection: BaoBot's own `Worker` execution logic runs exactly as it does in native mode — the ACP package is a thin adapter over them, not a second runtime. There is no dependency on `TeamManager`, `ChannelMonitor`, or `MessageQueue` — ACP mode services exactly one persona, one process, driven synchronously by `buzz-acp`'s requests rather than an async multi-bot queue, so those abstractions don't fit and aren't used (confirms FR-006 as originally scoped).

## Component Architecture

```
cmd/boabot/main.go
  └─ new "acp" mode: same config/provider/worker-factory/budget wiring as daemon mode,
     but instead of mgr.WithChannelMonitor(...).Run(ctx), constructs internal/infrastructure/acp.Agent
     and calls acp.NewAgentSideConnection(agent, os.Stdout, os.Stdin) — then blocks on that connection's lifetime.

internal/infrastructure/acp/
  ├─ agent.go       — Agent struct implementing acp.Agent (coder/acp-go-sdk), holds a domain.WorkerFactory
  ├─ session.go      — per-session state: sessionId, active turn cancel func, turn counter
  ├─ turn.go         — session/prompt handling: builds domain.Task, calls Worker.Execute, runs the
  │                     keep-alive ticker concurrently, maps domain.TaskResult -> ACP response/updates
  └─ *_test.go        — unit tests against a mocked acp.Client-side connection and a mocked domain.Worker

(no changes to internal/domain, internal/application/team, or existing infrastructure packages)
```

**Worker construction, confirmed by reading team_manager.go's startBot (verified during implementation, not assumed):** `application.NewExecuteTaskUseCase(provider domain.ModelProvider, mcp domain.MCPClient, memory domain.MemoryStore, embedder domain.Embedder, vectors domain.VectorStore, soulPrompt string) *ExecuteTaskUseCase` implements `domain.Worker`, and exposes `WithProgressHandler(fn func(taskID, line string))` — a real per-tool-call progress callback that is a *better* keep-alive signal than a blind ticker alone (use both: progress-driven updates, plus a short ticker fallback for a single long silent tool call with no intermediate progress events). Clean, orchestrator-mode-free construction primitives to reuse directly (do not duplicate their logic): `fs.New(basePath string) (*FS, error)`, `vector.New(basePath string) (*VectorStore, error)`, `bm25.DefaultEmbedder() *Embedder`, `localmcp.NewClient(allowedDirs []string, opts...) *Client`, and the provider factory in `boabot/internal/application/team/provider_factory.go` (currently package-private as `newLocalProviderFactory` — needs a small export-rename, e.g. to `NewLocalProviderFactory`, for `internal/infrastructure/acp` to construct providers the same way native mode does; `export_test.go` already has a test-only wrapper confirming this is a safe, isolated rename).

## Layer Responsibilities

- **Domain:** No new interfaces required. `Worker`, `Task`, `TaskResult`, `WorkerFactory` are reused as-is (see data-dictionary.md). This is a deliberate minimization — introducing a parallel domain seam here would risk re-creating exactly the duplicated-logic problem ADR-B020 flagged.
- **Application:** No new use-case package needed for v1 — `internal/infrastructure/acp`'s `turn.go` constructs an `application.ExecuteTaskUseCase` directly via the primitives above, mirroring how `cmd/boabot/main.go` already wires workers for native mode, since ACP mode has no multi-bot orchestration to coordinate (non-goal, one process = one persona).
- **Infrastructure:** New `internal/infrastructure/acp/` package — the ACP protocol adapter. This is the only new package.

## Data Flow

1. `buzz-acp` spawns `boabot acp -config bots/tech-lead/config.yaml` once; the process stays alive for the pool's lifetime.
2. `buzz-acp` sends `initialize` → `Agent` responds with capabilities (SDK handles version negotiation).
3. `buzz-acp` sends `session/new` for a channel/thread → `Agent` allocates a `session`, returns `sessionId`.
4. On a mention, `buzz-acp` sends `session/prompt` with fully-assembled prompt content (platform context + team instructions + memory + persona system prompt + user message, all pre-assembled by `buzz-acp` per research.md — BaoBot treats it as opaque `Instruction` text).
5. `Agent` builds a `domain.Task`, starts a goroutine calling `WorkerFactory.New().Execute(ctx, task)`, and **concurrently** emits periodic `session/update` (`acp::thought`) keep-alive notifications until it returns — required so `buzz-acp`'s `--idle-timeout` (silence-based) doesn't kill a long tool-using turn.
6. On completion, `Agent` emits a final `session/update` (`acp::stream`) with the full output, then responds to `session/prompt` with `stopReason: EndTurn` (or an error-mapped reason); `Usage` is left `nil` for v1 (no enforcement is wired into the task path to source it from — see FR-005).
7. `session/cancel` (if received mid-turn) cancels that session's active turn context; `Worker.Execute` must return promptly on context cancellation — verify this is already true of existing `Worker` implementations during implementation (if not, that's a pre-existing gap surfaced by this feature, to be logged in implementation-notes.md, not silently patched around).
8. Process continues serving further `session/new`/`session/prompt` calls for the pool's lifetime; exits cleanly on stdin EOF (FR-007).

## Sequence Diagrams

`[TBD]` — a simple sequence diagram (initialize → session/new → session/prompt with concurrent keep-alive → session/update final → response) can be added during implementation if useful; the Data Flow section above covers the same ground in prose and is sufficient to start implementation.

## Integration Points

- `cmd/boabot/main.go` — new mode routing; reuses existing config/provider/worker-factory/budget construction code (refactor into a shared helper if `main.go`'s existing wiring isn't already factored for reuse — check during implementation rather than assuming).
- `internal/domain/worker.go` — `Worker`/`Task`/`TaskResult`/`WorkerFactory`, unchanged.
- `internal/application/team/provider_factory.go` — small export-rename (`newLocalProviderFactory` → `NewLocalProviderFactory`) so `internal/infrastructure/acp` can reuse it.
- External dependency: `github.com/coder/acp-go-sdk` (new `go.mod` entry).
- External: `buzz-acp` binary (host/spawner) — validated against the actual bundled binary via an integration test, per plan.md.

## Architectural Decisions

- **AD-1:** Reuse `Worker` directly rather than introducing a new domain seam — the ACP adapter is intentionally "thin." This is the concrete answer to ADR-B020's objection and must be reflected in the new/superseding ADR entry (spec.md acceptance criteria).
- **AD-5:** A real cost-enforcement system exists (`internal/domain/cost`, `internal/application/cost.EnforceBudgetUseCase`) but is wired into neither native nor ACP mode's task path (grep-verified: `NewEnforceBudgetUseCase` is constructed nowhere outside its own package) — `boabot/AGENTS.md`'s "BudgetTracker" description doesn't match either the old or corrected picture. ACP mode's v1 leaves `PromptResponse.Usage` nil; wiring real enforcement into either mode for the first time is a separate follow-up, out of scope here and asymmetric to do only for ACP mode (see corrected FR-005 in spec.md).
- **AD-2:** `boabot acp` is a long-lived, multi-session, multi-turn process (not spawn-per-turn), per research.md's confirmed `buzz-acp` pooled-process behavior. Process lifecycle design (FR-007) targets clean shutdown on stdin EOF, not a single-turn-then-exit model.
- **AD-3:** Keep-alive `session/update` notifications during a turn are a **correctness requirement** (idle-timeout compatibility), not just a UX nicety for FR-003 — `Worker.Execute` has no incremental output today, so true token-level streaming is out of scope for v1; this must be stated explicitly in the ADR and in FR-003's acceptance criteria language rather than left implicit.
- **AD-4:** `buzz-acp --mcp-command` semantics remain unresolved (research.md) and are not required for FR-001–FR-008; not a blocking dependency for this feature's v1.
