# Research: ACP Harness Feature Parity

**Created:** 2026-08-15
**Source PRD:** [acp-harness-feature-parity-PRD.md](./acp-harness-feature-parity-PRD.md)

## Research Questions

1. FR-402's exact persistence design: what path should ACP mode's own board-store JSON file live at? Check what memory/vector store paths `acp.go` already constructs (it must derive a per-persona directory somehow) and mirror that convention for the board store, rather than inventing a new path scheme.
2. Should FR-403–FR-405's tool wiring reuse the existing `orchestrator.enabled` config flag as ACP mode's opt-in signal, or introduce a new flag? Confirm whether `orchestrator.enabled: true` in a persona's `config.yaml` could realistically be read by BOTH an ACP-mode process and a native-mode process for the same persona at different times (per the cutover model, they're not meant to run concurrently for the same persona, but the same config.yaml file is shared across modes) — if so, does reusing this flag for ACP mode's tool-wiring risk a confusing double-meaning, or is it actually the cleanest signal precisely because it already means "this persona wants the full control-plane experience"?
3. What plugin install-dir and CLI-tool-enablement config fields does native mode read to decide whether to wire `WithPluginStore`/`WithCLIRunner` (`team_manager.go:1023-1034` exact conditions)? ACP mode needs to read the identical fields from the same persona config, not invent parallel ones.
4. Does `acp.go`'s existing `RulesTracker` construction (which already reads `cfg.Orchestrator.WorkDirs`, per prior research) offer a reusable pattern for how ACP mode should read persona-scoped config fields it doesn't currently touch?

## Industry Standards

[TBD — not relevant; internal wiring/infrastructure work.]

## Existing Implementations

- `team_manager.go:1023-1034` — native mode's conditional tool-wiring logic, the reference implementation FR-403–FR-405 must match the conditions of (not just the mechanism).
- `orchestratorlocal.NewInMemoryBoardStore`, `localplugin.NewLocalPluginStore` — existing store constructors to reuse, not reimplement.
- `execute_task.go:20-22` (`isConversationalSource`) — existing function FR-401 extends with one more matched string.
- The prior ACP-mode research pass (mid-conversation, not in this repo) already confirmed `acp.go:80-150` is ACP mode's entire construction path and never touches `TeamManager`.

## API Documentation

[TBD — no external APIs; internal Go code only.]

## Best Practices

[TBD]

## Open Questions

- RQ2 (config flag reuse vs. new flag) — the more consequential open question; affects both the PRD's Breaking Changes claim ("reuses existing config surface") and actual implementation shape. Must be resolved before task breakdown.
- RQ1 (persistence path convention) — must be resolved before FR-402 implementation, low risk of surprising findings.

## References

- Source PRD: [acp-harness-feature-parity-PRD.md](./acp-harness-feature-parity-PRD.md)
- Prior features (research-question resolution precedent): `specs/archive/260814-boabot-native-daemon-mode/research.md`, `specs/archive/260814-buzz-dm-and-thread-support/research.md`
