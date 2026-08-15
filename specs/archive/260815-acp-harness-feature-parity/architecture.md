# Architecture: ACP Harness Feature Parity

**Created:** 2026-08-15
**Status:** Draft

## Architecture Overview

`buildACPAgent` (`acp.go:83-150`) gains three additive construction steps, inserted after the existing memory/vector store construction (which already computes the `memPath` the board store reuses) and before the MCP client is finalized:

1. Board store at `filepath.Join(memPath, "board.json")`, gated on `cfg.Bot.Type != "tech-lead"`.
2. Plugin store gated on `cfg.Orchestrator.Plugins.InstallDir != ""`.
3. CLI runner (unconditional) + per-tool CLI tools gated on `cfg.Orchestrator.CLITools.<tool>.Enabled`.

All three mirror `team_manager.go:1020-1036`'s exact conditions — not approximated. `execute_task.go`'s `isConversationalSource` gains `"acp"` as a fourth recognized source string (FR-401), independent of the above three.

## Component Architecture

- ACP-mode board store construction (new call site in `acp.go`, existing `orchestratorlocal.NewInMemoryBoardStore` type, gated on persona type).
- ACP-mode plugin store construction (new call site in `acp.go`, existing `localplugin.NewLocalPluginStore` type, gated on install-dir presence).
- ACP-mode MCP client wiring extension (`WithBoardStore`/`WithPluginStore`/`WithInstallDir`/`WithCLIRunner`/`WithCLITools`), each gate matching native mode's exact condition.
- `execute_task.go`'s `isConversationalSource` extension (`"acp"` added).

## Layer Responsibilities

- **Domain:** unchanged — `BoardStore`/`PluginStore` interfaces already exist.
- **Application:** `execute_task.go`'s provider-selection logic extended (FR-401).
- **Infrastructure/cmd:** `acp.go` (wiring-only, per AGENTS.md's `cmd/` convention) gains store construction and MCP client option calls — no business logic added, matching the existing pattern native mode's `team_manager.go` already follows.

## Data Flow

`buildACPAgent` constructs `memPath` (existing) → board store at `memPath/board.json` (new, type-gated) → plugin store (new, install-dir-gated) → CLI runner/tools (new, runner unconditional/tools per-tool-gated) → all four handed to `localmcp.NewClient` via the same functional-options pattern native mode already uses → `ExecuteTaskUseCase` (unchanged) sees a richer tool set for ACP-sourced tasks, same as it already does for chat/Buzz-sourced ones.

## Sequence Diagrams

[Deferred — the data-flow description above is sufficiently precise; this is additive wiring into an existing linear construction function, not a new interaction pattern worth diagramming.]

## Integration Points

- Existing `orchestratorlocal`/`localplugin`/`localmcp` packages — reused, not extended with new capabilities.

## Architectural Decisions

- **Do not reuse `orchestrator.enabled`.** It specifically means "start the HTTP dashboard" in native mode and doesn't even gate board-store wiring there — reusing it for ACP mode would conflate unrelated concepts and misrepresent how native mode itself gates features. Each ACP feature is gated on its own granular field instead, exactly matching native mode's real per-feature conditions.
- **Board store path reuses the already-computed `memPath`, no new path-construction logic.** Consistent with the existing memory/vector store convention in the same function.
- **No new config fields.** All three gating conditions (`Bot.Type`, `Orchestrator.Plugins.InstallDir`, `Orchestrator.CLITools.*`) already exist and are already read by native mode for the identical purpose — ACP mode reads the same fields from the same persona config file, not parallel ACP-specific fields.
