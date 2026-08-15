# Architecture: ACP Harness Feature Parity

**Created:** 2026-08-15
**Status:** Draft

## Architecture Overview

[TBD — to be filled in Phase 3, after research resolves RQ1 (persistence path) and RQ2 (config flag reuse).]

## Component Architecture

- ACP-mode board store construction (new call site in `acp.go`, existing type).
- ACP-mode plugin store construction (new call site in `acp.go`, existing type).
- ACP-mode MCP client wiring extension (`WithBoardStore`/`WithPluginStore`/`WithInstallDir`/`WithCLIRunner`/`WithCLITools`).
- `execute_task.go`'s `isConversationalSource` extension.

## Layer Responsibilities

- **Domain:** unchanged — `BoardStore`/`PluginStore` interfaces already exist.
- **Application:** `execute_task.go`'s provider-selection logic extended (FR-401).
- **Infrastructure/cmd:** `acp.go` (wiring-only, per AGENTS.md's `cmd/` convention) gains store construction and MCP client option calls — no business logic added, matching the existing pattern native mode's `team_manager.go` already follows.

## Data Flow

[TBD — pending RQ1/RQ2 resolution.]

## Sequence Diagrams

[TBD]

## Integration Points

- Existing `orchestratorlocal`/`localplugin`/`localmcp` packages — reused, not extended with new capabilities.

## Architectural Decisions

[TBD — record here once RQ1/RQ2 are resolved during research.]
