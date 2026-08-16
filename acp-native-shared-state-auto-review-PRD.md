# PRD: ACP/Native Shared-State Parity — Code and Design Review Findings

**Created:** 2026-08-16
**Source branch:** `feat/acp-native-shared-state` (vs `main`)
**Source spec:** `specs/260816-acp-native-shared-state/`
**Status:** Draft

## Executive Summary

**Overall assessment: Approve with minor comments.** The implementation covers all five functional requirements (FR-501–505), reuses existing native-mode mechanisms (`ChatStore` history replay, `ChatTaskManager` scheduling, `BuzzTaskBridge`'s board-item pattern) rather than reimplementing them, and correctly identified and redesigned FR-501 mid-implementation when research proved the PRD's original wording structurally unimplementable (documented in `implementation-notes.md` and reflected in the revised `spec.md`). Two genuine pre-existing bugs were found and fixed as blocking dependencies (a `ChatStore`/`DirectTaskStore` cross-process clobber race, and a `TeamManager.cancel` data race) — both are real fixes with regression tests, not scope creep. No Must Fix findings. The two items below are Warning-level, neither blocks merge.

This review followed the standard TDD workflow: findings below were identified by reading the diff against `main...HEAD` and `spec.md`'s acceptance criteria, not by re-running the implementation's own test suite (which already passes — see `implementation-notes.md`).

## Findings

**FR-R1 (P2):** Live-deployment end-to-end verification is not part of this dev-flow run's scope, and should not be assumed complete from a green automated test suite alone.

- **What:** All verification in Step 3 was automated (`go test`, `-race`, coverage, `golangci-lint`). Acceptance criteria AC2/AC3 (a follow-up question via ACP mode reflects prior context; that history is visible/consistent with native mode's chat feed) describe end-to-end, cross-process behavior against a real shared `memory.path` between a running native-mode process and a running ACP-mode process. The repo's own deployed `boabot-team/bots/orchestrator/config.yaml` (outside this worktree) does not yet set an explicit shared `memory.path` opting into this behavior for both modes consistently — this feature's design is additive/backward-compatible (a persona not opting in keeps prior behavior), so nothing breaks, but the acceptance criteria are not yet demonstrated against a live deployment.
- **Why it matters:** A future reader of `status.md` could mistake "Step 3 complete, tests green" for "verified end-to-end in production," which this dev-flow run does not claim.
- **Fix:** No code change needed. `status.md`/`implementation-notes.md` already scope this correctly (automated verification only) — this finding exists to make that scoping explicit in the review record too, and as a reminder for whoever next operates the live deployment to set matching `memory.path` values and manually confirm AC2–AC4 once, per `ACP-Harness-Adoption-Config.md`'s new startup-validation note.
- **Acceptance criteria:** No code change required. Optional: a follow-up note in `implementation-notes.md`'s "Deviations from Plan" explicitly stating live end-to-end verification is deferred to first real deployment, not part of this automated pipeline run.

**FR-R2 (P2):** `sharedstate.EnsureOwner`'s malformed-marker handling silently reclaims a directory rather than treating a read/parse failure distinctly from "unclaimed."

- **File:** `boabot/internal/infrastructure/local/sharedstate/sharedstate.go`, `EnsureOwner` (~line 70)
- **What:** If the marker file exists but fails to unmarshal (or has an empty `Owner` field), `EnsureOwner` falls through to the same code path as "no marker exists yet" and overwrites it with the current process's identity. This mirrors `board.go`'s existing "malformed = empty" tolerance convention for `readDiskItems`, so it's consistent with the codebase's established pattern, not a novel risk — but for a directory *ownership* marker specifically (rather than accumulated data), a torn/corrupted marker (e.g. a crash mid-write, though the write itself is atomic via temp-file+rename) could theoretically let a second identity silently reclaim a directory that was legitimately claimed by a first identity moments earlier, with no warning logged for the malformed-content case (only the identity-mismatch case logs a warning today).
- **Why it matters:** Low likelihood (the marker write is already atomic) and the consequence is only a missed warning, not corrupted shared state — the underlying board/chat/task stores have their own independent concurrency protection regardless of what the marker says. Not a correctness bug in the feature's actual safety property.
- **Fix:** Optional hardening: log at `slog.Warn` when a malformed marker is encountered and overwritten, distinguishing it from the clean "no marker yet" case, so an operator has a trail if this ever happens in practice.
- **Acceptance criteria:**
  - [ ] A malformed/corrupt `.shared-state-owner` file logs a specific warning distinct from "directory already claimed by a different identity" before being overwritten.
  - [ ] Existing `sharedstate` tests continue to pass unchanged.

## Non-Findings Worth Recording

Reviewed and confirmed correct, no action needed:

- `schedulingFailureMessage`'s `errors.Is(err, errImmediateDispatchUnsupported)` unwrap path was traced through `ChatTaskManager.DetectAndHandle` → `LocalTaskDispatcher.DispatchWithSchedule` → `dispatchNow` → `sendMessage` → `NoImmediateDispatchQueue.Send`, confirming the error chain is unwrapped correctly (also covered by `TestAgent_Prompt_SchedulingConfirmation_ImmediateModeDeclinesGracefully`).
- `buildInstructionWithHistory`'s windowing logic correctly assumes `recordChatMessage` (outbound) already ran earlier in the same `Prompt` call before it's invoked, so `history[0]` is the just-appended message — verified by reading call order in `turn.go`'s `Prompt`, matching `BuzzTaskBridge`'s identical `recordInbound`-then-`buildInstructionWithHistory` ordering.
- `DeleteThread`'s in-place message-array filter (`filtered := s.messages[:0]`) combined with the new `deletedIDs` collection in the same loop is safe (write index never exceeds read index) — pre-existing idiom, correctly extended.
- The `TeamManager.cancel` race fix (`cancelMu`/`setCancel`/`callCancel`) has no dedicated new regression test, but is exercised by the pre-existing `TestTeamManager_ShutdownAlreadyCancelledCtx` under `-race` (confirmed: this is exactly the test whose failure surfaced the race in the first place, and it now passes clean across 8 repeated `-race` runs) — a bespoke timing-based regression test would be redundant with what `-race` + the existing concurrent test already provides.
- `startDirectTask`/`finishDirectTask`/`createBoardItemForTask`/`finishBoardItem` all correctly no-op when their required store(s) are nil, verified by `TestAgent_Prompt_NilChatState_BehavesAsBeforeThisFeature`.

## Priorities

| ID | Priority | Blocking? |
|---|---|---|
| FR-R1 | P2 | No |
| FR-R2 | P2 | No |

No P0 (blocker) or P1 (high) findings.

## Guidance for Implementing Fixes (Step 9)

- Both findings are optional hardening, not defects — apply via TDD (failing test first) if implemented, same as every other change in this feature.
- No P0 items exist, so this PRD does not block the branch from proceeding to Step 11 (Final Quality Pass) even without further fixes; implementing FR-R2's logging improvement is recommended (small, low-risk) but not required.
- No git worktree or agent-teammate parallelization is needed for a fix set this small — a single sequential pass covers both findings.
