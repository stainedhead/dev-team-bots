# Data Dictionary: ACP Harness Feature Parity

**Created:** 2026-08-15

No new types introduced. All FRs reuse existing types:

## Entities

- `domain.BoardStore` (existing, implemented by `orchestratorlocal.InMemoryBoardStore`) — ACP mode gains its own instance (FR-402), not a new type.
- `domain.PluginStore` (existing, implemented by `localplugin.LocalPluginStore`) — same (FR-404).

## Value Objects

None new.

## Interfaces

- `localmcp.Client`'s existing `WithBoardStore`/`WithPluginStore`/`WithInstallDir`/`WithCLIRunner`/`WithCLITools` functional options (existing, already used by native mode) — ACP mode's `acp.go` construction gains calls to these, no interface changes.

## Enumerations

- `isConversationalSource`'s recognized source strings (existing, `execute_task.go`) — gains `"acp"` alongside `"chat"`/`"buzz"` (FR-401). Not a formal enum type, just an extended string match.
