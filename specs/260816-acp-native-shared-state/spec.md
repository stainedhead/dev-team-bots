# Spec: ACP/Native Shared State and Task-Layer Parity

**Created:** 2026-08-16
**Status:** Draft
**Source PRD:** [acp-native-shared-state-PRD.md](./acp-native-shared-state-PRD.md)

## Executive Summary

Closes four gaps found by a deep audit of native daemon mode vs. ACP mode (`boabot -acp`), triggered by observing that a Buzz conversation via ACP mode has no memory of earlier turns while native mode's web UI does: (1) board/chat state is shared only by coincidence between the two modes, not by design; (2) ACP mode has zero conversation continuity; (3) ACP mode has no scheduling path at all — structurally, not just unwired; (4) ACP mode has no heap watchdog. This is the largest of the four dev-flow features run this session — real new infrastructure, not just wiring fixes.

## Problem Statement

1. **Shared state is coincidental, not designed.** Native mode's shared board path (`<MemoryRoot>/<orchestratorName>/board.json`) and ACP mode's board path (`<memRoot>/<cfg.Bot.Name>/board.json`) are computed by two independent formulas with no shared code, config field, or constant. They agree today only because a persona's `bot.name` happens to match its `team.yaml` entry name, and because native mode's `memory.path` was manually set to match ACP mode's independent fallback. Nothing validates this; a rename or config drift would silently split state with no error.
2. **No conversation continuity in ACP mode.** `internal/infrastructure/acp/session.go`'s `session` struct holds only `cancel context.CancelFunc` — confirmed via direct code read. Every `Prompt` call builds its instruction purely from that call's own content.
3. **No scheduling path in ACP mode — structural.** ACP mode never constructs a `Dispatcher`, `DirectTaskStore`, or `ChatTaskManager`. A recurring-instruction Buzz request via ACP mode has nowhere to go.
4. **No heap watchdog in ACP mode.** Confirmed via grep — zero references to `watchdog`/`HeapWarnMB`/`HeapHardMB` in `acp.go`, despite every persona's config already carrying these fields.

## Goals

- Native mode and ACP mode share board and conversation state through one explicit, validated source of truth.
- A human interacting via Buzz through ACP mode gets the same conversation continuity native-mode web-UI chat already has.
- A recurring/scheduled Buzz request via ACP mode creates a real scheduled task, same as native-mode Buzz.
- Every ACP-dispatched task (not just scheduled ones) creates a real, visible board/task record — explicit product decision, made directly with the repo owner: consistency with native mode, and operational value (watching the board instead of reading raw process logs to tell if a task is stuck — directly informed by a live debugging session earlier this evening).
- ACP-mode processes get the same graceful heap-limit shutdown native-mode bots already have.

## Non-Goals

- Not merging ACP mode's process model with `TeamManager`/native mode's multi-bot orchestration — ACP mode remains single-persona, no-`team.yaml`.
- Not implementing budget tracking or context-window checkpointing — confirmed unimplemented in *both* modes today (`AGENTS.md` describes a `local/budget` package and `context.threshold_tokens` config that don't exist anywhere in the codebase, zero references, field not even parsed). Pre-existing documentation-vs-reality gap, unrelated to ACP/native parity, explicitly out of scope here.
- Not investigating `CardRegistry`/Agent Card distribution — audit found no evidence either mode wires it, inconclusive, deferred.
- Not changing native mode's existing board/chat/dispatch behavior.
- Not implementing mid-task clarifying questions for ACP mode — already correctly scoped out by the prior ACP-parity feature (ADR-B030), unaffected by this work.

## User Requirements / Functional Requirements

**FR-501:** A new, explicit shared-state configuration mechanism (exact shape TBD at research) replaces path-inference for state ACP mode and native mode share. Native mode's top-level config and any ACP persona config intended to share state set this value identically. ACP mode's existing per-persona fallback remains available for standalone personas with no shared-state requirement.

**FR-502:** This mechanism is designed once, applying to both existing `board.json` and new `chat.json` — not board-only.

**FR-503:** ACP mode gains conversation continuity via the same `ChatStore` mechanism native mode uses — inbound/outbound messages appended, each turn's instruction built by replaying recent history (reusing the existing `handleChatSend`/`BuzzTaskBridge` pattern).

**FR-504:** A recurring/scheduled Buzz request via ACP mode creates a real `Schedule`/`RecurrenceRule`-backed task via `DispatchWithSchedule`, reusing `ChatTaskManager`/`ParseScheduleNL`. Exact integration shape (reconciling ACP's synchronous protocol with the async `Dispatcher` layer) — research-phase question, see Open Questions.

**FR-504a:** Every ACP-dispatched task creates a real, `bot_name`-tagged `DirectTask` and Kanban board item, visible live in the dashboard — not just scheduled tasks. (Confirmed explicitly with the repo owner, not a default assumption.)

**FR-505:** ACP mode wires a heap watchdog from each persona's own `heap_warn_mb`/`heap_hard_mb` config fields, matching native mode.

## Non-Functional Requirements

- **Reliability:** FR-501's mechanism fails loudly on inconsistent configuration rather than silently diverging. New store construction failures degrade gracefully (ACP mode still starts, logged clearly).
- **Correctness:** FR-504's scheduling integration must not change ACP mode's existing synchronous immediate-response behavior for non-scheduling requests.
- **Data safety:** Any new shared-state writer (ChatStore, and DirectTaskStore if FR-504 needs one in ACP mode) reuses the existing board-store P0 fix's locking mechanism (`specs/archive/260815-acp-harness-feature-parity-auto-review/`) — not a new concurrency bug for a new file.
- **Observability:** ACP-mode startup logs state clearly whether shared-state mode is active (and what root it resolved to), whether ChatStore/scheduling wiring activated, and whether the heap watchdog is running.

## System Architecture

- **Affected layers:** `cmd/boabot/acp.go` (shared-state config resolution, ChatStore/Dispatcher/watchdog wiring), `internal/infrastructure/acp/turn.go` (history replay, scheduling-intent detection), possibly `internal/infrastructure/config/config.go` (new shared-state config field).
- **New/modified components:** shared-state config resolution (new); ChatStore wiring in ACP mode (new, reuses existing type); scheduling/dispatch layer in ACP mode (new — scope TBD at research, see Open Questions); heap watchdog wiring in ACP mode (new, reuses existing type).
- **Out of scope architecturally:** `TeamManager`/native mode's multi-bot orchestration — untouched.

## Scope of Changes

- Files likely to modify: `boabot/cmd/boabot/acp.go`, `boabot/internal/infrastructure/acp/turn.go`, `boabot/internal/infrastructure/acp/session.go`, `boabot/internal/infrastructure/config/config.go` (if a new field is needed).
- Files likely to create: none expected beyond possibly a small shared-state resolution helper — reuses existing store/dispatcher/watchdog types throughout.
- Dependencies: existing `orchestratorlocal.NewInMemoryChatStore`, `ChatTaskManager`, `ParseScheduleNL`, `DispatchWithSchedule`, `watchdog.New`, the board-store `filelock`-based concurrency fix.

## Breaking Changes

None expected to public config schema for existing fields. FR-501 likely introduces one new optional config field (exact shape at research) — additive, not breaking existing deployments that don't set it (falls back to today's per-persona behavior for personas not opting into shared state).

## Success Criteria and Acceptance Criteria

- [ ] Native mode and an ACP persona configured to share state, when either's config diverges from the other's shared-state value, fail to start (or log a loud, specific warning) rather than silently writing to different paths.
- [ ] A follow-up question via ACP mode gets a response reflecting context from the earlier turn, verified end-to-end.
- [ ] The ChatStore-recorded history is visible/consistent with what native mode's chat feed would show for the equivalent thread, when both share state.
- [ ] A recurring-instruction Buzz request via ACP mode creates a real scheduled task, visible under the Tasks UI's "Scheduled" filter.
- [ ] A non-scheduling Buzz request via ACP mode still gets a direct, synchronous reply with no added latency from scheduling-detection.
- [ ] A normal (non-scheduled) ACP-dispatched task also creates a real, `bot_name`-tagged `DirectTask` and Kanban board item, visible live as it progresses and completes.
- [ ] An ACP-mode process with `heap_warn_mb`/`heap_hard_mb` configured logs a warning at the soft limit and shuts down gracefully at the hard limit.
- [ ] `boabot -acp`'s existing turn-handling, fallback-publish, and keep-alive tests still pass unchanged.
- [ ] Existing native-mode behavior (web-UI chat, native-mode Buzz, the Kanban board) is unchanged.

**Quality gates:** `go fmt`, `go vet`, `golangci-lint run`, `go test -race -gcflags=all=-d=checkptr=0 ./...` all clean; domain+application aggregate coverage ≥90% (currently 92.2%, must not regress).

## Risks and Mitigation

| Item | Type | Notes | Mitigation |
|------|------|-------|------------|
| `board.json`'s P0 concurrency fix | Dependency | New `chat.json` (and DirectTaskStore, if needed) must reuse this locking mechanism. | Reuse `filelock` package directly, don't reinvent. |
| `ChatTaskManager`/`ParseScheduleNL`/`DispatchWithSchedule` | Dependency | Reused for FR-504, not reimplemented. | Same precedent as prior two Buzz features this session. |
| Reconciling ACP's sync protocol with async dispatch (FR-504) | Risk | Core design risk of this PRD. | Research phase resolves concretely before implementation — see Open Questions. |
| Introducing Dispatcher/DirectTaskStore into ACP mode (if FR-504 needs it) | Risk | New infrastructure; must not slow down or complicate the common immediate-request case. | Scope narrowly at research phase; prefer a scheduling-intent pre-check over full async routing if that satisfies the ACs. |
| Shared-state config field naming/shape (FR-501) | Risk (low) | Bikeshed risk more than technical. | Resolve concretely during research. |

## Timeline and Milestones

[TBD] — tracked via `status.md`.

## References

- Source PRD: [acp-native-shared-state-PRD.md](./acp-native-shared-state-PRD.md)
- Prior features (context/precedent): `specs/archive/260815-acp-harness-feature-parity/`, `specs/archive/260815-acp-harness-feature-parity-auto-review/` (P0 board-lock fix, chat_provider/board/plugin/CLI wiring pattern)
