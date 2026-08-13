# Data Dictionary: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13

## Purpose

Defines the domain entities, value objects, interfaces, and protocol message types introduced by BaoBot's ACP stdio harness mode.

## Existing Types Reused (no changes)

- `domain.Worker` — `Execute(ctx, Task) (TaskResult, error)`. **Blocking, single-shot — no incremental/streaming callback exists today.** This is a real constraint on FR-003 design; see architecture.md.
- `domain.Task` — `{ID, BoardItemID, Instruction, Source, WorkDir}`. ACP mode populates `Instruction` from the ACP `session/prompt` content (already fully assembled by `buzz-acp` — see research.md), `Source` set to a constant identifying ACP-mode origin (e.g. `"acp"`), `WorkDir` from persona config same as native mode.
- `domain.TaskResult` — `{TaskID, Output, Success, Err}`. `Output` maps to the final `acp::stream` update content; `Success`/`Err` map to `session/prompt`'s `stopReason` (`EndTurn` on success, else an error-mapped reason).
- `domain.WorkerFactory` — `New() Worker`, used exactly as native daemon mode uses it, scoped to the single persona ACP mode loads.
- ~~`domain.BudgetTracker`~~ — **does not exist in this codebase** (grep-verified during implementation; `boabot/AGENTS.md`'s description of it is aspirational, not real). FR-005 corrected accordingly — see spec.md.

## New Types (infrastructure/acp package)

- **`Agent`** (implements `github.com/coder/acp-go-sdk`'s `acp.Agent` interface) — the ACP-side adapter. Holds a `domain.WorkerFactory` and session state. No budget tracker to hold (see above).
- **`session`** — internal struct tracking one ACP `sessionId`: created on `session/new`, holds a cancellation `context.CancelFunc` for in-flight turns (`session/cancel` target), and a turn counter (for `MaxTurnRequests` stop-reason mapping if a persona-level per-session turn cap is configured — `[TBD]` whether this is needed for v1 or deferred).
- **Keep-alive ticker** — not a domain type, but a required behavior: since `Worker.Execute` is blocking with no incremental output, and `buzz-acp`'s `--idle-timeout` kills a turn after N seconds of *stdout silence*, ACP mode MUST emit periodic `session/update` notifications (e.g. an `acp::thought` "still working" ping) while a turn is in flight — mirroring the existing Slack-mode typing-indicator refresh pattern (`Buzz-Adoption-Config.md`'s 15s refresh) — or long-running turns will be killed by the host. This is a correctness requirement, not a cosmetic one.

## ACP Protocol Types (from `coder/acp-go-sdk`, not redefined by BaoBot)

Confirmed via research.md against the real `buzz-acp` binary: `initialize`/`initializeResult`, `authenticate`, `session/new`, `session/prompt` (response `stopReason` ∈ `{EndTurn, Cancelled, MaxTokens, MaxTurnRequests, Refusal}` + usage block), `session/update` (notification; kinds `acp::stream`, `acp::tool`, `acp::plan`, `acp::thought`, `acp::usage`), `session/cancel`, `session/request_permission`, `session/set_config_option`, `session/set_model`. BaoBot's ACP package uses the SDK's Go types directly for these — no BaoBot-side redefinition.

## Mapping Table: ACP ↔ BaoBot domain

| ACP concept | BaoBot domain concept |
|---|---|
| `session/new` | Allocates a new `session` (no `domain.Task` yet — no work has been requested). |
| `session/prompt` | Constructs one `domain.Task` from the prompt content, calls `WorkerFactory.New().Execute(ctx, task)` synchronously, emits keep-alive `session/update`s while it runs, maps the resulting `domain.TaskResult` to the ACP response. |
| `session/cancel` | Cancels the `context.Context` passed to the in-flight `Worker.Execute` call for that session. |
| `session/update` (`acp::stream`) | Final (v1) or incremental (future, if `Worker` gains streaming) `TaskResult.Output` content. |
| `session/update` (`acp::usage`) | No source exists for v1 (no `BudgetTracker`) — omitted; `PromptResponse.Usage` left `nil`, which the SDK marks optional. |
