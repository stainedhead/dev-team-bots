# Plan: Channel-Agnostic Agent Tool Parity

**Created:** 2026-08-17
**Status:** Planning

## Development Approach

TDD per FR, sequential (FR-601 → FR-602 → FR-604 audit → FR-603 stretch if time permits), given demo deadline.

## Phase Breakdown

1. FR-601: board-read tool, TDD, wired in `localmcp.Client`.
2. FR-602: task-status-read tool, TDD, same pattern.
3. FR-604: audit pass across all existing tools for hidden source-conditioning.
4. FR-603 (stretch, cut first if short on time): team-registry-read tool, native-mode-only.
5. Live verification: rebuild, restart native process + ACP pool, manually re-test over real Buzz conversation.

## Testing Strategy

Unit tests at the MCP client level (mirroring existing `complete_board_item` test patterns) plus an end-to-end test proving a model can actually invoke the new tool and get real data back (matching this repo's existing "don't just assert the option was passed" convention, e.g. `TestBuildACPMCPOptions_BoardStore_EndToEnd_CompletesItem`).

## Rollout Strategy

Single PR, automerge per repo convention. Live verification is a hard requirement before the demo, not just automated tests.
