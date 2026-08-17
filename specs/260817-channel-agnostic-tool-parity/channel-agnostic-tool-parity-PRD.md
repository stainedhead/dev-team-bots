# PRD: Channel-Agnostic Agent Tool Parity (Board/Task/Team Read Access)

**Created:** 2026-08-17
**Jira:** N/A
**Status:** Draft

## Problem Statement

An operator asking an agent "what's on the kanban board" gets a confidently wrong answer ("no items") regardless of which channel they ask from — including native mode's own web-UI chat, where the same question was expected to work. Investigation (this session, `dev-flow-analysis.md`/session history) found the true cause: no tool anywhere in the codebase — native daemon mode or ACP mode, any channel — lets an agent actually *read* board state, task/schedule status, or team registry. `complete_board_item` is the only board-related MCP tool that exists, and it is write-only (marks a specific item done by ID; it cannot list or query anything). Confirmed via exhaustive grep across `internal/infrastructure/local/mcp/client.go` (every tool name), `internal/application/execute_task.go` (no per-source context injection), and `internal/application/orchestrator/board_dispatch.go` (human-initiated dispatch only, not agent-initiated read). This blocks the goal of using Buzz — or any channel — as a full-fidelity remote interface to everything the native app can do: an agent cannot accurately report on state it has no tool to check, so it either declines or, worse, fabricates a plausible-sounding wrong answer.

This is the direct continuation of `specs/archive/260816-acp-native-shared-state/` (today's earlier work, closing conversation-continuity/scheduling/board-*recording* gaps between native and ACP mode) — that work made sure a task's *existence* and *outcome* are visible on the board from any channel; this PRD closes the parallel gap that an agent itself cannot *read* that same board/task state to answer questions about it, from any channel, including the one (native chat) originally assumed to already work.

## Goals

- Any tool available to a native-chat-sourced task is available identically to a task sourced from any channel (Buzz today, any future channel by construction) — no channel gets a narrower tool/capability surface than any other, except where a genuine, already-documented per-mode constraint applies (e.g., ACP mode's single-persona/no-team-registry scope, per ADR-B026).
- An agent can accurately read and report real board-item state and task/schedule status when asked, from any channel — not just write to or complete them.
- Buzz functions as a genuine primary interface: an operator can accomplish through Buzz everything they could through native mode's web-UI chat, including reporting accurately on kanban/task state.

## Non-Goals

- Not building support for channels that don't exist yet (Slack already exists as a `ChannelMonitor` type; anything genuinely new beyond native chat/Buzz/Slack is out of scope) — the deliverable is a channel-agnostic *mechanism*, verified against native chat and Buzz specifically, that any future channel automatically inherits by using the same tool-wiring path.
- Not redesigning the web UI's own human-facing board/task rendering — unaffected, read-only views already work correctly for a human looking at the dashboard directly.
- Not adding any new *write* capability beyond what already exists (`complete_board_item`, task dispatch, `run_shell`, etc.) — this PRD closes the read-side gap only.
- Not tackling budget tracking or context-window checkpointing (confirmed unimplemented in both modes, scoped out of the prior PRD too — remains its own future item).
- Not building multi-bot cross-visibility for ACP mode's task-status tool — ACP mode is single-persona by design (ADR-B026); a task-status tool there reports only the running persona's own tasks, matching `DirectTaskStore.List(botName)`'s existing signature.

## Functional Requirements

**FR-601:** A board-read MCP tool (exact name resolved at research phase, e.g. `list_board_items`) is added to `internal/infrastructure/local/mcp`, wired at the identical construction site and gate `complete_board_item` already uses in both native mode (`team_manager.go`'s persona-type gate) and ACP mode (`buildACPMCPOptions`'s identical gate) — every persona that gets `complete_board_item` also gets this. Returns real board-item data (title, status, assignee, ID at minimum) from the same `domain.BoardStore` instance the write-side tool already uses.

**FR-602:** A task-status-read MCP tool (e.g. `list_my_tasks`) exposing the calling bot's own `domain.DirectTask` state (status, schedule, next-run time where applicable) via `DirectTaskStore.List(botName)`, wired at the same gate/construction site as FR-601, in both native and ACP mode.

**FR-603 (stretch — cut first if time is short before the demo):** A team-registry-read MCP tool for native mode's multi-bot context (which bots are active, their status) — matches the orchestrator persona's own `SOUL.md` claim ("Maintain the team registry: know which bots are active"). Not applicable to ACP mode, which has no team-registry concept (single-persona, no `team.yaml`, per ADR-B026) — this tool is native-mode-only by construction, not a gap in ACP mode.

**FR-604:** An audit pass (verification, not new code by default) confirming no other existing tool (`run_shell`, plugin tools, CLI tools, memory search) is silently channel- or source-conditioned in a way that isn't already an intentional, documented per-mode constraint. Report findings; fix only if a genuine unintentional gap is found (tool wiring today does not appear to branch on `task.Source` anywhere — this FR exists to confirm that claim, not because a specific gap is already known).

## Non-Functional Requirements

- **Reliability:** New tools ride on the same `BoardStore`/`DirectTaskStore` instances already constructed for the write-side tools — no new construction-failure mode. If those stores are nil (the existing tech-lead gate), the new read tools are simply absent too, matching `complete_board_item`'s existing degrade-gracefully behavior.
- **Correctness:** A board/task-status read tool must reflect the actual on-disk state at call time (no caching that could go stale mid-conversation) — reuse the store's existing `List`/`ListAll` methods directly, no new caching layer.
- **Security:** Read tools must not expose secret material or anything beyond what the equivalent web-UI dashboard view already shows a human operator — board items and tasks carry no secrets by construction (existing `domain.WorkItem`/`domain.DirectTask` fields), so no new redaction logic is needed.
- **Observability:** New tool activation is logged the same way `complete_board_item`'s activation already is (`acp mode: board store activated` / native mode's equivalent), so an operator can confirm from startup logs whether a persona has read access.

## Acceptance Criteria

- [ ] An agent asked "what's on the kanban board" via Buzz correctly lists the real items (names/statuses) instead of a false "no items" claim.
- [ ] The identical question asked via native mode's web-UI chat gets the same correct answer, using the identical tool — not a different, unverified mechanism.
- [ ] An agent asked "what's scheduled" or "what's running" via Buzz correctly reports real `DirectTask` state, including any recurring tasks created via this session's earlier scheduling work.
- [ ] FR-601/602's tools are wired at the exact same gate/construction site as `complete_board_item` in both native and ACP mode — verified by reading the code, not inferred from a single test case.
- [ ] (If FR-603 ships) A native-mode Buzz-sourced task can list active team members and their status.
- [ ] No regression: `complete_board_item` and every other existing tool continues to work unchanged.
- [ ] Existing test suites for both modes pass unchanged; every new tool has TDD (red-then-green) coverage, including a genuinely-red-before-the-fix regression test mirroring today's actual failure (agent falsely reports no board items when items exist).
- [ ] **Live verification, not just automated tests** (explicit requirement given tomorrow's demo, and given this session's finding that the currently-deployed binary predates all of today's other fixes): the rebuilt binary is actually deployed (native-mode process and ACP-mode pool restarted) and the board/task-status questions are manually re-tested over a real Buzz conversation before the demo, not assumed correct from `go test` alone.

## Dependencies and Risks

| Item | Type | Notes |
|------|------|-------|
| Demo tomorrow night, limited remaining time | Risk | FR-601/602 are must-have (they directly fix the confirmed, demo-visible bug); FR-603 is explicitly cuttable — if implementation time runs short, ship FR-601/602/604 and defer FR-603 to a follow-up, not silently drop it. |
| `localmcp.Client`'s existing tool-registration pattern (`WithBoardStore`, etc.) | Dependency | New tools reuse the identical registration/gating mechanism `complete_board_item` already uses — no new abstraction invented. |
| `domain.BoardStore.List`/`domain.DirectTaskStore.List`/`ListAll` | Dependency | Both interfaces already have the needed read methods — this is new MCP tool wrappers around existing store reads, not new store capability. |
| Live-deployment verification gap (carried over from today's earlier PRD) | Risk | The currently-running deployed binaries predate today's entire `feat/acp-native-shared-state` merge, let alone this PRD's work. This PRD's own acceptance criteria explicitly require a real rebuild+restart+manual-test cycle before the demo — do not treat `go test` passing as sufficient given the stakes. |

## Open Questions

- **Exact tool names** (`list_board_items` vs `get_board` vs `board_status`, `list_my_tasks` vs `list_direct_tasks`) — resolve concretely at research phase; low-stakes naming choice, don't block on it.
- **FR-602's exact response shape** — minimal (status + title + schedule) vs richer (full instruction/output history) — default to minimal for a chat-readable summary; richer detail can follow if the demo shows it's needed.
- **FR-603's exact scope if it ships** — bot name + status only, vs. richer detail (current task, last-seen time) — default to minimal (name + status), matching FR-602's minimal-first bias.
