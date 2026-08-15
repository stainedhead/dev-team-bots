# Status: ACP Harness Feature Parity

**Created:** 2026-08-15
**Last Updated:** 2026-08-15

## Overall Progress

| Phase | Description | Status |
|-------|-------------|--------|
| 0 | Initial Research (PRD/Feature Research) | Complete |
| 1 | Specification (spec.md) | Complete |
| 2 | Research & Data Modeling | Complete |
| 3 | Architecture & Planning | Complete |
| 4 | Task Breakdown | Complete |
| 5 | Implementation | Complete |
| 6 | Completion & Archival | Not Started |

## Phase 0 Task Checklist

- [x] Spec directory created (`specs/260815-acp-harness-feature-parity/`)
- [x] PRD reviewed (`/review-prd`) — verdict: Ready for spec, no gaps found
- [x] Research questions identified (see `research.md`, seeded from PRD Open Questions + prior code-comparison research)
- [x] Phase files initialized (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md)

## Phases 2-4 Task Checklist

- [x] RQ1 (board persistence path) resolved — reuse `memPath` already computed in `acp.go:103`, board store at `<memPath>/board.json`.
- [x] RQ2 (config flag reuse) resolved — do NOT reuse `orchestrator.enabled`; gate each feature on its own granular field instead.
- [x] RQ3 (native mode's exact wiring conditions) resolved — `Bot.Type != "tech-lead"` (board), `Orchestrator.Plugins.InstallDir != ""` (plugin), unconditional runner + per-tool `Enabled` (CLI).
- [x] RQ4 (`RulesTracker` precedent) resolved — confirms this pattern is already established/shipped in `acp.go`, not a new idea.
- [x] `spec.md` FR-402/404/405 updated with concrete gating conditions; Breaking Changes and Risks table corrected.
- [x] `architecture.md` populated with concrete design and 3 recorded architectural decisions.
- [x] `tasks.md` populated with 6-task, 5-phase breakdown.

## Phase 5 Task Checklist

- [x] P1.1 — `isConversationalSource` recognizes `"acp"` (FR-401). Deviation: also wired `WithChatProvider` into `cmd/boabot/acp.go` (not in FR-401's literal text) — required to meet spec.md's acceptance criterion #1; see `implementation-notes.md`.
- [x] P2.1 — ACP mode constructs its own board store (FR-402)
- [x] P2.2 — Wire `WithBoardStore` into ACP mode's MCP client (FR-403), verified end-to-end (`complete_board_item` actually marks a seeded work item done)
- [x] P3.1 — ACP mode constructs its own plugin store (FR-404), verified end-to-end (`read_skill` actually returns an installed plugin's skill content), including the relative-install-dir-resolved-against-memPath and construction-failure-degrades-gracefully edge cases
- [x] P4.1 — Wire CLI runner + per-tool CLI tools (FR-405), verified end-to-end (`run_opencode` actually spawns a real subprocess and returns its output)
- [x] P5.1 — Document the mid-task-question non-goal (FR-406): `docs/architectural-decision-record.md` ADR-B030 added, citing `acp-go-sdk`'s `UnstableCreateElicitation`/`UnstableCompleteElicitation` and confirming `buzz-acp`'s method set has no `elicitation/*` handling. Also updated `docs/technical-details.md`, `docs/product-details.md`, and `user-docs/ACP-Harness-Adoption-Config.md` to reflect the new tool/provider parity and the mid-task-question gap (AGENTS.md's "keep docs in sync" requirement).

## Blockers

- None currently.

## Recent Activity

- 2026-08-15: Spec directory created from `acp-harness-feature-parity-PRD.md`; PRD moved into spec directory. This PRD followed a direct code comparison between ACP mode and native mode's task execution (triggered by observing repeated `exceeded max tool iterations (50)` failures on the live orchestrator persona), which found the two modes share the identical execution loop but differ in provider selection, tool-set wiring, and mid-task question support (the last confirmed infeasible without upstream protocol support and scoped as a Non-Goal).
- 2026-08-15: `/review-spec` run; codebase research resolved all 4 research questions concretely, most notably determining `orchestrator.enabled` should NOT be reused as the tool-wiring signal (it only means "start the dashboard" and doesn't even gate board wiring in native mode). Spec now implementation-ready with exact, precedent-matched gating conditions for every FR.
- 2026-08-15: P1.1 implemented (TDD red-green). Found during implementation that `acp.go` never called `WithChatProvider` at all, so the FR-401 string-match fix alone would have been inert in production — added chat-provider wiring to `acp.go` as an in-scope deviation (see implementation-notes.md) to actually satisfy acceptance criterion #1.
- 2026-08-15: P2.1/P2.2/P3.1/P4.1 implemented together (all share `cmd/boabot/acp.go`, per plan). Extracted `buildACPMCPOptions` (board/plugin/CLI wiring, all three FRs) and `buildACPWorker` (chat provider + calls buildACPMCPOptions) out of `buildACPAgent`, keeping `buildACPAgent` itself wiring-only per AGENTS.md's `cmd/` convention — mirrors `main.go`'s `newBuzzMonitorBuilder` extraction precedent named in the task brief. All three tool surfaces verified end-to-end (real board/plugin-store/subprocess side effects, not just constructor-argument assertions) in `cmd/boabot/acp_mcp_options_test.go`. `go test -race -gcflags=all=-d=checkptr=0 ./...`, `go vet ./...`, `golangci-lint run`, `gofmt -l .` all clean. Domain+application coverage confirmed unchanged at 92.2% (only application-layer change is the one-line `isConversationalSource` extension from P1.1, already covered). Committed as `3fcab64`.
- 2026-08-15: P5.1 implemented (docs only). ADR-B030 added to `boabot/docs/architectural-decision-record.md`, grounded in the vendored `github.com/coder/acp-go-sdk v0.13.5` source (`agent_gen.go:471-477`'s `UnstableCreateElicitation`/`UnstableCompleteElicitation`, `client_gen.go:10-49`'s optional-interface dispatch) and ADR-B026's confirmed `buzz-acp` method set (no `elicitation/*` handling observed). `docs/technical-details.md`, `docs/product-details.md`, and `user-docs/ACP-Harness-Adoption-Config.md` updated to describe the new tool/provider parity and disclose the mid-task-question gap to operators, per AGENTS.md's documentation-sync requirement. All 6 tasks complete; Phase 5 (Implementation) done.
