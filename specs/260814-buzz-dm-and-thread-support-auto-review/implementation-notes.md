# Implementation Notes: Buzz DM/Thread Support — Code Review Fixes

**Created:** 2026-08-15

Record technical decisions, edge cases, deviations from the plan, and lessons learned as implementation proceeds. Update this file continuously during Phase 5 — do not leave it until the end.

## Technical Decisions

### FR-301 (P1) — fixed, option (a): pass the real Buzz ThreadID through

`chatMessageThreadID` (`boabot/internal/application/team/team_manager.go`) now returns `task.ThreadID` for `domain.DirectTaskSourceBuzz`, not `""`. This is the same NIP-10 thread-root hex (or `"dm:<pubkey>"`) key that `internal/infrastructure/buzz`'s `dispatchViaBridge`/`handleDMEvent` already pass through `Dispatch` and that `BuzzTaskBridge` stores verbatim on the resulting `DirectTask` — it was never actually "the Nostr channel UUID" the old doc comment claimed (that claim predated P1.1's fix, which changed the passed value from `channelUUID` to `root`).

To eliminate the resulting duplicate write (not just make the surviving write correctly threaded), `Monitor.recordOutbound` (`boabot/internal/infrastructure/buzz/monitor.go`) was moved off the task-completion path entirely:
- `publishReply`/`publishDMReply` no longer call `recordOutbound` themselves.
- `HandleResult` (task completion) no longer causes a `recordOutbound` write at all — `TeamManager.handleSharedTaskResult` (the extracted, now-directly-testable form of the old inline `WithTaskResultHandler` closure) is the single writer for a Buzz task's completion message, correctly threaded.
- `recordOutbound` is still called, but only from the two call sites that have no `DirectTask`/`TaskResultPayload` at all and so are never seen by `handleSharedTaskResult`: `dispatchViaBridge`'s and `handleDMEvent`'s immediate-`Reply` branches (a `ChatTaskManager` scheduling-confirmation prompt or ack). Each now checks `publishReply`/`publishDMReply`'s returned error and calls `recordOutbound` only on success, preserving that path's pre-fix behaviour exactly.

**Failure-mode regression, addressed as flagged:** `handleSharedTaskResult` runs synchronously ahead of `forwardResultToMonitors` (which is what eventually reaches a channel monitor's own relay-publish attempt), so the shared chat record of a Buzz task's output is written before a relay-publish failure could ever occur, exactly preserving the pre-fix guarantee that a publish failure still leaves a record of the bot's output.

**Testability refactor:** the `WithTaskResultHandler` closure inline in `startBot` was extracted into `(tm *TeamManager) handleSharedTaskResult(ctx, p, monitors)` (pure extraction, no behaviour change) so FR-301's acceptance criterion — "a new test drives a Buzz-dispatched task ... and asserts `sharedChatStore` contains exactly one message" — could be verified directly against production code (`internal/application/team/internals_test.go`'s `TestHandleSharedTaskResult_BuzzTask_RecordsExactlyOneMessage`) rather than only indirectly through a full `RunAgentUseCase`/bus integration harness. A second regression test on the buzz-package side (`TestMonitor_HandleResult_DoesNotRecordChatOutbound`) and a third confirming the immediate-reply path still records once (`TestMonitor_Dispatch_WithTaskDispatcher_ReplyOnly_RecordsChatOnce`) round out coverage of both halves of the fix.

### FR-302 (P2) — documented as accepted, no code change

`BuzzTaskBridge.dispatchedThreads` (`boabot/internal/application/orchestrator/buzz_task_bridge.go`) is left without a TTL/eviction policy. Each entry is two short strings and a `time.Time`; growth is one entry per distinct (persona, thread-root-or-DM-counterparty) pair for the lifetime of the process. At realistic Buzz @mention/DM volume for a small number of personas this stays small (this matches the review's own framing: "bounded in practice once `respond_to`/`respond_to_allowlist` is configured"). Reviewed and accepted as-is rather than adding an eviction mechanism whose main effect would be complicating `KnownThread`'s straightforward semantics for a memory bound that isn't currently a demonstrated problem. Revisit if a persona's distinct-thread volume in a long-running daemon deployment is ever observed to be a real memory concern.

### FR-303 (P2) — documented as accepted, no code change

`handleDMEvent` (`boabot/internal/infrastructure/buzz/dm.go`) runs gift-unwrap (NIP-44 decrypt + Schnorr signature verification) before the author gate check, unlike the channel path's cheap `maxContentLen`-before-gate ordering. This is inherent to NIP-17 addressing: the sender's pubkey is not known until after the seal is decrypted and verified, so the gate genuinely cannot run first. Accepted as-is per the review's own conclusion ("no demonstrated exploit ... inherent to how gift-wrap addressing works"); a raw-event-size pre-filter before `GiftUnwrap` was considered but rejected as not clearly a "cheap, contained" win worth the added surface, since Nostr relays already gate event size independently and the per-event async work is already bounded per-persona.

### FR-304 (P2) — fixed: startup warning for fail-open DM gate

Added a `Warn`-level log line at the top of `startDMSubscription` (`boabot/internal/infrastructure/buzz/dm.go`), before the `Subscribe` call, firing when `!m.gate.active()` — mirroring `Monitor.Start`'s existing `LockDir`-empty warning's style exactly (same `"buzz monitor: ..."` prefix, names the inactive protection explicitly, attaches `agent_pubkey`). Placed in `startDMSubscription` rather than `run()` so it is directly testable via the same call the existing DM-subscription tests already drive (`dm_test.go`'s `TestMonitor_StartDMSubscription_*`), with no relay/discovery mocking required. TDD: `TestMonitor_StartDMSubscription_GateInactive_WarnsFailOpen` (red before the fix, asserts `"INACTIVE"` and `"respond_to"` both appear) and `TestMonitor_StartDMSubscription_GateActive_NoWarning` (asserts silence when `RespondTo` is configured).

### FR-305 (P2) — part (a) fixed (docs), part (b) documented as accepted

(a) `specs/archive/260814-buzz-dm-and-thread-support/spec.md`'s FR-207 wording is tightened to scope the three-tag NIP-10 requirement (root `e`, reply `e`, `p`) to channel replies only, cross-referencing the DM line's separate, no-NIP-10-tags acceptance criterion.

(b) `publishReply` (`boabot/internal/infrastructure/buzz/monitor.go`) still emits exactly one `p` tag (the immediate parent's author), not the full NIP-10 multi-hop convention of carrying forward every prior participant's `p` tag. Accepted as-is: boabot's actual usage is a bot replying within a single-human NIP-29 channel thread, where a multi-hop `p`-tag accumulation gap has no observable effect in practice. Extending `publishReply` to walk the parent event's own `p` tags was considered; not implemented, since it would require carrying the parent event's full tag list through `dispatchViaBridge`/`awaitResult`'s call chain (currently only `authorPubKey`, a single string, is threaded through) for a deployment shape where it has no realistic observable benefit.

### FR-306 (P2) — documented (docs-only, no code change)

One sentence added to `specs/archive/260814-buzz-dm-and-thread-support/implementation-notes.md`'s Technical Decisions stating that `dispatchedThreads`/`ChatStore` state is intentionally not rolled back on a failed dispatch attempt, distinguishing it from the `eventID` dedup rollback (which IS rolled back via `unmarkEvent`).

## Edge Cases & Solutions

- FR-301: the immediate-bridge-reply call sites (`dispatchViaBridge`, `handleDMEvent`) can fire `recordOutbound` for content that never becomes a `DirectTask` at all (a cancellation ack) or fire it in addition to `awaitResult`/`awaitDMResult` registering a pending entry for a later task completion (a scheduling confirmation immediately followed, in a later turn, by a real dispatch). Both are legitimate, distinct messages, not duplicates of each other or of a task-completion write.

## Deviations from Plan

- FR-301: architecture.md called out "Final call deferred to implementation after confirming the failure-mode handling the review flagged" for option (a) vs (b). Option (a) was implemented, plus a testability-only refactor (extracting `handleSharedTaskResult` from `startBot`'s inline closure) that architecture.md did not anticipate but that was needed to write a real production-code test for the acceptance criterion.
- FR-302, FR-303: both resolved by documenting as accepted, matching the plan's stated default ("Default toward documenting as accepted ... unless implementation reveals a concrete problem"). Neither did.
- FR-305(b): resolved by documenting as accepted rather than extending `publishReply`, per the same stated default and the reasoning above (would require threading a full tag list through several call sites for no realistic observable benefit in this deployment shape).

## Lessons Learned

- FR-301: `recordOutbound` had two distinct classes of caller (task-completion replies driven by `HandleResult`, and immediate bridge-produced replies with no `DirectTask` at all) that happened to share one call site inside `publishReply`/`publishDMReply`. Removing `recordOutbound` from those methods entirely (rather than just fixing `chatMessageThreadID`) would have silently broken FR-206 history replay for confirmation prompts/acks, since the generic per-bot handler never sees that class of reply. Caught before implementation by tracing every caller of `publishReply`/`publishDMReply`, not just the one HandleResult exercised in the finding's own description.
- FR-304: placing the warning at the top of `startDMSubscription` (rather than in `run()`, where the finding's own file/function pointer initially suggested looking) kept the test directly callable against the existing `dm_test.go` helpers with no additional mocking, and matches exactly where `Monitor.Start`'s analogous `LockDir` warning already lives relative to its own guarded action.
