# Review PRD: Boabot Native Daemon Mode — Multi-Agent Buzz Support

**Reviewed branch:** `worktree-boabot-native-multi-agent-buzz`
**Base:** `origin/main` @ `904a732` (boabot v0.4.1)
**Commits reviewed:** `76a7df0`..`800958e` (9 commits)
**Spec:** `specs/260814-boabot-native-daemon-mode/`

> **Note on diff scope:** the local `main` ref in this worktree was stale (pointed at `e1aa9a0`, well behind `origin/main`). Diffing against it would have incorrectly pulled in ~570 unrelated lines from an already-merged, already-released PR (#33, the ACP fallback-publish fix, shipped in v0.4.1). This review uses the corrected diff against `origin/main` (`904a732`), matching exactly the 9 commits and file list this feature actually comprises.

## Executive Summary

This is a well-executed, disciplined piece of work, and every specific claim this review was asked to scrutinize held up under independent, hands-on verification rather than being taken on faith. The `router.Register` double-registration fix is correct and covered by dedicated regression tests. The `TaskPayload.Source` deviation is safe, backward-compatible, and its side effect (chat-sourced tasks finally getting `chatProvider`) is real and deliberate. Clean Architecture boundaries hold — `internal/application/team`'s infrastructure import list is byte-for-byte identical to the pre-feature baseline, and the new `BuzzTaskDispatcher` seam is correctly defined in `domain` and implemented in `application`, keeping `internal/infrastructure/buzz.Monitor`'s own imports domain-only. The Buzz DM non-goal boundary is respected (zero touches to `trigger.go`, no DM/gift-wrap/kind-1059 references anywhere in the diff). No secret value is ever logged. Concurrency correctness for the feature's actual new risk area — per-persona dispatch cross-talk — is backed by a real `-race` test against genuine `sync.RWMutex`-protected stores, not mocks, and it passes.

The claimed pre-existing `-race` crash in `internal/infrastructure/buzz` deserves a more precise framing than the implementation notes give it: it's real (independently reproduced here, identically, on both `HEAD` and the pre-feature base commit `262aba0`), but it is **already fully mitigated at the CI level** — `.github/workflows/boabot.yml` has invoked `go test -race -gcflags=all=-d=checkptr=0 ./...` since a prior spec cycle, specifically to work around this exact upstream `fiatjaf.com/nostr` bug. Run with that flag (verified directly), the entire module — including `internal/infrastructure/buzz` — passes `-race` with zero failures, and the domain+application aggregate coverage gate `boabot.yml` actually enforces (`go tool cover -func ... | grep total`, one number across every package under `internal/domain/...`+`internal/application/...`) measures 91.2%, clearing the 90% threshold. This is not what `implementation-notes.md` item #11 says, though — it claims a concurrency test placed inside `internal/infrastructure/buzz` "would be unable to run under `-race` at all," which is false for this repo's actual CI invocation. The test-placement decision itself (testing `BuzzTaskBridge.Dispatch` concurrency directly rather than through the real `Monitor`/relay stack) remains architecturally sound on its own merits; only the stated justification is wrong.

Four real issues found, none of them P0. Two are correctness bugs with zero test coverage on the exact branch that would have caught them: `BuzzTaskBridge`'s relay-replay dedup marks a Nostr event ID "seen" *before* the dispatch it's guarding actually succeeds, so a transient dispatch failure followed by a relay reconnect/replay silently and permanently drops the user's instruction — misreported in the logs as a harmless duplicate-skip, not a lost task. `buzzBoardTitle`'s title truncation slices by byte offset, not rune offset, corrupting UTF-8 for any Buzz instruction whose 80-byte mark falls mid-rune (reproduced empirically, not just inferred from the missing test). The other two are a real integration-point test gap (the exact edge case `spec.md` names by name — "incomplete Buzz config fails in isolation" — is untested at the only place it's actually wired into production) and an undocumented behavior change for existing deployments (the `chat_provider` dead-code fix makes that setting live for every current chat-sourced deployment, not just new Buzz ones, with no Breaking-Changes/changelog-visible callout). The remaining findings are documentation-hygiene and minor-completeness items. **Overall assessment: Approve with minor comments.** The four P1s should be closed before this is considered done; none block the PR on their own.

**Findings: 0 P0 / 4 P1 / 6 P2**

---

## FR-101 — Relay-replay dedup marks an event "seen" before its dispatch succeeds, permanently losing instructions on transient failure

**Priority: P1**

**Location:** `boabot/internal/application/orchestrator/buzz_task_bridge.go`, `Dispatch()` (calls `markSeenIfDuplicate` before `chatMgr.DetectAndHandle`/`dispatcher.DispatchWithSchedule` is even attempted) and `markSeenIfDuplicate()` itself.

**Problem:** `markSeenIfDuplicate(eventID)` records the event ID as seen unconditionally on first call, regardless of whether the dispatch attempt that follows succeeds or fails:

```go
func (b *BuzzTaskBridge) Dispatch(ctx context.Context, botName, eventID, threadID, instruction string) (domain.BuzzDispatchResult, error) {
	if eventID != "" && b.markSeenIfDuplicate(eventID) {   // marks seen unconditionally
		return domain.BuzzDispatchResult{Duplicate: true}, nil
	}
	// ... DetectAndHandle / DispatchWithSchedule can still fail below ...
```

If the subsequent `ChatTaskManager.DetectAndHandle` or `dispatcher.DispatchWithSchedule` call returns an error — a transient local-store hiccup, a context cancellation, anything — `Dispatch` returns the error, `Monitor.dispatchViaBridge` logs `"buzz monitor: task dispatcher bridge failed"`, and there is no retry. Per `spec.md`'s own "Relay reconnect / message replay" edge case, a relay reconnect is expected to redeliver recently-seen events — that's the exact scenario this dedup mechanism exists for. When that redelivery happens for this event ID, `markSeenIfDuplicate` reports `Duplicate: true` (because it genuinely was marked seen), `Monitor` logs `"buzz monitor: duplicate event, skipping re-dispatch"` at `Info` level, and the user's original instruction is now permanently lost — silently, with a log trail that actively misdescribes what happened as a harmless duplicate-skip rather than a dropped task.

**Evidence:** `TestBuzzTaskBridge_Dispatch_DispatcherError_Propagates` confirms the error-propagation half but never issues a second `Dispatch` with the same event ID afterward to check whether the failed attempt is later mistaken for a duplicate. No test in the suite exercises "dispatch fails, then the same event ID is redelivered."

**Acceptance criterion:** Dedup state is only recorded once the underlying dispatch (or `ChatTaskManager`-handled response) has actually succeeded — e.g. move the mark-seen call to after a successful `DispatchWithSchedule`/handled-with-no-error return, or explicitly un-mark the event ID before returning an error. A new regression test dispatches an event ID that fails, then redispatches the identical event ID, and asserts it is *not* reported as `Duplicate` and is retried.

---

## FR-102 — `buzzBoardTitle` truncates by byte index, not rune index, corrupting UTF-8

**Priority: P1**

**Location:** `boabot/internal/application/orchestrator/buzz_task_bridge.go`, `buzzBoardTitle()`.

**Problem:**

```go
if len(title) > boardItemTitleMaxLen {
    title = strings.TrimSpace(title[:boardItemTitleMaxLen]) + "…"
}
```

`title[:80]` is a byte offset, not a rune offset. Reproduced directly:

```go
instr := strings.Repeat("a", 78) + "日本語のテキストです"
title := instr[:80]
// result ends: ...aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\xe6\x97…
// utf8.ValidString(title) == false
```

Any Buzz instruction with multi-byte characters (emoji, non-Latin scripts — entirely plausible on a chat-native relay) whose 80-byte boundary falls mid-rune produces an invalid UTF-8 string, which becomes the persisted `WorkItem.Title` rendered in the orchestrator's Kanban board. This is real, user-visible data corruption for non-ASCII-heavy Buzz conversations, not a hypothetical edge case, and there is zero test coverage on the truncation branch at all — `buzzBoardTitle` measures 66.7% coverage (the lowest of any new function in this file), and no test in `buzz_task_bridge_test.go` exercises an instruction longer than 80 characters or an empty one.

**Acceptance criterion:** Truncate on rune boundaries (`[]rune(title)` slicing, or `utf8.RuneCountInString` + a rune-safe truncate helper). New tests cover: (a) an instruction whose 80-byte mark falls mid-rune, asserting `utf8.ValidString(...)` on the result; (b) the empty-instruction fallback (`"Buzz task"`); (c) ASCII-only truncation is unchanged. `go tool cover -func` reports `buzzBoardTitle` at 100%.

---

## FR-103 — The edge case `spec.md` names by name ("incomplete Buzz config fails in isolation") is untested at its real production integration point

**Priority: P1**

**Location:** `boabot/internal/application/team/team_manager.go`, `Run()`'s Buzz-monitor-builder loop, the `if mon == nil { continue }` branch.

**Problem:** `spec.md`'s Edge Cases section names this scenario explicitly: *"Bot has `buzz.enabled: true` but incomplete config... that persona's monitor construction fails in isolation — logged, does not prevent other personas' monitors or the orchestrator UI from starting."* The actual production code path is: `TeamManager.Run()` calls `tm.buzzMonitorBuilder(...)` (in production, `main.go`'s closure around `buildBuzzMonitor`), and if it returns `nil` (its documented behavior for a bad/missing key), `Run()`'s loop must `continue` without disturbing any other persona. Confirmed directly via the coverage profile (`go tool cover` raw output for `team_manager.go:397.18,398.13` — the `continue` statement's line — shows **0** hit count): this exact branch has never been exercised by a test. The three `TestTeamManager_BuzzMonitorBuilder_*` tests all supply a builder mock that unconditionally returns `&mocks.ChannelMonitor{}`; none return `nil` to simulate a builder failure. The isolation behavior *is* tested at the lower `buildBuzzMonitor` layer (`main_test.go`'s `TestBuildBuzzMonitor_KeypairLoadFailure` etc.), but the specific integration point `spec.md` calls out by name — the actual `Run()` loop not disturbing other personas — has never been exercised.

**Acceptance criterion:** Add a test where `WithBuzzMonitorBuilder`'s mock returns `nil` for one Buzz-enabled persona and a real `&mocks.ChannelMonitor{}` for another, asserting the `nil`-returning persona is skipped without affecting the other persona's monitor or `Run()`'s overall success. `go tool cover -func` shows the `mon == nil` branch at 100%.

---

## FR-104 — `chat_provider` becomes live for existing chat-sourced deployments with no Breaking-Changes/changelog-visible disclosure

**Priority: P1**

**Location:** `boabot/internal/application/execute_task.go` (`isConversationalSource`), `boabot/internal/domain/message.go` (`TaskPayload.Source`), `boabot/internal/infrastructure/local/orchestrator/task_dispatcher.go` (`sendMessage`), `boabot/internal/application/run_agent.go` (`handleTask`).

**Problem:** `implementation-notes.md` documents, honestly and in detail, that `execute_task.go:100`'s `task.Source == "chat"` check has been dead code since it was written — nothing ever set `Message.From == "chat"`, so no chat-sourced task has ever actually used a configured `models.chat_provider`. Fixing this (via the new `TaskPayload.Source` field, verified correct and safe by this review) is necessary to make the Buzz-equivalent check reachable. But the fix's blast radius is not scoped to Buzz: it also activates `chat_provider` for the *existing*, already-shipped web-UI chat path, for every current operator who has `models.chat_provider` configured and has been unknowingly getting `models.default` instead. This is a real production behavior change — potentially a different model, cost profile, or latency — for deployments that have nothing to do with this feature. It's documented in `docs/architectural-decision-record.md` (ADR-B028) and two adoption-config docs, but `spec.md`'s "Breaking Changes" section (which explicitly discusses config-schema compatibility) says nothing about it, and there is no changelog-visible callout an operator upgrading `boabot` would actually see before their chat behavior silently changes.

**Acceptance criterion:** `spec.md`'s Breaking Changes section (or an equivalent changelog-visible note) states explicitly: "operators with `models.chat_provider` configured will see it apply to chat-sourced tasks for the first time after this upgrade — previously inert." Documentation-only; no code change required.

---

## FR-105 — `implementation-notes.md`'s stated reason for testing Buzz concurrency outside `internal/infrastructure/buzz` is factually inaccurate

**Priority: P2**

**Problem:** `implementation-notes.md` item #11 states a concurrency test "inside `internal/infrastructure/buzz` itself would be unable to run under `-race` at all." This is false for this repository's actual CI configuration: `.github/workflows/boabot.yml` runs `go test -race -gcflags=all=-d=checkptr=0 ./...` — a documented workaround (with an inline comment citing the exact upstream `fiatjaf.com/nostr` bug) that predates this feature by a full spec cycle (`specs/260804-boabot-buzz-support`). Verified directly: with that flag, `go test -race -gcflags=all=-d=checkptr=0 ./internal/infrastructure/buzz/...` and the whole module (`./...`) both pass with zero failures. The bare `go test -race` crash the implementation notes reproduced is real (independently reproduced here too, identically, on base commit `262aba0`) — but only for the bare invocation, not the project's actual, already-established CI invocation. The test-placement choice itself (testing `BuzzTaskBridge.Dispatch` directly, isolating the concurrency guarantee from relay/JSON-serialization noise) remains defensible on its own architectural merits — only the stated justification for it is wrong, and should be corrected so a future reader doesn't conclude `internal/infrastructure/buzz` categorically can't be race-tested (it can; CI already proves it every run).

**Acceptance criterion:** `implementation-notes.md` item #11 is corrected to state the accurate reason (e.g. "isolates the guarantee from relay-layer noise," or an acknowledgment that CI's `-gcflags=all=-d=checkptr=0` already avoids the crash and the test could live in either place). No code change required.

---

## FR-106 — Coverage-gate framing: `internal/application/team`'s 78.9% is fine against CI's actual (aggregate) gate, but reads as a violation against README's per-package table

**Priority: P2**

**Problem:** `internal/application/team` measures 78.9% (up from a pre-feature 77.8% — not a regression; AGENTS.md's actual hard rule, "do not reduce coverage when adding code," is satisfied). Read against `README.md`'s per-package coverage table, this looks like it's failing AGENTS.md's stated "90% or above on Domain and Application layers" target. It isn't, in the sense CI actually enforces: `boabot.yml`'s Coverage-check step computes **one aggregate number** across every package matching `internal/domain/...` + `internal/application/...` (excluding `mocks/`) and checks that single total against 90% — verified directly at 91.2%, which clears the gate. This feature's own additions to `team_manager.go` are well-covered in isolation (`WithBuzzMonitorBuilder`, `chatMessageThreadID`, `boardTracksSource`, `LoadTeamConfig` all 100%; `Run()` itself 84.9%) — the package's low aggregate is dragged down by large, pre-existing, mostly-untested functions this feature only added a few lines to (`startBot` at 59.7%, `Run` at 84.9%).

**Acceptance criterion:** Either (a) file a non-blocking follow-up to raise `startBot`/`Run` coverage toward 90% (`internal/application/team` is the single largest gap in the domain+application aggregate), or (b) clarify AGENTS.md/README's coverage framing to state the bar is an aggregate across domain+application packages, not a strict per-package minimum, so this doesn't misread as a violation on every future PR touching `team_manager.go`.

---

## FR-107 — `LoadTeamConfig` was exported for a purpose the final design no longer needs

**Priority: P2**

**Problem:** `architecture.md`'s stated rationale for exporting `loadTeamConfig` → `LoadTeamConfig` is "`main.go` doesn't duplicate YAML parsing" — implying `main.go` would call it directly. Per `implementation-notes.md`'s own documented deviation #3, the design changed: the per-persona Buzz-monitor loop moved inside `TeamManager.Run()` instead, and `main.go` never ends up calling `LoadTeamConfig` at all. Confirmed by grep: its only non-test caller is `team_manager.go` calling its own now-exported function. The export is now unnecessary public API relative to the reason given for it.

**Acceptance criterion:** Either revert to unexported `loadTeamConfig` (and its test back to package-internal), or update `architecture.md`/ADR-B028 to state the real reason it needs to stay exported. Either is a small, low-risk cleanup.

---

## FR-108 — `spec.md`/`data-dictionary.md` were not kept current with implementation-time deviations

**Priority: P2**

**Problem:** Four distinct places where the planning docs now read as inaccurate relative to what shipped, none individually severe but collectively a "living document" hygiene gap the spec workflow (AGENTS.md: "specs are living documents, not written once") calls out as a rule:

1. All nine Acceptance Criteria checkboxes in `spec.md` (lines 94–103) remain unchecked despite `status.md` declaring Phase 5 "Complete." Several are demonstrably satisfied by shipped tests (e.g. no-cross-talk, `boabot -acp` still building); others are explicitly deferred to a live Buzz relay session (`tasks.md` P4.1, out of scope for this pass). Nothing distinguishes the two categories.
2. `spec.md`'s FR-008 ("A Buzz request can update or cancel a previously-created task... immediate or scheduled") overstates the shipped capability: there is no in-place task mutation at all, and cancellation only ever applies to a not-yet-confirmed pending intent — an already-dispatched or running task can't be touched from Buzz. This is honestly disclosed in `implementation-notes.md` item 8 and the getting-started guide, but `spec.md`'s own FR-008 text was never revised.
3. `spec.md`'s "Scope of Changes" states "No files need to change in `internal/infrastructure/buzz/`... its constructor already supports being called multiple times with distinct identities." In practice `monitor.go` received a substantial, well-justified change (+108/-12: `WithTaskDispatcher`, `dispatchViaBridge`/`dispatchDirect`, `publishReply` extraction) — architecturally sound, but not called out as a deviation from this specific claim the way the other four implementation-time deviations are.
4. `data-dictionary.md` documents the new `DirectTaskSourceBuzz` enum value but has no entry for the new `TaskPayload.Source` field — `implementation-notes.md`'s own "Deviations from Plan" section flags this gap explicitly but it was never backfilled.

**Acceptance criterion:** (1) Each AC checkbox in `spec.md` is checked with a test reference, or left unchecked with a one-line reason (e.g. "requires live Buzz relay session, see tasks.md P4.1"). (2) FR-008's text in `spec.md` is reworded to state the actual shipped scope (or gets a "Known Limitation" callout). (3) `implementation-notes.md`'s "Deviations from Plan" list gains a line acknowledging the `monitor.go`/`buildBuzzMonitor`-signature deviation from the "Scope of Changes" claim. (4) `data-dictionary.md` gains a `TaskPayload.Source` entry matching its existing entries' level of detail. Documentation-only; no code change required.

---

## FR-109 — `chat_provider` adoption docs still inaccurately list Slack DMs

**Priority: P2**

**Problem:** This branch edits the `chat_provider` doc line in both `user-docs/Claude-Adoption-Config.md` and `user-docs/OpenAI-Adoption-Config.md`, changing it to: "`chat_provider` overrides `default` for tasks sourced from the chat interface (Slack DMs, web UI chat, direct API chat calls) **and** from a Buzz channel `@mention`." The "Slack DMs" clause is inaccurate and was inaccurate before this PR too: Slack's dispatch path (`internal/infrastructure/slack/monitor.go`, untouched by this branch) sends its `domain.TaskPayload` directly via `queue.Send`, never setting `TaskPayload.Source` — so `task.Source` is never `"chat"` for a Slack-originated task, even after this branch's fix makes the check reachable for web-UI chat and Buzz. This isn't a regression this branch introduced, but the branch did edit this exact line without correcting the pre-existing inaccuracy it carries.

**Acceptance criterion:** The `chat_provider` doc line in both files is corrected to list only the sources that actually resolve to `task.Source == "chat"`/`"buzz"` today (web UI chat, direct API chat calls, Buzz `@mention`) — Slack DMs removed, or a separate tracking item opened if Slack is meant to eventually route through the same `Source`-tagging path.

---

## FR-110 — `BuzzTaskBridge.markSeenIfDuplicate` does a full map scan under lock on every dispatch

**Priority: P2**

**Problem:** `markSeenIfDuplicate` iterates the entire `seenEvts` map to evict expired entries on *every* call, while holding `b.mu` — the same mutex that serializes all of that persona's dispatch calls. At the default 10-minute TTL and realistic Buzz mention volume this isn't a practical problem today, but per-dispatch lock hold time grows linearly with distinct events seen in the last TTL window, and it does this on every single message, not periodically.

**Acceptance criterion:** Either accept as-is with a comment noting the bound is intentional given expected Buzz mention volume, or move eviction to a periodic sweep (e.g. every N calls, or on a ticker) instead of every call. Not required to close this PR.

---

## Verification Notes (independently reproduced, not taken on faith)

- **Clean Architecture:** `internal/application/team/team_manager.go`'s infrastructure import list is identical between base `904a732` and `HEAD` (`git show 904a732:...team_manager.go` diffed byte-for-byte against `HEAD`'s import block) — zero new infra imports added. `grep -rn "infrastructure/buzz" internal/application/` finds only comments, never an import.
- **`router.Register` double-registration fix:** correct and tested — `TestBuildBuzzMonitor_QueueAlreadyRegistered_DoesNotDoubleRegister`, `TestRouter_Lookup_Registered`/`_Unregistered`, `TestTeamManager_BuzzMonitorBuilder_DuplicateBotName_IsolatedNotPanic` all exercise the actual `panic`-avoidance path, not just the happy path.
- **`TaskPayload.Source`:** `domain.Task.Source` has exactly one other read site in the codebase (`execute_task.go`'s `isConversationalSource`) — grepped and confirmed. Backward-compatible (empty `Source` falls back to pre-existing `Message.From` behavior).
- **`-race` crash in `internal/infrastructure/buzz`:** reproduced on `go1.26.0 darwin/arm64` — identical `fatal error: checkptr: pointer arithmetic result points to invalid allocation` in `fiatjaf.com/nostr.writeJSONString` (`event.go:245`) on both `HEAD` and an isolated worktree of base commit `262aba0`. **With CI's actual flag** (`-gcflags=all=-d=checkptr=0`), the crash does not occur anywhere in the module — verified directly, `go test -race -gcflags=all=-d=checkptr=0 ./...` is fully green, 72 packages, zero failures. See FR-105.
- **Coverage:** `go test -race -gcflags=all=-d=checkptr=0 -coverprofile=... ./internal/domain/... ./internal/application/...` (CI's exact command) measures 91.2% aggregate, clearing the 90% gate `boabot.yml` enforces. Per-package numbers match `README.md`'s table exactly: `internal/domain` 94.9%, `internal/application` 99.0%, `internal/application/orchestrator` 95.1%, `internal/application/team` 78.9%.
- **Quality gates:** `go build ./...`, `go vet ./...` (0 warnings), `golangci-lint run` (0 issues), `gofmt -l .` (0 files), `go test ./...` (all green), `go test -race -gcflags=all=-d=checkptr=0 ./...` (all green) — all reproduced directly.
- **DM non-goal:** zero diff to `internal/infrastructure/buzz/trigger.go`; no occurrence of `1059`, `giftwrap`/`gift-wrap`, or DM-handling logic anywhere in the branch's diff.
- **Secrets:** `buzz_private_key`/`buzz_api_token`/`buzz_auth_tag` resolution is unchanged, pre-existing logic, correctly namespaced per `botCfg.Buzz.BotName`, invoked once per persona instead of once globally. No secret value reaches a log call anywhere in the diff.
- **`techLeadPool.Deallocate` on a never-`Allocate`d Buzz board item:** verified harmless by reading `pool.Deallocate` directly — the not-found case returns an error before any mutation of `p.entries`; the call site only `slog.Warn`s.
- **Two minor, optional completeness notes, not full findings:** `main.go`'s actual `WithBuzzMonitorBuilder` closure (the real production glue combining `ChatTaskManager`+`BuzzTaskBridge`+`buildBuzzMonitor`) has no end-to-end test of its own — `cmd/` is excluded from AGENTS.md's coverage gate, and each piece is unit-tested separately, but a parameter-order mistake between two same-typed strings (`botsDir`, `memoryRoot`) would go undetected by either side's unit tests; manually verified correct by inspection. Separately, no test exercises concurrent `Dispatch` calls *within* one persona across multiple Buzz channels/threads (only cross-persona concurrency is `-race`-tested) — by inspection this is safe (`ChatTaskManager.pendingMap` is `sync.Map`; `BuzzTaskBridge.seenEvts` is `sync.Mutex`-protected), and the whole module's `-race` run is clean, so this is a coverage-completeness note, not a suspected bug.

---

## Implementation Guidance for Fixes

- **Use TDD for every fix.** Each finding above starts with a failing test that reproduces the problem — a redelivered event ID after a failed dispatch (FR-101), a multi-byte truncation boundary (FR-102), a `nil`-returning `BuzzMonitorBuilder` (FR-103) — before any production code changes. This applies to test-only additions too: write the test, confirm it exercises the previously-uncovered branch, then leave production code as-is if it's already correct (e.g. FR-103 once written may reveal the `continue` logic needs no change at all).
- **Conduct a brief code review after each fix, before moving to the next.** Don't batch all ten findings into one uninspected commit — land, review, then proceed. Confirm `go fmt`, `go vet`, `golangci-lint run`, and `go test -race -gcflags=all=-d=checkptr=0 ./...` are clean for each affected package before moving on.
- **Use agent teammates and git worktrees for parallel fix workstreams.** FR-101 (bridge dedup) and FR-102 (title truncation) touch the same file but different functions and can proceed in parallel; FR-103 (test gap) is fully independent; FR-104, FR-105, FR-106, FR-107, FR-108, FR-109 are documentation-only and independent of every code fix and each other — farm them out to separate worktrees/agents rather than serializing.
- **P0 items block the PR from being considered mergeable.** There are no P0 findings in this review. FR-101 through FR-104 (P1) should be closed before this is considered done, per dev-flow Step 9's rule that every review finding gets a corresponding commit — but none of them are correctness/security/AC-breaking blockers severe enough to prevent merge on their own if the team's timeline requires otherwise. FR-105 through FR-110 (P2) are recommended, not required.
- Before closing dev-flow Step 9, check each of the ten findings above off explicitly against the commit log — do not rely on memory (AGENTS.md's TDD section).
