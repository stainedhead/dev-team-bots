# PRD: ACP/Native Shared State and Task-Layer Parity

**Created:** 2026-08-16
**Jira:** N/A
**Status:** Draft

## Problem Statement

A deep audit (triggered by observing that a Buzz conversation via ACP mode has no memory of earlier turns, while the native-mode web UI does) found that native daemon mode and ACP mode (`boabot -acp`) diverge in ways beyond the tool/provider gaps already closed this session (`specs/archive/260815-acp-harness-feature-parity/`):

1. **Shared state is coincidental, not designed.** Native mode's shared board (`board.json`) path is computed as `<MemoryRoot>/<orchestratorName>/board.json`, where `orchestratorName` comes from `team.yaml`'s `orchestrator: true` entry. ACP mode's board path is computed independently as `<memRoot>/<cfg.Bot.Name>/board.json`, where `memRoot` falls back to the running binary's own directory and `cfg.Bot.Name` comes from that persona's own `config.yaml`. These two formulas share no code, config field, or constant — they only produce the same path today because a persona's own `bot.name` happens to match its `team.yaml` entry name, and because the native-mode top-level `config.yaml`'s `memory.path` was manually set to match ACP mode's independent fallback. Nothing validates this at startup; a renamed persona, a moved binary, or an unsynced `memory.path` edit would silently split the two processes onto different state directories with no error.
2. **ACP mode has no conversation continuity.** `internal/infrastructure/acp/session.go`'s `session` struct holds only a `cancel context.CancelFunc` — zero message history. Every `Prompt` call builds its task instruction purely from that call's own content (`extractText(params.Prompt)`). Native mode's web-UI chat and native-mode Buzz both explicitly maintain and replay from a `ChatStore`; ACP mode has no equivalent.
3. **ACP mode has no scheduling path at all — structural, not a missing option.** ACP mode never constructs a `Dispatcher`, `DirectTaskStore`, or `ChatTaskManager`. Its `Prompt→Task→worker.Execute` path is fully synchronous with no task-persistence layer underneath it. A Buzz request phrased as "remind me every day at 9am," which native-mode Buzz correctly turns into a real scheduled task via `DispatchWithSchedule`, has nowhere to go when it arrives through ACP mode — confirmed via code audit, not inferred.
4. **ACP mode has no heap watchdog.** Native mode gracefully shuts down at a configured heap limit (`heap_warn_mb`/`heap_hard_mb`, already present in every persona's `config.yaml`); ACP mode reads neither field and will hard-crash instead.

## Goals

- Native mode and ACP mode share board and conversation state through one explicit, validated source of truth — not two independently-computed paths that happen to agree.
- A human interacting with a persona via Buzz (through ACP mode) gets the same conversation continuity a native-mode web-UI chat user already gets.
- A Buzz request phrased as a recurring/scheduled instruction, received via ACP mode, creates a real scheduled task the same way native-mode Buzz already does.
- ACP-mode processes get the same graceful heap-limit shutdown behavior native-mode bots already have.

## Non-Goals

- Not merging ACP mode's process model with `TeamManager`/native mode's multi-bot orchestration — ACP mode remains single-persona, no-`team.yaml`, per its own established design (confirmed correct and preserved by the prior ACP-parity feature's Non-Goals).
- Not implementing budget tracking or context-window checkpointing as part of this work. The audit found these are unimplemented in *both* modes today — `AGENTS.md` describes a `local/budget` package and `context.threshold_tokens` config that don't exist anywhere in the codebase (confirmed via repo-wide grep, zero references, the config field isn't even parsed). This is a pre-existing documentation-vs-reality gap unrelated to ACP/native parity — worth its own future PRD, explicitly out of scope here.
- Not investigating or fixing `CardRegistry`/Agent Card distribution — the audit found no evidence this is wired in either mode and didn't reach a confident conclusion on whether it's a real gap; deferred to a future audit if it turns out to matter.
- Not changing native mode's existing board/chat/dispatch behavior — this work makes ACP mode conform to native mode's existing model, not the reverse.
- Not implementing mid-task clarifying questions for ACP mode — already correctly scoped out by the prior ACP-parity feature (ADR-B030), blocked on unstable upstream ACP protocol support outside this repo's control. Unaffected by this PRD.

## Functional Requirements

**FR-501:** A new, explicit shared-state configuration mechanism (exact field name/shape TBD at research phase, e.g. `shared_state.root`) replaces path-inference for any state ACP mode and native mode are meant to share. Native mode's top-level `config.yaml` and any ACP-mode persona `config.yaml` intended to share state with native mode's team both set this value identically. ACP mode's existing `cfg.Bot.Name`-based path computation and exe-dir fallback remain available for personas that genuinely run standalone (no shared-state requirement), but the *shared* case is explicit and validated, not inferred.

**FR-502:** This shared-state mechanism is designed once and applies to both the existing `board.json` (already shared, currently coincidentally) and the new `chat.json` (FR-503) — not board-only.

**FR-503:** ACP mode gains conversation continuity via the same `ChatStore` mechanism native mode already uses: inbound and outbound messages are appended to the shared `ChatStore`, and each new turn's instruction is built by replaying recent history for that conversation (mirroring the existing `handleChatSend`/`BuzzTaskBridge` history-replay pattern — reused, not reimplemented).

**FR-504:** A Buzz request received via ACP mode that's phrased as a recurring/scheduled instruction creates a real `Schedule`/`RecurrenceRule`-backed task via `DispatchWithSchedule`, reusing the existing `ChatTaskManager`/`ParseScheduleNL` heuristic parser and confirm/cancel flow — not a new NL-scheduling mechanism. Exact integration shape (how ACP mode's synchronous `Prompt`/`PromptResponse` protocol reconciles with the existing async `Dispatcher`/`DirectTaskStore` layer) is a research-phase question — see Open Questions.

**FR-504a:** Every ACP-dispatched task — not just scheduled ones — creates a real, `bot_name`-tagged `DirectTask` and Kanban board item, visible live in the native-mode orchestrator dashboard, matching native-mode Buzz's existing automatic behavior (explicit decision: consistency with native mode, and direct operational value — an operator can watch the board to see whether an ACP-mode task is progressing or stuck, rather than needing to read raw process logs).

**FR-505:** ACP mode wires a heap watchdog from each persona's own `heap_warn_mb`/`heap_hard_mb` config fields, matching native mode's existing graceful-shutdown behavior.

## Non-Functional Requirements

- **Reliability:** FR-501's shared-state mechanism must fail loudly (clear startup error) if configured inconsistently between native mode and an ACP persona meant to share state with it — not silently diverge as today's coincidental paths would. A construction failure in any newly-wired store (ChatStore, Dispatcher, DirectTaskStore) must degrade gracefully — ACP mode still starts and executes tasks, logged clearly — mirroring the existing pattern for board/plugin store failures.
- **Correctness:** FR-504's scheduling integration must not change ACP mode's existing synchronous immediate-response behavior for non-scheduling requests — a normal question/task must still get a direct, timely reply.
- **Data safety:** Any new shared-state writer (ChatStore, and Dispatcher/DirectTaskStore if FR-504 requires them in ACP mode) must be safe under the same cross-process concurrency conditions the board-store P0 fix (`specs/archive/260815-acp-harness-feature-parity-auto-review/`) already addressed for `board.json` — reuse that fix's locking mechanism, don't reintroduce the same class of bug for a new shared file.
- **Observability:** ACP-mode startup logs state clearly whether shared-state mode is active (and what root it resolved to), whether ChatStore/scheduling wiring activated, and whether the heap watchdog is running — mirroring the existing pattern for board/plugin/CLI-tool activation logging from the prior ACP-parity feature.

## Acceptance Criteria

- [ ] Native mode and an ACP-mode persona configured to share state, when either's config diverges from the other's shared-state value, fail to start (or the ACP process logs a loud, specific warning) rather than silently writing to different paths.
- [ ] A conversation via ACP mode where a human asks a follow-up question gets a response that reflects context from the earlier turn, verified end-to-end (not just "the history was fetched").
- [ ] The same conversation's ChatStore-recorded history is visible/consistent with what native mode's chat feed would show for the equivalent thread, when both point at the same shared-state root.
- [ ] A Buzz request phrased as a recurring instruction, received via ACP mode, creates a real `Schedule`/`RecurrenceRule`-backed task, visible under the Tasks UI's "Scheduled" filter (native mode's dashboard, since ACP mode has none of its own).
- [ ] A normal (non-scheduled) ACP-dispatched task also creates a real, `bot_name`-tagged `DirectTask` and Kanban board item, visible live in the dashboard as it progresses and completes — not just scheduled tasks.
- [ ] A non-scheduling Buzz request via ACP mode still gets a direct, synchronous reply with no new added latency from the scheduling-detection step.
- [ ] An ACP-mode process started with `heap_warn_mb`/`heap_hard_mb` configured logs a warning at the soft limit and shuts down gracefully at the hard limit, matching native mode's existing behavior (verifiable via the same test pattern native mode's watchdog tests already use).
- [ ] `boabot -acp`'s existing turn-handling, fallback-publish, and keep-alive tests still pass unchanged.
- [ ] Existing native-mode behavior (web-UI chat, native-mode Buzz, the Kanban board) is unchanged.

## Dependencies and Risks

| Item | Type | Notes |
|------|------|-------|
| `board.json`'s P0 concurrency fix (`filelock` package, `specs/archive/260815-acp-harness-feature-parity-auto-review/`) | Dependency | The new `chat.json` (and `DirectTaskStore`, if FR-504 needs one in ACP mode) must reuse this existing locking mechanism, not reinvent it. |
| `ChatTaskManager`/`ParseScheduleNL`/`DispatchWithSchedule` (existing, `internal/application/orchestrator`) | Dependency | Reused for FR-504, not reimplemented — same precedent as the original multi-agent Buzz feature and the DM/thread-support feature. |
| Reconciling ACP's synchronous protocol with async dispatch (FR-504) | Risk | The core design risk of this PRD. `Prompt` currently returns synchronously once `worker.Execute` completes; native-mode Buzz's scheduling path is async (dispatch, then a scheduler runs it later). A scheduling *confirmation* can likely still be synchronous (matching `ChatTaskManager.DetectAndHandle`'s existing confirm/cancel flow, which already returns a synchronous acknowledgment without waiting for the scheduled run itself) — but this needs concrete research, not assumption, before implementation. See Open Questions. |
| Introducing `Dispatcher`/`DirectTaskStore` into ACP mode's process (if FR-504 requires it) | Risk | This is new infrastructure for ACP mode, similar in size to the board/plugin store work already done, but for a different subsystem — scope carefully so immediate (non-scheduled) requests aren't accidentally slowed down or made to depend on infrastructure they don't need. |
| Shared-state config field naming/shape (FR-501) | Risk (low) | Bikeshed risk more than technical risk — resolve concretely during research, don't block on it. |

## Open Questions

- **FR-504's exact integration shape** — the central open question. Does ACP mode need a *full* `Dispatcher`/`DirectTaskStore` layer (like native mode), or can scheduling detection be added as a narrower pre-check in `turn.go`'s `Prompt` handler (detect scheduling intent via `ChatTaskManager.DetectAndHandle` first; if it's a scheduling confirmation, dispatch via `DispatchWithSchedule` and return the confirmation synchronously; otherwise fall through to the existing direct `worker.Execute` path for immediate requests, optionally also recording a `DirectTask`/board item *after* execution completes for observability parity)? The narrower approach avoids introducing async-dispatch/synchronous-protocol reconciliation complexity for the common (non-scheduling) case. Research phase should evaluate both and recommend concretely, the way FR-402/RQ1-RQ4 were resolved for the prior ACP-parity feature.
- **FR-501's exact config field name/shape and validation mechanism** — deferred to research, per the Risks table.
