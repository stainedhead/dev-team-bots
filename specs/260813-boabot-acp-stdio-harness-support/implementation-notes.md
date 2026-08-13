# Implementation Notes: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13

## Purpose

Living record of implementation decisions, edge cases, and deviations from plan.md, updated as work proceeds. Update this file whenever a non-obvious decision is made during implementation — do not wait until the end.

## Technical Decisions

- **Worker resolved once at construction, not per-turn.** `acp.New(workerFactory, workDir)` calls `workerFactory.New()` once and stores the resulting `domain.Worker`, rather than storing the factory and calling `New()` per session/turn. Native mode's own `simpleWorkerFactory.New()` always returns the same cached instance anyway (it's a singleton wrapper), so this is behaviorally identical and simpler.
- **Optional progress-hook detection via local interface, not a domain change.** `internal/infrastructure/acp` defines its own `progressReporter` interface (`WithProgressHandler(func(taskID, line string))`) and type-asserts the resolved `domain.Worker` against it, rather than adding this method to `domain.Worker` itself. `*application.ExecuteTaskUseCase` already has this method; the type assertion picks it up without touching the domain layer (architecture.md AD-1).
- **Keep-alive is progress-driven when available, ticker fallback otherwise.** Both signals feed the same `session/update` (`acp::thought`) emission path concurrently with the blocking `Worker.Execute` call.
- **`Prompt` waits for the keep-alive goroutine to fully exit before returning** (`sync.WaitGroup`), not just signaling it to stop. Caught by `go test -race` during TDD: without this, a caller could observe (or a real ACP client could receive) a `session/update` arriving after the `session/prompt` response, a real ordering bug, not just a test artifact.
- **`SOUL.md` is read from `filepath.Dir(configPath)`, not via `botsDir`/`team.yaml` lookup.** Since ACP mode is handed one persona's own `config.yaml` path directly (FR-004) and `SOUL.md` lives in the same directory in this repo's actual layout (verified: `boabot-team/bots/tech-lead/{config.yaml,SOUL.md}`), this avoids needing `cfg.Team.BotsDir`/`cfg.Bot.BotType` reconstruction that native mode needs (native mode iterates `team.yaml`'s bot list; ACP mode never touches `team.yaml` at all).
- **`domain.Task.WorkDir` defaults to `""` for ACP mode.** There is no persona-level "single work dir" config field (only `Orchestrator.WorkDirs`, a list used for MCP tool scoping) — `WorkDir` is documented as optional on `domain.Task`, and inventing new config surface for this wasn't justified by anything in the spec.

## Edge Cases & Solutions

- **Worker panics** are recovered in `runWorkerSafely` and mapped to `StopReasonRefusal`, never surfaced as a raw ACP transport error or process crash — verified by a dedicated test (`TestAgent_Prompt_WorkerPanicIsRecovered`) using a worker that always panics.
- **Cancellation** (`session/cancel`) reads the session's `context.CancelFunc` under `Agent.mu` before calling it outside the lock, rather than calling it while holding the lock — avoids a lock-ordering concern and was verified race-clean under `-race` with a concurrent Prompt+Cancel test.
- **Unknown `session/prompt` sessionId** returns a plain Go error (matching `coder/acp-go-sdk`'s own `example/agent`'s convention for this case, rather than a typed `*sdk.RequestError`).

## Deviations from Plan

- **FR-005 (budget/usage) corrected mid-implementation.** `domain.BudgetTracker` does not exist anywhere in this codebase — grep-verified independently twice (once by the first implementation attempt, once by the coordinator). `boabot/AGENTS.md`'s "Key Interfaces" table describes it, but no such type, file, or package exists under `internal/domain`, `internal/application`, or `internal/infrastructure`. This is a **pre-existing gap in native daemon mode too**, not something this feature introduced or made worse. spec.md, data-dictionary.md, architecture.md, plan.md, and tasks.md were all corrected in place (not just noted here) so a future reader doesn't have to rediscover this. v1 leaves `PromptResponse.Usage` nil (the ACP SDK marks it optional). **Follow-up recommendation for the user:** if real per-bot budget/token-cap enforcement is wanted, that's a standalone piece of work — `boabot/AGENTS.md`'s documentation should also be corrected to stop describing a `BudgetTracker` interface that doesn't exist, independent of whether the feature itself ever gets built.
- **FR-003 (streaming) was already refined during architecture, before implementation began** — see architecture.md AD-3. Implementation confirmed this was the right call: `domain.Worker.Execute` genuinely has no incremental-output hook at the interface level; the `progressReporter` type-assertion (see Technical Decisions) is the closest available signal, and per-tool-call progress lines from `*application.ExecuteTaskUseCase` are in practice a good proxy for "real" streaming even though they aren't token-level.
- **`newLocalProviderFactory` → `NewLocalProviderFactory` export**, plus changing its return type to `domain.ProviderFactory` (an interface that already existed) rather than the unexported `*localProviderFactory` concrete type — avoids an `unexported-return` lint smell. Two call sites updated (`team_manager.go`, `export_test.go`); `internal/application/team`'s existing test suite passes unchanged, confirming no behavior change.

## Known Pre-Existing Issue (not fixed here, out of scope)

`internal/infrastructure/buzz` has a `checkptr` **fatal error** (not just a failing test — `go test` aborts that package's binary) under `go test -race`, in `TestE3_NIPOAAuthTagIncludedOnAuthEvent` → `RelayClient.Authenticate` → `fiatjaf.com/nostr`'s `Event.Sign`/`serializedHash`/`writeJSONString`. This is unsafe pointer arithmetic inside the third-party `fiatjaf.com/nostr` dependency (or in how the `buzz` package invokes it), entirely unrelated to this PRD. Confirmed pre-existing by `git diff origin/main -- boabot/internal/infrastructure/buzz` returning empty — this ACP feature makes zero changes there. Without `-race`, the same test passes. Recommend the user file this as a separate issue against native Buzz support; not addressed as part of this PRD's scope.

## Lessons Learned

- Reading the actual SDK example (`coder/acp-go-sdk`'s `example/agent/main.go`) directly from the module cache, rather than relying on `go doc` summaries alone, was necessary to get the `SetConnection`-after-`NewAgentSideConnection` wiring idiom right — `go doc` shows types and signatures but not usage patterns.
- `go test -race` caught two genuine concurrency bugs during this implementation (the `Cancel` lock-scope issue and the keep-alive-goroutine-outliving-`Prompt` issue) that a non-race test run would have missed entirely — both are documented above under Technical Decisions / Edge Cases.
