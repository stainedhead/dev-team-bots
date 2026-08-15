# PRD: ACP Harness Feature Parity

**Created:** 2026-08-15
**Jira:** N/A
**Status:** Draft

## Problem Statement

`boabot -acp` (ACP mode, driven externally by `buzz-acp` over the Agent Client Protocol) and native daemon mode's task execution both call the exact same `ExecuteTaskUseCase.Execute` loop and share its `maxToolIterations = 50` cap (`execute_task.go:12,122-178`) — confirmed by direct code comparison, not assumption. There is no separate, stricter iteration limit in ACP mode. But the two modes *wire* that shared engine very differently, and the gap is real and observable in production: the live `orchestrator` persona, running in ACP mode, has repeatedly hit `stop_reason=refusal err="execute task: exceeded max tool iterations (50)"` on tasks that complete cleanly via native mode's web-UI chat (which dispatches through the identical execution engine, just with richer wiring).

Three concrete wiring gaps were found:

1. **Model provider:** ACP-sourced tasks are tagged `Source: "acp"` (`turn.go:47`). `isConversationalSource` (`execute_task.go:20-22`) only recognizes `"chat"`/`"buzz"`, so a configured `models.chat_provider` never applies to ACP tasks — they always run on the bot's plain default provider, even when a better-tuned provider is configured and available.
2. **Tool set:** native mode's MCP client is built with `WithBoardStore`, `WithPluginStore`/`WithInstallDir`, and `WithCLIRunner`/`WithCLITools` (`team_manager.go:1023-1034`). ACP mode's client (`acp.go:122`, `localmcp.NewClient(cfg.Orchestrator.WorkDirs)`) has none of them — a materially smaller tool surface for the same kind of task.
3. **Mid-task clarifying questions:** native mode wires an "ask channel" so the model can ask a clarifying question mid-task instead of guessing or burning extra tool-call turns; ACP mode has no equivalent interrupt/resume point in its single-shot `Prompt` handler (`turn.go:19-130`).

A narrower toolset and a non-tuned model are a plausible, testable explanation for why the same class of task needs more turns (and sometimes exceeds the 50-iteration cap) via ACP mode than via native mode.

## Goals

- ACP-sourced tasks benefit from a configured `models.chat_provider` the same way chat- and Buzz-sourced tasks already do.
- ACP mode's task execution has access to the same board-completion, plugin, and CLI tools native mode already provides, closing the tool-surface gap that plausibly drives extra iterations.
- Document, precisely and honestly, why the third gap (mid-task clarifying questions) is **not** being closed by this work, so it isn't silently reopened as a "missed requirement" later.

## Non-Goals

- **Not implementing mid-task clarifying questions for ACP mode.** Confirmed via direct protocol research: ACP's only relevant mechanism is `UnstableCreateElicitation`/`UnstableCompleteElicitation` (vendored `github.com/coder/acp-go-sdk v0.13.5`), explicitly marked unstable ("not part of the spec yet, and may be removed or changed at any point") and requiring the *client* (`buzz-acp`) to advertise and implement `ClientCapabilities.Elicitation` — which it does not today. This is blocked on upstream support outside this repo's control, not a scoping choice we're free to reverse by working harder.
- Not raising or making configurable the `maxToolIterations = 50` cap itself — that's a separate, narrower question the repo owner explicitly considered and declined in favor of this PRD's approach (closing the wiring gap, not raising the ceiling).
- Not changing native mode's web-UI chat or native-mode Buzz behavior — both already have full feature parity with each other and are the reference implementation this PRD brings ACP mode toward, not something being modified.
- Not merging ACP mode's process model with `TeamManager`/native mode's multi-bot orchestration. ACP mode remains a single-persona, no-team.yaml process (`acp.go:80-82`'s own documented design) — this PRD adds board/plugin/CLI infrastructure *to* that process, it does not restructure ACP mode into a TeamManager-driven mode.

## Functional Requirements

**FR-401:** `execute_task.go`'s provider-selection logic treats `Source: "acp"` as a conversational source alongside `"chat"`/`"buzz"`, so a configured `models.chat_provider` applies to ACP-sourced tasks the same way it already applies to chat- and Buzz-sourced ones.

**FR-402:** ACP mode's process (`cmd/boabot/acp.go`) constructs its own board-store instance — there is none today; `TeamManager.Run()` is the only current constructor of a `BoardStore` (`team_manager.go:449`) and ACP mode never touches `TeamManager` at all. Design question for the spec/research phase: does ACP mode's board persist to its own file path (parallel to, not shared with, any concurrently-running native-mode instance for a different persona), or is sharing ever meaningful given ACP and native mode are not expected to run for the same persona simultaneously post-cutover?

**FR-403:** ACP mode's MCP client is constructed with `WithBoardStore` wired to FR-402's board instance, giving ACP-sourced tasks the same `complete_board_item`-class tooling native mode has.

**FR-404:** ACP mode's process constructs its own plugin store (parallel to `localplugin.NewLocalPluginStore(installDir)`, `team_manager.go:521-522`) when a plugin install directory is configured for the persona, and wires `WithPluginStore`/`WithInstallDir` into its MCP client — closing the plugin-tool gap.

**FR-405:** ACP mode's MCP client is constructed with `WithCLIRunner`/`WithCLITools` when the persona's config enables CLI tool delegation, matching native mode's conditional wiring (`team_manager.go:1023-1034`).

**FR-406 (documentation only, no code):** `docs/architectural-decision-record.md` gains an entry explaining why mid-task clarifying questions remain ACP-mode-unsupported, citing the unstable-protocol/upstream-dependency finding above, so a future contributor doesn't attempt this without first checking whether `buzz-acp` has since added elicitation support.

## Non-Functional Requirements

- **Performance:** Standing up board/plugin store infrastructure in ACP mode's process must not measurably slow down `boabot -acp` startup (it already constructs memory/vector stores and a provider at startup; this is incremental, not a new category of cost).
- **Reliability:** A failure to construct the board/plugin store (bad path, permissions) must degrade gracefully — ACP mode should still start and execute tasks without those tools available, logged clearly, not fail to start entirely. Mirrors the existing "Buzz monitor failure is isolated, doesn't crash the process" pattern established in earlier work.
- **Security:** No change to secret handling — this PRD adds tool *surface*, not new credential paths.
- **Observability:** Logs at ACP-mode startup should state clearly whether board/plugin/CLI tools activated for this persona, the same way Buzz monitor activation is already logged — an operator should be able to tell from the log alone whether FR-402–FR-405 are actually active for a given ACP-mode run, not just assume it from config.

## Acceptance Criteria

- [ ] An ACP-sourced task with `models.chat_provider` configured runs on that provider, verified by a test asserting the provider selection, not just config presence.
- [ ] ACP mode constructs a board store at startup and an ACP-sourced task can successfully call a board-completion tool, verified end-to-end (not just "the option was passed").
- [ ] ACP mode constructs a plugin store when configured and an ACP-sourced task can successfully invoke a plugin-provided tool.
- [ ] ACP mode wires CLI tools when configured and an ACP-sourced task can successfully invoke one.
- [ ] A board/plugin-store construction failure at ACP-mode startup is logged clearly and does not prevent `boabot -acp` from starting.
- [ ] `docs/architectural-decision-record.md` documents the mid-task-question non-goal with the specific upstream blocker cited.
- [ ] Existing ACP-mode tests (turn handling, fallback-publish, etc.) still pass, untouched by this work except where FR-401–FR-405 require new test coverage alongside them.
- [ ] Existing native-mode behavior (web-UI chat, native-mode Buzz) is unchanged — this PRD only adds capability to ACP mode's own process construction.

## Dependencies and Risks

| Item | Type | Notes |
|------|------|-------|
| `BoardStore`/`PluginStore`/CLI-tool infrastructure (existing, `internal/infrastructure/local/orchestrator`, `internal/infrastructure/local/plugin`) | Dependency | Reused, not replaced — ACP mode gets its own instances of already-existing types, per FR-402/FR-404. |
| ACP mode's process lifecycle and persistence path design (FR-402) | Risk | Genuinely new design surface — ACP mode has never owned persistent board/plugin state before. Needs research-phase resolution before implementation, not assumed. |
| `buzz-acp` upstream elicitation support | Dependency (external, out of this repo's control) | The mid-task-question gap (Non-Goal) stays open until/unless `buzz-acp` implements the unstable ACP `elicitation` extension — outside this PRD's or this repo's control to close. |
| Multiple ACP-mode processes potentially running concurrently for different personas (e.g. `orchestrator` and `tech-lead` both in ACP mode at once) | Risk | If FR-402's board is per-persona/per-process (not shared), confirm this doesn't create N separate boards an operator has no unified view of — acceptable given ACP mode has no web UI to view a board in anyway, but worth stating explicitly rather than assuming. |

## Open Questions

- FR-402's exact persistence design: per-persona file path under the ACP process's own memory root, or some other scheme? Deferred to research phase.
- Should FR-403–FR-405's tool wiring be unconditional (always on if the underlying store/dir constructs successfully) or gated behind a new explicit config flag (e.g. `orchestrator.enabled: true` in the ACP-mode persona's own config, which today is read by native mode's team.yaml path but never checked by `acp.go` at all per the research findings)? Using the existing `orchestrator.enabled` flag as ACP mode's own opt-in signal is the more consistent design (reuses an existing, already-understood config field rather than inventing a new one) but needs confirmation this doesn't have an unintended interaction with native mode's own use of that same flag for a *different* purpose (control-plane/API/UI activation) if both modes ever read the same persona's config file.
- Whether FR-401's `chat_provider` extension to `"acp"` should be unconditional or itself config-gated — no strong reason found to gate it (chat/buzz aren't gated), default to unconditional unless research surfaces a reason not to.
