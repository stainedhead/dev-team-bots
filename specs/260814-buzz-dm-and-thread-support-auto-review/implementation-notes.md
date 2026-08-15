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

## Edge Cases & Solutions

- FR-301: the immediate-bridge-reply call sites (`dispatchViaBridge`, `handleDMEvent`) can fire `recordOutbound` for content that never becomes a `DirectTask` at all (a cancellation ack) or fire it in addition to `awaitResult`/`awaitDMResult` registering a pending entry for a later task completion (a scheduling confirmation immediately followed, in a later turn, by a real dispatch). Both are legitimate, distinct messages, not duplicates of each other or of a task-completion write.

## Deviations from Plan

- FR-301: architecture.md called out "Final call deferred to implementation after confirming the failure-mode handling the review flagged" for option (a) vs (b). Option (a) was implemented, plus a testability-only refactor (extracting `handleSharedTaskResult` from `startBot`'s inline closure) that architecture.md did not anticipate but that was needed to write a real production-code test for the acceptance criterion.

## Lessons Learned

- FR-301: `recordOutbound` had two distinct classes of caller (task-completion replies driven by `HandleResult`, and immediate bridge-produced replies with no `DirectTask` at all) that happened to share one call site inside `publishReply`/`publishDMReply`. Removing `recordOutbound` from those methods entirely (rather than just fixing `chatMessageThreadID`) would have silently broken FR-206 history replay for confirmation prompts/acks, since the generic per-bot handler never sees that class of reply. Caught before implementation by tracing every caller of `publishReply`/`publishDMReply`, not just the one HandleResult exercised in the finding's own description.
