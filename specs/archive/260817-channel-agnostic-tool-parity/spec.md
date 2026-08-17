# Spec: Channel-Agnostic Agent Tool Parity (Board/Task/Team Read Access)

**Created:** 2026-08-17
**Status:** Draft
**Source PRD:** [channel-agnostic-tool-parity-PRD.md](./channel-agnostic-tool-parity-PRD.md)

## Executive Summary

Closes a read-side gap confirmed across every mode and channel this session: no tool anywhere lets an agent read board state, task/schedule status, or (in native mode) team registry — only `complete_board_item` (write-only) exists. This caused a confirmed demo-visible bug (an agent falsely reporting "no items" on a kanban board that has 6 real items) and blocks the goal of Buzz being a full-fidelity primary interface. **Demo tomorrow night — FR-601/602/604 are must-have, FR-603 is explicitly cuttable.**

## Problem Statement

An operator asking an agent "what's on the kanban board" gets a confidently wrong answer regardless of channel, including native mode's own web-UI chat. Root-caused via exhaustive code search (not assumption): `internal/infrastructure/local/mcp/client.go` has exactly one board-related tool (`complete_board_item`, write-only); `execute_task.go` has no per-source context injection; `board_dispatch.go` is human-initiated dispatch, not agent-initiated read. Both native chat and Buzz are equally blind — any earlier apparent "success" in native chat was very likely luck or misremembering, not a real capability difference.

## Goals

- Any tool available to a native-chat-sourced task is available identically to a task sourced from any channel (Buzz today, future channels by construction), except genuine documented per-mode constraints (ACP mode's single-persona/no-team scope).
- An agent can accurately read and report real board-item state and task/schedule status when asked, from any channel.
- Buzz functions as a genuine primary interface for everything native chat can do.

## Non-Goals

- Not building support for hypothetical future channels.
- Not redesigning the web UI's human-facing board/task rendering.
- Not adding new write capability beyond what exists.
- Not tackling budget tracking or context-window checkpointing (pre-existing, separately scoped-out gap).
- Not building multi-bot cross-visibility for ACP mode's task-status tool — reports only the running persona's own tasks.

## User Requirements / Functional Requirements

**FR-601 (must-have):** A board-read MCP tool, wired at the identical construction site/gate `complete_board_item` uses in both native mode (`team_manager.go`) and ACP mode (`buildACPMCPOptions`). Returns real board-item data (title, status, assignee, ID) from the same `domain.BoardStore` instance the write-side tool uses.

**FR-602 (must-have):** A task-status-read MCP tool exposing the calling bot's own `domain.DirectTask` state via `DirectTaskStore.List(botName)`, wired at the same gate/site as FR-601, in both modes.

**FR-603 (stretch — cut first):** A team-registry-read MCP tool for native mode's multi-bot context. Native-mode-only by construction (ACP has no team-registry concept, ADR-B026).

**FR-604 (must-have, mostly verification):** Audit pass confirming no other existing tool is silently channel/source-conditioned outside documented per-mode constraints. Fix only if a genuine unintentional gap is found.

## Non-Functional Requirements

- **Reliability:** New tools ride on existing `BoardStore`/`DirectTaskStore` instances — no new construction-failure mode; absent exactly when `complete_board_item` is already absent (tech-lead gate).
- **Correctness:** Reads reflect actual on-disk state at call time — reuse `List`/`ListAll` directly, no caching layer.
- **Security:** No secret exposure — board/task fields carry no secrets by construction; no new redaction logic needed.
- **Observability:** New tool activation logged the same way `complete_board_item`'s activation already is.

## System Architecture

- **Affected layers:** `internal/infrastructure/local/mcp/client.go` (new tool definitions/handlers), `cmd/boabot/main.go`/`internal/application/team/team_manager.go` (native wiring, mirrors existing board-store gate), `cmd/boabot/acp.go` (ACP wiring, mirrors existing gate).
- **New/modified components:** New MCP tool handlers only — no new domain interfaces (existing `BoardStore.List`/`DirectTaskStore.List` already cover the read need).

## Scope of Changes

- Files likely to modify: `boabot/internal/infrastructure/local/mcp/client.go`, `boabot/internal/application/team/team_manager.go` or `boabot/cmd/boabot/main.go` (native wiring), `boabot/cmd/boabot/acp.go` (ACP wiring — likely no change needed if the new tools are added inside `buildACPMCPOptions`'s existing `WithBoardStore`-conditioned block).
- Dependencies: `domain.BoardStore.List`, `domain.DirectTaskStore.List`/`ListAll` (both already exist).

## Breaking Changes

None — purely additive tools.

## Success Criteria and Acceptance Criteria

- [ ] An agent asked "what's on the kanban board" via Buzz correctly lists real items instead of a false "no items" claim.
- [ ] The identical question via native web-UI chat gets the same correct answer, using the identical tool.
- [ ] An agent asked "what's scheduled/running" via Buzz correctly reports real `DirectTask` state.
- [ ] FR-601/602's tools are wired at the exact same gate as `complete_board_item` in both modes — verified by reading the code.
- [ ] (If FR-603 ships) A native-mode Buzz-sourced task can list active team members/status.
- [ ] No regression: every existing tool continues to work unchanged.
- [ ] Existing test suites pass unchanged; every new tool has TDD coverage including a genuinely-red-before-the-fix regression test.
- [ ] **Live verification required**: rebuilt binary actually deployed (native process + ACP pool restarted), board/task-status questions manually re-tested over a real Buzz conversation before the demo.

**Quality gates:** `go fmt`, `go vet`, `golangci-lint run`, `go test -race -gcflags=all=-d=checkptr=0 ./...` all clean; domain+application aggregate coverage stays ≥90%.

## Risks and Mitigation

| Item | Type | Notes | Mitigation |
|------|------|-------|------------|
| Demo tomorrow night, limited time | Risk | FR-601/602/604 must-have; FR-603 cuttable. | Prioritize in that order; defer FR-603 explicitly, not silently, if time runs short. |
| Live deployment predates all recent fixes | Risk | Confirmed earlier this session — currently-running binaries are stale. | Acceptance criteria mandate a real rebuild+restart+manual test, not just `go test`. |

## Timeline and Milestones

Single-session implementation, targeting completion well before tomorrow night's demo, with explicit time budget for live verification.

## References

- Source PRD: [channel-agnostic-tool-parity-PRD.md](./channel-agnostic-tool-parity-PRD.md)
- Direct predecessor: `specs/archive/260816-acp-native-shared-state/` (today's earlier session work)
