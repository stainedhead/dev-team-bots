# Research: Channel-Agnostic Agent Tool Parity

**Created:** 2026-08-17
**Source PRD:** [channel-agnostic-tool-parity-PRD.md](./channel-agnostic-tool-parity-PRD.md)

## Research Questions

1. Exact shape of `localmcp.Client`'s tool-registration pattern for `complete_board_item` (client.go ~line 154) — mirror it exactly for the new read tools' handler signature, error handling, and result formatting.
2. `domain.BoardStore.List(ctx, filter)`'s exact filter semantics — confirm an empty filter returns all items, sorted, so the new tool can pass `domain.WorkItemFilter{}` for "all items."
3. `domain.DirectTaskStore.List(ctx, botName)`'s exact signature and what "this bot's own tasks" resolves to inside the MCP client (does the client already know its own bot name at construction time, or does it need threading through?).
4. Where exactly `WithBoardStore`/`buildACPMCPOptions` construct/gate the board store today, to confirm the new tools attach at the identical site (team_manager.go for native, acp.go for ACP) rather than a new conditional.
5. (FR-603 only) Where native mode's team registry actually lives in-memory (`BotRegistry`?) and whether it's accessible from the MCP client construction site without threading new dependencies through.

## Findings

(Resolved directly during implementation — this feature is small and well-scoped enough that research and implementation happen together, matching the demo timeline.)

## References

- `acp-native-shared-state-PRD.md`'s predecessor work (`specs/archive/260816-acp-native-shared-state/`) for the exact tool-wiring gate pattern already established.
