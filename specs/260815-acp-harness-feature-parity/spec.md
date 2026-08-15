# Spec: ACP Harness Feature Parity

**Created:** 2026-08-15
**Status:** Draft
**Source PRD:** [acp-harness-feature-parity-PRD.md](./acp-harness-feature-parity-PRD.md)

## Executive Summary

Closes two of three confirmed wiring gaps between ACP mode (`boabot -acp`) and native daemon mode's task execution — both share the identical `ExecuteTaskUseCase.Execute` loop and `maxToolIterations = 50` cap, but ACP mode never gets `chat_provider` benefit and has zero board/plugin/CLI tool infrastructure in its process. The third gap (mid-task clarifying questions) is documented as an explicit Non-Goal, blocked on an unstable upstream ACP protocol extension `buzz-acp` doesn't implement.

## Problem Statement

The live `orchestrator` persona, running in ACP mode, has repeatedly hit `stop_reason=refusal err="execute task: exceeded max tool iterations (50)"` on tasks that complete cleanly via native mode's web-UI chat — despite both paths sharing the exact same execution engine and iteration cap (confirmed via direct code comparison: `execute_task.go:12,122-178`). The difference is wiring, not the loop itself:

1. **Model provider:** ACP-sourced tasks (`Source: "acp"`, `turn.go:47`) never match `isConversationalSource`'s `"chat"`/`"buzz"` check (`execute_task.go:20-22`), so `models.chat_provider` never applies.
2. **Tool set:** native mode wires `WithBoardStore`/`WithPluginStore`/`WithInstallDir`/`WithCLIRunner`/`WithCLITools` (`team_manager.go:1023-1034`); ACP mode's client (`acp.go:122`) has none of them — confirmed via research that this isn't just unwired options, ACP mode's process has *no* board, plugin, or CLI-tool infrastructure standing up at all (`acp.go:80-82`'s own doc comment: "without going through team.yaml or TeamManager at all").
3. **Mid-task clarifying questions:** native mode has an ask-channel; ACP mode's single-shot `Prompt` handler (`turn.go:19-130`) has no interrupt/resume point. Confirmed infeasible without upstream `buzz-acp` support for ACP's unstable `elicitation` extension (`github.com/coder/acp-go-sdk v0.13.5`).

## Goals

- ACP-sourced tasks benefit from a configured `models.chat_provider` the same way chat/Buzz-sourced tasks already do.
- ACP mode's task execution gets access to the same board-completion, plugin, and CLI tools native mode already provides.
- Document precisely why the third gap isn't being closed, so it isn't silently reopened as a missed requirement later.

## Non-Goals

- Not implementing mid-task clarifying questions for ACP mode — blocked on upstream `buzz-acp` elicitation support, confirmed not implementable within this repo's control.
- Not raising or making configurable `maxToolIterations = 50` — a separate question the repo owner explicitly considered and declined in favor of closing the wiring gap instead.
- Not changing native mode's web-UI chat or native-mode Buzz behavior — both already have full parity and are the reference this PRD brings ACP mode toward.
- Not merging ACP mode's process model with `TeamManager`/native mode's multi-bot orchestration — ACP mode remains single-persona, no-team.yaml, per its own documented design.

## User Requirements / Functional Requirements

**FR-401:** `execute_task.go`'s provider-selection logic treats `Source: "acp"` as a conversational source alongside `"chat"`/`"buzz"`, so a configured `models.chat_provider` applies to ACP-sourced tasks.

**FR-402:** ACP mode's process (`cmd/boabot/acp.go`) constructs its own board-store instance. Design question (research phase): per-persona file path under the ACP process's own memory root, parallel to (not shared with) any concurrently-running native-mode instance.

**FR-403:** ACP mode's MCP client is constructed with `WithBoardStore` wired to FR-402's board instance.

**FR-404:** ACP mode's process constructs its own plugin store when a plugin install directory is configured, and wires `WithPluginStore`/`WithInstallDir` into its MCP client.

**FR-405:** ACP mode's MCP client is constructed with `WithCLIRunner`/`WithCLITools` when CLI tool delegation is enabled for the persona, matching native mode's conditional wiring.

**FR-406 (documentation only):** `docs/architectural-decision-record.md` gains an entry explaining why mid-task clarifying questions remain ACP-mode-unsupported, citing the unstable-protocol/upstream-dependency finding.

## Non-Functional Requirements

- **Performance:** Standing up board/plugin infrastructure must not measurably slow ACP-mode startup — incremental cost on top of what it already constructs (memory/vector stores, provider).
- **Reliability:** A board/plugin store construction failure must degrade gracefully — ACP mode still starts and executes tasks without those tools, logged clearly, not a hard startup failure. Mirrors the existing Buzz-monitor-failure-isolation pattern from earlier work.
- **Security:** No new credential paths — this adds tool surface, not secret handling changes.
- **Observability:** ACP-mode startup logs must state clearly whether board/plugin/CLI tools activated for this persona, mirroring how Buzz monitor activation is already logged.

## System Architecture

- **Affected layers:** `cmd/boabot/acp.go` (new board/plugin store construction, MCP client wiring), `internal/application/execute_task.go` (FR-401's source-check extension).
- **New components:** ACP-mode's own `BoardStore`/`PluginStore` instances (reusing existing types from `internal/infrastructure/local/orchestrator`/`internal/infrastructure/local/plugin`, not new implementations).
- **Out of scope architecturally:** `TeamManager`/native mode (`internal/application/team`) — untouched; ACP mode gains its own parallel infrastructure, not a shared one.

## Scope of Changes

- Files to modify: `boabot/cmd/boabot/acp.go` (board/plugin store construction, MCP client options), `boabot/internal/application/execute_task.go` (`isConversationalSource`), `boabot/docs/architectural-decision-record.md` (FR-406).
- Files to create: none expected — reuses existing store implementations.
- Dependencies: existing `orchestratorlocal.NewInMemoryBoardStore`, `localplugin.NewLocalPluginStore`, `localmcp.WithBoardStore`/`WithPluginStore`/`WithInstallDir`/`WithCLIRunner`/`WithCLITools` (all already exist, used by native mode today).

## Breaking Changes

None expected to public config schema — reuses existing `orchestrator.enabled`-style config surface (exact reuse decision deferred to research, see Open Questions) rather than inventing new fields.

## Success Criteria and Acceptance Criteria

- [ ] An ACP-sourced task with `models.chat_provider` configured runs on that provider (test asserts provider selection, not just config presence).
- [ ] ACP mode constructs a board store at startup and an ACP-sourced task can successfully call a board-completion tool, verified end-to-end.
- [ ] ACP mode constructs a plugin store when configured and an ACP-sourced task can successfully invoke a plugin-provided tool.
- [ ] ACP mode wires CLI tools when configured and an ACP-sourced task can successfully invoke one.
- [ ] A board/plugin-store construction failure at ACP-mode startup is logged clearly and does not prevent `boabot -acp` from starting.
- [ ] `docs/architectural-decision-record.md` documents the mid-task-question non-goal with the specific upstream blocker cited.
- [ ] Existing ACP-mode tests still pass, untouched except where FR-401–FR-405 add new coverage alongside them.
- [ ] Existing native-mode behavior (web-UI chat, native-mode Buzz) is unchanged.

**Quality gates:** `go fmt`, `go vet`, `golangci-lint run`, `go test -race -gcflags=all=-d=checkptr=0 ./...` all clean; domain+application aggregate coverage ≥90% (currently 92.2%, must not regress). Note: `cmd/` is excluded from the coverage gate per AGENTS.md, but `acp.go`'s new logic should still be tested directly, not left untested just because it's outside the enforced threshold.

## Risks and Mitigation

| Item | Type | Notes | Mitigation |
|------|------|-------|------------|
| ACP mode's persistence/lifecycle design (FR-402) | Risk | Genuinely new design surface — ACP mode has never owned persistent board/plugin state before. | Resolve concretely during research phase, not assumed; see Open Questions. |
| `buzz-acp` upstream elicitation support | Dependency (external) | Mid-task-question gap stays open until/unless upstream implements it — outside this repo's control. | Documented as Non-Goal (FR-406), not silently dropped. |
| Multiple ACP-mode processes for different personas running concurrently | Risk | If FR-402's board is per-persona/per-process, confirm no unified-view expectation is broken (acceptable since ACP mode has no web UI to view a board in anyway). | State explicitly in implementation notes, don't assume. |
| Reusing `orchestrator.enabled` as ACP mode's tool-wiring opt-in signal | Risk | That flag today only means something to native mode (control-plane/API/UI activation) — reusing it for ACP mode's tool wiring needs confirmation it has no unintended interaction if the same persona's config is ever read by both modes. | Resolve during research phase; see Open Questions. |

## Timeline and Milestones

[TBD] — tracked via `status.md`.

## References

- Source PRD: [acp-harness-feature-parity-PRD.md](./acp-harness-feature-parity-PRD.md)
- Prior features (context/precedent): `specs/archive/260814-boabot-native-daemon-mode/`, `specs/archive/260814-buzz-dm-and-thread-support/`
