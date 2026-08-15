# Research: ACP Harness Feature Parity

**Created:** 2026-08-15
**Source PRD:** [acp-harness-feature-parity-PRD.md](./acp-harness-feature-parity-PRD.md)

## Research Questions

1. ~~Persistence path convention~~ — **Resolved.** `buildACPAgent` (`acp.go:83-150`) already computes `memPath := filepath.Join(memRoot, cfg.Bot.Name)` (line 103) for the memory/vector stores. **Decision:** board store lives at `filepath.Join(memPath, "board.json")`, reusing the already-computed `memPath` — exactly mirrors native mode's `<memRoot>/<orchestratorName>/board.json` convention (`team_manager.go:445-449`), with `cfg.Bot.Name` standing in for `orchestratorName` since ACP mode has one persona per process.
2. ~~Config flag reuse vs. new flag~~ — **Resolved — do NOT reuse `orchestrator.enabled`.** Confirmed `Orchestrator.Enabled` is read in exactly two places outside native mode's own package (`team_manager.go:800,982`), both gating whether to start the HTTP/API/dashboard/UI server — a meaning that's inert in ACP mode (stdio-only, no HTTP server). Critically, native mode's own board-store wiring is **not** gated by `Enabled` at all (`tm.sharedBoard` constructed unconditionally, `team_manager.go:449`). Reusing it for ACP's tool wiring would conflate two unrelated concepts and misrepresent how native mode itself gates things. **Decision:** no new top-level flag needed either — gate each ACP feature on the same granular, mode-agnostic field native mode already uses per feature (see RQ3).
3. ~~Native mode's exact wiring conditions~~ — **Resolved**, precise conditions from `team_manager.go:1020-1036`:
   - `WithBoardStore`: gated on persona **type** != `"tech-lead"` (line 1023-1024), not any enabled flag — `tm.sharedBoard` is always non-nil.
   - `WithPluginStore`/`WithInstallDir`: gated on `cfg.Orchestrator.Plugins.InstallDir != ""` (line 513-516 pattern) — a persona-scoped config field, not an umbrella flag.
   - `WithCLIRunner`: unconditional (`cliagent.New()` always constructed, line 531) — the runner itself is always wired; `WithCLITools`: per-tool availability gated by each `CLIToolConfig.Enabled` bool inside `cfg.Orchestrator.CLITools` (line 526).
   - **Decision:** ACP mode mirrors these exact same conditions read from its own persona's `config.yaml` — board wiring gated on `cfg.Bot.Type != "tech-lead"`, plugin wiring on `cfg.Orchestrator.Plugins.InstallDir != ""`, CLI runner unconditional with per-tool `cfg.Orchestrator.CLITools.<tool>.Enabled` gating — not approximated, not gated on `Enabled`.
4. ~~`RulesTracker` precedent~~ — **Resolved, directly applicable.** `acp.go:136-137` already does exactly this pattern: `if len(cfg.Orchestrator.WorkDirs) > 0 { worker.WithRulesTracker(...) }`, its own doc comment stating it "mirrors team_manager.go's startBot exactly." This is a shipped precedent confirming the RQ2/RQ3 approach — ACP mode reading a specific persona-scoped `cfg.Orchestrator.*` field and gating on its own presence/value, never on `Enabled` — is the established, correct pattern in this codebase, not a new idea.

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

None remaining — all four research questions resolved concretely above. spec.md's "reuses existing config surface" Breaking Changes claim needs a small correction: it's accurate in spirit (no new top-level flag) but should specify *which* existing granular fields (`Orchestrator.Plugins.InstallDir`, `Orchestrator.CLITools.*`, `Bot.Type`), not `orchestrator.enabled` as originally implied.

## References

- Source PRD: [acp-harness-feature-parity-PRD.md](./acp-harness-feature-parity-PRD.md)
- Prior features (research-question resolution precedent): `specs/archive/260814-boabot-native-daemon-mode/research.md`, `specs/archive/260814-buzz-dm-and-thread-support/research.md`
