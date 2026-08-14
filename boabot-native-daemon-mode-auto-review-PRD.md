# Code Review PRD: Boabot Native Daemon Mode (Multi-Agent Buzz Support)

**Reviewed branch:** `worktree-boabot-native-multi-agent-buzz`
**Reviewed against:** `origin/main` @ `904a732` (Merge PR #34) — note: the local `main` ref in this worktree is stale (`e1aa9a0`); using it as the diff base incorrectly pulls in three already-released, unrelated commits (the ACP fallback-publish fix, the `-agent`/`-bots-dir` flags, and boabot 0.4.0/0.4.1 CHANGELOG entries). This review uses the correct base, `904a732`, matching the feature's actual commit range `162f28a`..`800958e`.
**Reviewer:** automated code review (dev-flow Step 5)
**Date:** 2026-08-14

## Executive Summary

This is a well-executed feature. The implementation is honest about its own trade-offs — `implementation-notes.md` documents real deviations from the original architecture sketch with concrete reasoning, and every claim checked during this review (the pre-existing `-race` crash in `internal/infrastructure/buzz`, the `internal/application/team` infra-import precedent, the `router.Register` double-registration fix, the DM non-goal boundary) held up under independent verification: `go build`, `go vet`, `golangci-lint run`, and `gofmt -l` are all clean; `go test ./...` is fully green; `go test -race ./...` fails in exactly one package (`internal/infrastructure/buzz`), and that failure was independently reproduced on base commit `262aba0` in an isolated worktree, confirming it is pre-existing and unrelated to this feature; the new concurrency test exercises real `sync.RWMutex`-protected stores under `-race` and passes cleanly. The `router.Register` panic fix is correct, well-isolated, and covered by dedicated regression tests. Coverage numbers cited in `README.md` were independently recomputed and match exactly.

That said, this is not a rubber stamp. Two P1 issues undercut stated guarantees: the relay-replay dedup mechanism in `BuzzTaskBridge` marks a Nostr event ID as "seen" *before* confirming the dispatch actually succeeded, meaning a transient dispatch failure followed by a relay reconnect/replay will cause that instruction to be silently and permanently dropped — misreported as a harmless duplicate rather than retried — and no test exercises this path. Separately, the exact edge case spec.md calls out by name ("Bot has `buzz.enabled: true` but incomplete config... fails in isolation") is untested at the one place it's actually wired into production (`TeamManager.Run()`'s builder loop) — confirmed via a 0%-covered branch, not an assumption. A third finding flags an undocumented, in-scope-adjacent production behavior change: fixing the `task.Source` dead-code bug makes `chat_provider` apply retroactively to every existing chat-sourced deployment, not just the new Buzz path, and this isn't called out anywhere a release-notes reader would see it. The remaining findings are minor (a byte-vs-rune truncation bug in board-item titles, on a fully untested branch; a small amount of stale/dead exported API; a pre-existing coverage gap in `internal/application/team` that this feature adds to without closing).

## Findings

### FR-101 (P1): Relay-replay dedup marks an event "seen" before its dispatch succeeds, permanently losing instructions on transient failure

**Location:** `boabot/internal/application/orchestrator/buzz_task_bridge.go`, `Dispatch()` (calls `markSeenIfDuplicate` at the top of the method, before `chatMgr.DetectAndHandle` or `dispatcher.DispatchWithSchedule` is even attempted) and `markSeenIfDuplicate()` itself.

**Problem:** `markSeenIfDuplicate(eventID)` records the event ID as seen unconditionally, on the very first call, regardless of whether the subsequent dispatch attempt (via `ChatTaskManager.DetectAndHandle` or `dispatcher.DispatchWithSchedule`) succeeds or fails. If that attempt returns an error — a transient failure in the local task store, a context cancellation, anything — `Dispatch` returns the error and `Monitor.dispatchViaBridge` logs it (`"buzz monitor: task dispatcher bridge failed"`) and gives up; there is no retry. Per spec.md's own "Relay reconnect / message replay" edge case, a relay reconnect is expected to redeliver recently-seen events. When that redelivery happens for this event ID, `markSeenIfDuplicate` will report it as `Duplicate: true` — because it really was marked seen — and `Monitor` will log `"buzz monitor: duplicate event, skipping re-dispatch"` at `Info` level and drop it. The user's original instruction is now permanently lost, silently, with a log trail that actively misdescribes what happened (it looks like a harmless duplicate-skip, not a lost task). This directly undermines the FR-005/FR-006 guarantee that a qualifying Buzz mention reliably becomes a visible task.

**Evidence:** `TestBuzzTaskBridge_Dispatch_DispatcherError_Propagates` (`buzz_task_bridge_test.go`) confirms the error-propagation half of this but never issues a second `Dispatch` call with the same event ID afterward to check whether the failed attempt is later treated as a duplicate. No test in the suite exercises "dispatch fails, then the same event ID is redelivered."

**Acceptance criterion:** Dedup state is only recorded once the underlying dispatch (or `ChatTaskManager.DetectAndHandle` handling) has actually succeeded — e.g. mark-seen moves to after a successful `DispatchWithSchedule`/handled-with-no-error return, or a failed attempt explicitly un-marks the event ID before returning the error. A new regression test dispatches an event ID that fails, then redispatches the identical event ID and asserts it is *not* reported as `Duplicate` and is retried.

---

### FR-102 (P1): The named edge case "Buzz-enabled persona with incomplete config fails in isolation" is untested at its actual production integration point

**Location:** `boabot/internal/application/team/team_manager.go`, `Run()`'s Buzz-monitor-builder loop, specifically the `if mon == nil { continue }` branch (currently uncovered — confirmed via `go tool cover -func`, not asserted).

**Problem:** spec.md's Edge Cases section names this scenario explicitly: *"Bot has `buzz.enabled: true` but incomplete config (missing relay URL, missing/unresolvable `buzz_private_key`): that persona's monitor construction fails in isolation — logged, does not prevent other personas' monitors or the orchestrator UI from starting."* The actual production code path for this is: `TeamManager.Run()` calls `tm.buzzMonitorBuilder(...)`, which in production is `main.go`'s closure around `buildBuzzMonitor` — and if `buildBuzzMonitor` returns `nil` (its documented behavior for a bad/missing key), `Run()`'s loop must `continue` without disturbing any other persona. This exact branch has 0% test coverage. The three new `TestTeamManager_BuzzMonitorBuilder_*` tests all supply a builder mock that unconditionally returns `&mocks.ChannelMonitor{}` — none of them return `nil` to simulate a builder failure. The isolation behavior *is* tested at a lower layer (`main_test.go`'s pre-existing `TestBuildBuzzMonitor_KeypairLoadFailure` etc., confirming `buildBuzzMonitor` itself returns `nil`), but the specific integration point spec.md calls out by name — "does not prevent other personas' monitors... from starting" in the actual `Run()` loop — has never been exercised by a test.

**Acceptance criterion:** Add a test where `WithBuzzMonitorBuilder`'s mock returns `nil` for one Buzz-enabled persona and a real `&mocks.ChannelMonitor{}` for another, asserting the `nil`-returning persona is skipped without affecting the other persona's monitor or `Run()`'s overall success. `go tool cover -func` should show the `mon == nil` branch at 100%.

---

### FR-103 (P1): `chat_provider` silently becomes live for existing chat-sourced deployments, and this isn't called out as a behavior change anywhere a release-notes reader would see it

**Location:** `boabot/internal/application/execute_task.go` (`isConversationalSource`), `boabot/internal/domain/message.go` (`TaskPayload.Source`), `boabot/internal/infrastructure/local/orchestrator/task_dispatcher.go` (`sendMessage`), `boabot/internal/application/run_agent.go` (`handleTask`).

**Problem:** `implementation-notes.md` (finding #4 under "Additional findings") documents, honestly and in detail, that `execute_task.go:100`'s `task.Source == "chat"` check has been dead code since it was written — nothing ever set `Message.From == "chat"`, so no chat-sourced task has ever actually used a configured `models.chat_provider`. Fixing this (via the new `TaskPayload.Source` field) is necessary to make the Buzz-equivalent check reachable, per architecture.md's decision. But the fix's blast radius is not scoped to Buzz: it also activates `chat_provider` for the *existing*, *already-shipped* web-UI/Slack chat path, for every current operator who has `models.chat_provider` configured and has been unknowingly getting `models.default` instead. This is a real production behavior change — potentially a different model, different cost profile, different latency — for deployments that have nothing to do with this feature. It is documented in `docs/architectural-decision-record.md` (ADR-B028, item 4) and in `user-docs/Claude-Adoption-Config.md`/`OpenAI-Adoption-Config.md`, and is mentioned in the `d771f75` commit body — but spec.md's "Breaking Changes" section (which explicitly discusses config-schema compatibility) says nothing about it, and there is no user-facing changelog-visible callout an operator upgrading `boabot` would actually encounter before their chat behavior silently changes.

**Acceptance criterion:** spec.md's Breaking Changes section (or an equivalent, prominent, changelog-visible note) explicitly states: "operators with `models.chat_provider` configured will see it apply to chat-sourced tasks for the first time after this upgrade — previously inert." This is a documentation-only fix; no code change required.

---

### FR-104 (P2): `buzzBoardTitle`'s truncation slices by byte index, not rune index, and the branch is completely untested

**Location:** `boabot/internal/application/orchestrator/buzz_task_bridge.go`, `buzzBoardTitle()`.

**Problem:** `title[:boardItemTitleMaxLen]` slices the Go string by byte offset. For an instruction whose first 80 bytes end in the middle of a multi-byte UTF-8 rune (any non-ASCII text — accented characters, CJK, emoji, all plausible in a chat-originated Buzz message), this produces an invalid/corrupted UTF-8 tail on the board item's title before the appended `…`. `go tool cover -func` confirms this branch (`len(title) > boardItemTitleMaxLen`) has 0% coverage — no existing test exercises an instruction longer than 80 characters, ASCII or otherwise.

**Acceptance criterion:** Truncate on rune boundaries (e.g. `[]rune(title)` slicing, or `utf8.RuneCountInString` + a rune-safe truncate helper). Add a test with a >80-character instruction containing multi-byte runes (e.g. emoji or accented text) straddling the truncation point, asserting the result is valid UTF-8 and does not end mid-rune.

---

### FR-105 (P2): `LoadTeamConfig` was exported for a purpose it no longer serves

**Location:** `boabot/internal/application/team/team_manager.go` (`loadTeamConfig` → `LoadTeamConfig`).

**Problem:** architecture.md's stated rationale for exporting this function is "`main.go` doesn't duplicate YAML parsing" — implying `main.go` would call it directly. Per implementation-notes.md's own documented deviation #3, the design changed: the whole per-persona loop moved inside `TeamManager.Run()` instead, and `main.go` never ends up calling `LoadTeamConfig` at all (confirmed by grep — its only callers are `team_manager.go` itself and `internals_test.go`). The export is now unnecessary public API relative to the reason given for it; it should either be un-exported again or the ADR/architecture doc corrected to state the real reason (if any) it needs to stay exported.

**Acceptance criterion:** Either revert `LoadTeamConfig` to unexported `loadTeamConfig` (and its test back to package-internal), or update `architecture.md`/ADR-B028 to reflect why it's actually exported. Pick one; either is a small, low-risk cleanup.

---

### FR-106 (P2): `internal/application/team` remains well below AGENTS.md's 90% coverage target, and this feature adds to it without closing the gap

**Location:** `boabot/internal/application/team/team_manager.go`.

**Problem:** AGENTS.md requires "Coverage target: 90% or above on Domain and Application layers." `internal/application/team` measured at 78.9% (independently recomputed via `go test -coverprofile` — matches `README.md`'s cited number exactly), up marginally from 77.8% pre-feature. This is not a regression — AGENTS.md's actual hard rule is "do not reduce coverage when adding code," which is satisfied — but this feature adds ~143 net lines to the single largest, least-covered application-layer file in the codebase (`Run()` itself is at 84.9%, `startBot` at 59.7%) without materially closing the gap to the stated 90% target. This is a pre-existing condition this PR is not obligated to fix, but it's worth tracking separately so it doesn't keep silently absorbing new logic at a below-target coverage level.

**Acceptance criterion:** No fix required for this PR to merge. File a separate, non-blocking follow-up to raise `internal/application/team` coverage (particularly `startBot`, which is both the largest uncovered surface and pre-existing) toward the 90% target.

## Verified Claims (no finding — confirmed accurate)

For transparency, the following claims from `implementation-notes.md` were independently checked, not taken on faith, and confirmed true:

- **`router.Register` double-registration fix**: correct and covered by dedicated regression tests (`TestTeamManager_Run_PreRegisteredBotName_NoDuplicatePanic`, `TestRouter_Lookup_Registered`, `TestRouter_Lookup_Unregistered`, `TestTeamManager_BuzzMonitorBuilder_DuplicateBotName_IsolatedNotPanic`). `Router.Lookup` is correctly `RLock`-protected and race-safe.
- **`TaskPayload.Source` reachability fix**: confirmed correct via `TestRunAgent_Poll_TaskMessage_PayloadSourcePreferredOverMessageFrom` and `TestLocalTaskDispatcher_Dispatch_MessagePayloadCarriesSource` — does not break any existing `Message.From`-only producer (empty `Source` falls back correctly). See FR-103 above for the separate documentation concern about its side effect.
- **Pre-existing `-race` failure in `internal/infrastructure/buzz`**: independently reproduced on both the feature branch and, in an isolated temporary `git worktree` at base commit `262aba0`, with byte-for-byte identical `checkptr` crash output (`fiatjaf.com/nostr`'s `writeJSONString`, triggered via `TestE3_NIPOAAuthTagIncludedOnAuthEvent`). Confirmed pre-existing and unrelated to this feature. `go test -race ./...` across the rest of the module (58 packages) passes cleanly, including the new `TestBuzzTaskBridge_ConcurrentMultiPersonaDispatch_NoCrossTalk` concurrency test.
- **`internal/application/team`'s pre-existing infra imports**: confirmed via `git show 904a732:...team_manager.go` — the import list (including `internal/infrastructure/http`, `local/queue`, `local/orchestrator`, etc.) is byte-identical to base `main`. Not a new Clean Architecture violation; the new `internal/application/orchestrator` code (`buzz_dispatch.go`, `buzz_task_bridge.go`) has zero infrastructure imports outside test files.
- **No `buzz_private_key` (or any secret) logged**: grepped every `slog`/logger call touched by this feature; none reference key material.
- **Buzz DM non-goal boundary respected**: no code changes touch NIP-17/kind-1059/gift-wrap paths; only doc mentions confirming DM remains unimplemented.
- **Coverage numbers in `README.md`**: recomputed independently via `go test -coverprofile` and match exactly (`internal/domain` 94.9%, `internal/application` 99.0%, `internal/application/orchestrator` 95.1%, `internal/application/team` 78.9%).
- **Quality gates**: `go build ./...`, `go vet ./...`, `golangci-lint run` (0 issues), `gofmt -l .` (no output), `go test ./...` (all green) all independently reproduced clean.

## Implementation Guidance for Fixes

- **Use TDD for every fix.** Each finding above starts from a failing test that reproduces the problem (a redelivered event ID after a failed dispatch; a `nil`-returning `BuzzMonitorBuilder`; a multi-byte truncation boundary) before any production code changes.
- **Conduct a brief code review after each fix before moving to the next.** Don't batch all six findings into one uninspected commit — review FR-101's fix before starting FR-102, and so on.
- **Use agent teammates and git worktrees for parallel fix workstreams if there are multiple independent fixes.** FR-101 (bridge dedup), FR-102 (test gap), FR-104 (truncation), and FR-105 (dead export) touch disjoint code and can be fixed in parallel worktrees; FR-103 is documentation-only and can proceed independently of all of them. FR-106 is a non-blocking follow-up, not part of this PR's fix set.
- **P0 items block the PR from being considered mergeable.** There are no P0 findings in this review — the P1 items (FR-101, FR-102, FR-103) should be fixed before this is considered done, but none are correctness-breaking blockers on their own merit as this PRD is written. If the team's bar for "done" requires P1 closure before merge, treat FR-101/FR-102/FR-103 as the mergeability gate instead.
