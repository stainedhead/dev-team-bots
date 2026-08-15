# Plan: ACP Harness Feature Parity

**Created:** 2026-08-15
**Status:** Planning

## Development Approach

TDD throughout, per AGENTS.md. `acp.go` stays wiring-only — any new decision logic (e.g., "should the plugin store construct") belongs in a small, directly-testable helper function, not inline in `run()`/`buildACPAgent`, mirroring the `newBuzzMonitorBuilder` extraction pattern from the earlier typed-nil-panic fix in this same file's sibling `main.go`.

## Phase Breakdown

1. Research (this directory's `research.md`) — resolve RQ1 (persistence path) and RQ2 (config flag reuse) before task breakdown.
2. Architecture (`architecture.md`) — finalize the extraction/wiring design.
3. Task breakdown (`tasks.md`) — concrete TDD tasks.
4. Implementation — FR-401 (small, independent) can land first/in parallel; FR-402/403 (board) and FR-404/405 (plugin/CLI) are somewhat independent of each other, both depend on RQ2's resolution.

## Critical Path

FR-401 is fully independent — smallest, safest first task. FR-402→FR-403 (board store then wiring it) and FR-404→FR-405 (plugin store then wiring it, plus CLI tools) can proceed in parallel once RQ1/RQ2 are resolved. FR-406 (docs) is independent of all code work.

## Testing Strategy

- FR-401: test asserting an ACP-sourced task with `chat_provider` configured actually selects that provider.
- FR-402/404: test asserting store construction succeeds/fails gracefully (logged, not fatal) under both good and bad config.
- FR-403/405: end-to-end test — an ACP-sourced task successfully calls a board-completion/plugin/CLI tool, not just "the option was passed to the constructor."
- Full suite + `-race -gcflags=all=-d=checkptr=0` + `golangci-lint` + `gofmt` + `go vet` after each fix.

## Rollout Strategy

Same branch, same PR (not yet opened) as this dev-flow run. ACP mode's existing tests (turn handling, fallback-publish) must keep passing untouched.

## Success Metrics

All FR-401–FR-406 acceptance criteria in spec.md met.
