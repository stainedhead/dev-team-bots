# Architecture: Channel-Agnostic Agent Tool Parity

**Created:** 2026-08-17
**Status:** Draft

## Architecture Overview

New MCP tool handlers only, in `internal/infrastructure/local/mcp/client.go`, attached via the same `WithBoardStore`/board-store-presence gate that already conditions `complete_board_item`. No new domain interfaces, no new construction sites — the channel-agnostic property comes for free because tool wiring already doesn't branch on `task.Source` anywhere (confirmed during PRD research), so adding a tool to the shared `localmcp.Client` construction path makes it available to every channel/mode that already gets `complete_board_item`.

## Architectural Decisions

- New tools live in the same file/registration pattern as `complete_board_item`, not a separate package — this is additive to an existing, working mechanism, not a new one.
