# Review PRD: Buzz DM Support and Full Threaded-Reply Support

**Reviewed branch:** `feat/buzz-dm-and-thread-support`
**Base:** `main` @ `e1aa9a0`
**Commits reviewed:** `37b39c4`..`de2bc63` (feature commits `c61a72f`, `fd0e7a8`, `e680676`, plus dashboard-only commits)
**Spec:** `specs/260814-buzz-dm-and-thread-support/`

## Executive Summary

This is careful, security-conscious work, and the specific claims this review was asked to independently verify — not read-and-trust — held up against the vendored library's actual source, not its doc comments. Concretely verified, not merely re-read:

- `fiatjaf.com/nostr`'s `nip44.GenerateConversationKey` (nip44.go:170) does format the raw secret-key bytes (`sk[:]`) into its error string on the out-of-range-key path. Both boabot call sites that can reach it — `handleDMEvent`'s gift-unwrap failure and `publishDMReply`'s prepare failure — omit the underlying error's text from their log calls, closing the leak at every reachable site, including the `GiftWrap`-internal call using the random per-message ephemeral key (not just the caller's own key).
- `RelayClient.PublishRaw` (relay_client.go:506) provably does not sign the event or overwrite `PubKey` — confirmed by reading the method body, not its comment. `Publish` (the ordinary path) does both; using it for gift-wrapped events would have defeated NIP-17 entirely.
- `nip17.PrepareMessage` is called with `modify == nil` at its one call site (dm.go:251); `GiftWrap`'s ephemeral-key/randomized-timestamp behavior (nip59.go) is therefore never overridden.
- The self-message-loop filter compares against `rumor.PubKey`, which the vendored `nip59.GiftUnwrap` sets from `seal.PubKey` (nip59.go:95) only *after* `seal.VerifySignature()` succeeds — the comparison is against a cryptographically authenticated value, not an attacker-controlled field inside the decrypted JSON, and `nostr.PubKey` is a plain `[32]byte`, safely `==`-comparable.
- `KnownThread`/`dispatchedThreads` and the pre-existing `seenEvts` dedup map share one lock (`b.mu`); a dedicated `-race` test drives concurrent same-event-ID and multi-persona dispatch through the real stores and passes.
- All quality gates were re-run directly in this review, not taken from the spec's own claim: `go build ./...` clean, `go vet ./...` clean, `golangci-lint run` reports 0 issues, `go test -race -gcflags=all=-d=checkptr=0 ./...` passes across every package including `internal/infrastructure/buzz`, and the domain+application aggregate coverage (statement-weighted, excluding `mocks/`, computed directly from the coverage profile rather than averaging per-function percentages) is **91.4%** against the 91.3% baseline spec.md claims — not a regression.
- Clean Architecture holds: no `fiatjaf.com/nostr` (or any of `nip17`/`nip44`/`nip59`) import anywhere under `internal/domain` or `internal/application`; the new `domain.RelayClient.PublishRaw` and `domain.BuzzTaskDispatcher.KnownThread` are plain, dependency-free interface additions.

**Confidence boundary:** this review verified boabot's own code against the vendored library's actual source and against real (non-mocked, `-race`-driven) concurrency tests. It did **not** independently audit the vendored `fiatjaf.com/nostr` package's cryptographic implementation itself (NIP-44 encryption, Schnorr signing, gift-wrap construction) — that library's crypto internals are treated as a trusted, unmodified dependency, consistent with the architecture's own decision to use its high-level API rather than reimplement primitives. This review also did not exercise the DM/thread paths against a live Nostr relay — all verification is code-level and unit/integration-test-level (see the Acceptance Criteria table below for exactly which criteria that covers and which it can't).

One real, user-facing correctness issue was found (P1): every Buzz-dispatched task's reply is now written to the operator's shared chat store **twice** and both copies pass the global chat feed's filter, so `GET /api/v1/chat` shows each bot reply as two rows. Five minor items round out the findings (P2) — none block merge on their own.

**Overall assessment: Approve with minor comments.**

**Findings: 0 P0 / 1 P1 / 5 P2**

---

## FR-301 — Buzz task replies are recorded twice in the shared chat store, both copies visible in the operator's global chat feed

**Priority: P1**

**Location:** `boabot/internal/application/team/team_manager.go` (`chatMessageThreadID`, `WithTaskResultHandler`'s closure, ~line 1025-1040), `boabot/internal/infrastructure/buzz/monitor.go` (`recordOutbound`, called from `publishReply`/`publishDMReply`), `boabot/internal/infrastructure/http/server.go` (`handleChatList`, line 1637).

**Problem:** `implementation-notes.md`'s Deviation 5 documents this as an accepted tradeoff and frames the net effect as "Buzz conversations now surface in the operator's global chat feed (previously excluded)." Verified against the actual code, the effect is stronger than that framing suggests — it is a visible duplication, not just a new surfacing:

1. `team_manager.go`'s generic `WithTaskResultHandler` (registered for every bot, Buzz included, since `BuzzTaskBridge.dispatcher` is the same `LocalTaskDispatcher` backed by `tm.sharedTaskStore`) appends every task's output to `tm.sharedChatStore` with `ThreadID: chatMessageThreadID(task)`. For `domain.DirectTaskSourceBuzz`, `chatMessageThreadID` returns `""` (by design, predating this feature).
2. This feature's `Monitor.recordOutbound` (called from both `publishReply` and `publishDMReply`, *after* a successful relay publish) appends the **same reply content** a second time, correctly `ThreadID`-keyed (`root` or `dm:<pubkey>`), because that correct key is what `BuzzTaskBridge.buildInstructionWithHistory`'s `ChatStore.List(threadID)` needs on the next turn.
3. `handleChatList` (`GET /api/v1/chat`) filters out only threads with a `board-` prefix (`server.go:1650`). Neither `""` nor `"dm:<pubkey>"`/a bare thread-root hex carries that prefix, so **both copies pass the filter and both are returned** to the operator's global chat screen.

Net effect, confirmed by reading `handleChatList` directly (not inferred from the append-site alone): every Buzz-dispatched task's bot reply appears as two separate rows in the operator's chat UI. This is distinct from — and should not be conflated with — the *intended* part of this deviation: inbound Buzz/DM messages (`BuzzTaskBridge.recordInbound`, correctly threaded) surfacing in the global feed is explicitly wanted per spec.md's "visible on the shared board per explicit product decision" and is documented in `user-docs/Buzz-Adoption-Config.md`. That part is not a finding. The finding is specifically the reply-side duplication.

FR-206 (conversation-continuation via `ChatStore.List(threadID)`) is unaffected — the `""`-keyed copy never matches a real `threadID` filter, so it's invisible to the replay logic that matters functionally. This is a UI-correctness issue, not a dispatch-correctness one.

**Acceptance criterion:** A Buzz-dispatched task's reply appears exactly once in `GET /api/v1/chat`'s response. Either (a) `chatMessageThreadID` passes the real Buzz `ThreadID` through instead of `""` (removing `recordOutbound`'s duplicate write), with the failure-mode regression implementation-notes.md itself flags — a relay-publish failure must still leave a chat record of the bot's output — addressed by having the generic handler remain the single writer for both success and failure, or (b) `handleChatList` (and `ListByBot`, if it has the same exposure) additionally excludes the `ThreadID: ""` copy specifically for Buzz-sourced tasks. A new test drives a Buzz-dispatched task through `HandleResult` end-to-end and asserts `sharedChatStore` contains exactly one message with the bot's output content, not two.

---

## FR-302 — `dispatchedThreads` has no TTL/eviction; it grows for the lifetime of a long-running daemon process

**Priority: P2**

**Location:** `boabot/internal/application/orchestrator/buzz_task_bridge.go`, `dispatchedThreads` field and `markDispatchedThread`.

**Problem:** `seenEvts` (event-ID relay-replay dedup) is swept for expired entries on every `checkAndMarkSeen` call. `dispatchedThreads` (thread-continuation state, `KnownThread`) deliberately has no TTL — architecture.md's RQ1 resolution treats a conversation's *relevance* as fading via the 10-message `ChatStore` replay window, which is a sound argument for why an old thread's *history* stops mattering. It doesn't address memory growth: the map entry itself is never removed, so a boabot process running in native daemon mode for weeks accumulates one permanent entry per distinct channel thread root and per distinct DM counterparty for as long as the process runs. This is bounded in practice once `respond_to`/`respond_to_allowlist` is configured (only known senders can create new entries), but unbounded under the documented default (open author gate, open DM reachability — see the Fail-Open DM Author-Gate section below) combined with a public or semi-public relay.

**Acceptance criterion:** Either document the accepted growth rate (entries are small — two strings and a `time.Time` — and bounded by realistic mention/DM volume, so this is a non-issue at expected scale) as an explicit, reviewed decision in implementation-notes.md, or add a bounded eviction policy (e.g. a max-entry LRU, or a TTL matching/exceeding the `ChatStore` replay window so it doesn't evict live conversations). No test currently exercises unbounded growth either way.

---

## FR-303 — Gift-unwrap (NIP-44 decrypt + Schnorr signature verification) runs before the author gate on every inbound DM

**Priority: P2**

**Location:** `boabot/internal/infrastructure/buzz/dm.go`, `handleDMEvent` — order is: translate → `nip59.GiftUnwrap` (two NIP-44 decrypts, one Schnorr `VerifySignature`) → self-check → `taskDispatcher` nil-check → `m.gate.allows(...)`.

**Problem:** This ordering is effectively unavoidable — the sender's pubkey isn't known until after the seal is decrypted and verified, so the gate cannot run first. But it does mean boabot performs real cryptographic work (decrypt attempts, signature verification) for **any** kind:1059 event addressed to the persona's pubkey, regardless of whether the eventual sender would pass the author gate — unlike the channel path, where `dispatch()` checks `maxContentLen` (a cheap length check) *before* `m.gate.allows(...)`, explicitly to avoid spending any more work than necessary on content that the gate or size check will reject anyway (see `dispatch`'s own comment: "there is no reason to spend a gate check on content that will be rejected anyway"). There is no equivalent cheap pre-filter on the DM path (an oversized gift-wrap event, for instance, is not rejected before `GiftUnwrap` is attempted). No demonstrated exploit — this is inherent to how gift-wrap addressing works, and relay-side abuse controls (if any) are outside boabot's control — but it is a real, if narrow, distinction from the channel path's care about pre-gate cost, worth a conscious accept-as-is note rather than leaving unaddressed by omission.

**Acceptance criterion:** Either document this as an accepted, inherent property of NIP-17 addressing (no code change needed — the async work is bounded per-event and per-persona, and Nostr relays already gate event size independently), or add a size check on the raw gift-wrap event (mirroring `maxContentLen`'s channel-side role) before attempting `GiftUnwrap`.

---

## FR-304 — No startup warning when DM listening activates with an unconfigured (fail-open) author gate

**Priority: P2**

**Location:** `boabot/cmd/boabot/main.go`, `buildBuzzMonitor` (`WithDMKeyer` is always added once `bc.Enabled` and a key resolve); `boabot/internal/infrastructure/buzz/monitor.go`, `Start`.

**Problem:** `Monitor.Start` already has precedent for exactly this pattern: when `Config.LockDir` is empty, it logs a loud `Warn` naming the specific protection that is inactive ("FR-031/OQ-1 multi-instance protection is INACTIVE for this monitor") so a missed wiring step is greppable in the running process's own logs. DM reachability with no `respond_to`/`respond_to_allowlist` configured is a materially similar situation — a real, load-bearing default that widens the persona's reachable surface to any Nostr identity that discovers its pubkey (which the persona itself routinely publishes — see the Fail-Open DM Author-Gate section) — but there is no equivalent log line. The only place this is currently visible to an operator is `user-docs/Buzz-Adoption-Config.md`'s prose (which is candid and correctly worded — see below), not the running process's own output.

**Acceptance criterion:** When DM support activates (`dmKeyer != nil` in `run()`/`startDMSubscription`) and `!m.gate.active()`, log a `Warn` at that point naming the condition explicitly (e.g. "buzz monitor: DM listening active with no respond_to/respond_to_allowlist configured; any Nostr identity that discovers this persona's pubkey can dispatch a task via DM"), mirroring the `LockDir`-empty warning's style and greppability. A test asserts the warning is emitted when DM activates with `gate.active() == false` and is absent when a gate is configured.

---

## FR-305 — Outbound NIP-10 tagging satisfies FR-207 as literally written but is narrower than full multi-hop NIP-10 convention; FR-207's DM parenthetical is broader than the accepted/implemented behavior

**Priority: P2**

**Location:** `boabot/internal/infrastructure/buzz/monitor.go`, `publishReply` (tags); `specs/260814-buzz-dm-and-thread-support/spec.md`, FR-207.

**Problem, two related but distinct observations:**

1. **Channel replies:** `publishReply` emits exactly the three tags FR-207 names — root-marked `e`, reply-marked `e` for the immediate parent (omitted when parent == root), and one `p` tag for the immediate parent's author. This is spec-compliant. Full NIP-10 convention for deep, multi-participant threads additionally recommends carrying forward the `p` tags already present on the event being replied to (accumulating every prior participant, not just the immediate one), so a client can build a complete participant list without walking the whole thread. Verified this is not implemented: only one `p` tag is ever added. In boabot's actual usage (a bot replying to a single human in a NIP-29-moderated channel thread), this gap has limited practical effect — most threads have exactly one non-bot participant — but it is a real gap against the stricter NIP-10 convention, worth noting explicitly rather than silently accepting because FR-207's literal wording happens not to require it.
2. **DM replies:** implementation-notes.md's Deviation 6 documents, correctly, that DM replies carry no `e`/root tags at all (NIP-17 1:1 DMs aren't NIP-10-threaded — there's no thread to reference), and that this matches spec.md's Success Criteria list, which gives DM replies their own separate acceptance line ("correctly gift-wrapped and decrypt correctly for the sender") distinct from the channel line naming the three tags. This is the correct reading and the right implementation. The gap is spec-text hygiene: FR-207's summary sentence says "(channel and DM)" in a way that a future reader checking FR-207 alone (not cross-referencing the Success Criteria) could reasonably read as requiring NIP-10 tags on DM replies too.

**Acceptance criterion:** (a) No code change required for item 2 — tighten FR-207's wording in spec.md to explicitly scope the three-tag requirement to channel replies only, cross-referencing the DM line, so a future reviewer doesn't reopen this as a defect. (b) For item 1, either accept as-is with a documented rationale (single-participant threads are the realistic case for this deployment shape) or extend `publishReply` to carry forward the parent event's own `p` tags plus the new one, with a regression test asserting a 3-hop reply chain accumulates all three participants' `p` tags.

---

## FR-306 — `markDispatchedThread`/`recordInbound` are not rolled back when a subsequent dispatch attempt fails (unlike `unmarkEvent` for relay-replay dedup)

**Priority: P2**

**Location:** `boabot/internal/application/orchestrator/buzz_task_bridge.go`, `Dispatch` — `b.markDispatchedThread(botName, threadID)` and `b.recordInbound(ctx, botName, threadID, instruction)` run unconditionally before `chatMgr.DetectAndHandle`/`dispatcher.DispatchWithSchedule`, and neither is undone if either subsequently errors (only `b.unmarkEvent(eventID)` is called on failure).

**Problem:** If a dispatch attempt fails after these two calls, the thread is nonetheless left marked "known" (`KnownThread` returns true) and the inbound message is left recorded in `ChatStore`, even though no task was actually created for that turn. This is arguably the right behavior — a human did address the bot in that thread, so recognizing a follow-up reply without re-mention still seems correct — and `TestBuzzTaskBridge_KnownThread_DuplicateEventDoesNotUnmarkThread` suggests this was a deliberate choice, not an oversight. It is flagged here only because it's an asymmetry with the `eventID` dedup handling worth an explicit one-line rationale in implementation-notes.md, not because it's suspected to be wrong.

**Acceptance criterion:** No code change required. Add one sentence to implementation-notes.md's Technical Decisions explicitly stating that `dispatchedThreads`/`ChatStore` state is intentionally not rolled back on a failed dispatch attempt, distinguishing it from the `eventID` dedup rollback, so a future reader doesn't mistake the asymmetry for a bug.

---

## Fail-Open DM Author-Gate: Independent Verification of the "Consistent with Existing Channel Behavior" Framing

This review was asked to independently verify, not accept on the basis it was accepted, the claim that the DM path's unconfigured-gate default is "consistent with existing channel behavior." The answer splits into two parts that must not be collapsed into one:

**Code-level: CONFIRMED, exactly.** `internal/infrastructure/buzz/trigger.go`'s `authorGate.allows(pubkeyHex string) bool` is the single implementation both paths use — `monitor.go:528` (`dispatch`, channel) and `dm.go:175` (`handleDMEvent`, DM) both call `m.gate.allows(...)` on the *same* `authorGate` value, with the *identical* nil-allowlist/empty-`respond_to` = allow-everyone semantics (`trigger.go:60-62`'s `active()`). There is no separate, more permissive DM-specific gate anywhere in the diff. (Note for future auditors: `guard.go` in this package is a different mechanism entirely — the p-gate *subscription filter* validator for FR-016/protocol-trap avoidance, not the author-authorization gate; the author gate lives in `trigger.go`. Anyone auditing "the gate" by opening `guard.go` is looking at the wrong file.)

**Exposure-level: the "consistent" framing is technically true but materially misleading if used alone to justify accepting the risk, and this review confirms that independently, not just by re-reading the PRD's own framing.** The two paths reach the identical gate code through very different front doors:

- **Channel:** to ever reach `dispatch()`'s gate check, an event must first be relay-accepted as a `kind:9` message tagged to a NIP-29 group (`discovery.go`'s comment: "kind:9, which any channel member can publish"). Group membership is a relay-enforced access-control step — normally a human moderator/admin adding a member — that happens *before* boabot's own code ever sees the event.
- **DM:** to reach `handleDMEvent`'s gate check, an event only needs to be a `kind:1059` gift-wrap `#p`-tagged to the persona's pubkey. There is no group, no membership, no relay-side curation step at all — any Nostr identity that has the persona's public key can address it directly. And the persona's public key is not a secret the system withholds: `RelayClient.Connect` publishes a `kind:0` profile for it on first connect (`WithProfile`/`publishProfile`), and every `kind:9` channel message and NIP-42 AUTH event it ever sends carries it in the clear. Discovering a persona's pubkey requires nothing more than observing it participate anywhere, once.

So the gate code is provably identical, but the population that can ever present a pubkey to that gate is not — channel membership is a real, external filter upstream of the gate; DM reachability has no equivalent. "Consistent with existing channel behavior" is an accurate statement about the gate function's code, and a misleading one if it was used, standing alone, as the basis for treating the DM default's real-world risk as equivalent to the channel default's. It should not have been the sole justification offered for accepting the risk, even though the code claim itself checks out.

**Already substantially mitigated in what actually shipped, independent of this review.** `user-docs/Buzz-Adoption-Config.md` (lines ~153-157) states this asymmetry to the operator in direct, bolded prose: *"Read this before assuming DMs are gated the same way channels are... any Nostr identity that knows a persona's public key can send it a DM, and if respond_to/respond_to_allowlist is left unconfigured (the default), that DM dispatches a real, budget-consuming task exactly like an in-gate channel mention would."* This means the shipped artifact does not mislead the operator — the documentation is more careful and more honest than the "consistent with channel behavior" summary alone would suggest. The gap this review identifies is specifically in the reasoning used to *accept* the tradeoff, not in what operators are ultimately told.

**Recommendation:** FR-304 above (a startup `Warn` when DM listening activates with no gate configured, mirroring the existing `LockDir`-empty warning pattern) is the cheap, low-risk hardening that would make this greppable in a running process's own logs, not just discoverable by reading the adoption doc. Rated P2, not P1/P0 — the operator-facing documentation already discloses the risk clearly, and the repo owner has already accepted it with that disclosure in place; this is a "make it cheaper to notice at runtime too" suggestion, not a blocker.

---

## Acceptance Criteria Cross-Check (spec.md Success Criteria)

All 11 items verified against code + the existing unit/integration test suite (re-run in this review, not taken on faith). None of these were exercised against a live Buzz relay — see the Executive Summary's confidence boundary.

| # | Criterion | Status | Evidence |
|---|---|---|---|
| 1 | Persona receives/responds to a DM sent to its own pubkey | PASS | `dm.go handleDMEvent`; `TestMonitor_HandleDMEvent_AuthorizedSender_Dispatches` |
| 2 | DM-triggered task appears in Tasks UI + Kanban item, correct `bot_name`, DM-labeled | PASS | `dmBoardLabel` prefix + `BuzzTaskBridge.createBoardItem`; `dmBoardLabel`/board-item tests |
| 3 | DM from unauthorized sender does not dispatch | PASS | `dm.go:175` `m.gate.allows(...)` before dispatch; `TestMonitor_HandleDMEvent_UnauthorizedSender_NotDispatched` |
| 4 | In-thread reply without re-mention is recognized/dispatched | PASS | `matchKnownThread`/`threadReplyCandidates`; `TestMonitor_HandleChannelEvent_ThreadReplyWithoutMention_KnownThread_Dispatches` |
| 5 | In-thread reply continues the same task/conversation context | PASS | `buildInstructionWithHistory` (ChatStore replay); `TestBuzzTaskBridge_Dispatch_ChatStore_SecondTurn_ReplaysBotReply` |
| 6 | Two concurrent threads/DM conversations keep independent scheduling-confirmation state | PASS | `ThreadID` = NIP-10 root / `dm:<pubkey>`, not channel UUID; `TestMonitor_Dispatch_ConcurrentThreadsSameChannel_IndependentThreadIDs` |
| 7 | Outbound channel replies carry root `e`, reply `e`, `p` tags | PASS (see FR-305 for a scope note) | `publishReply` tag construction; tag-assertion tests in `monitor_bridge_test.go` |
| 8 | Outbound DM replies gift-wrapped correctly, decrypt for the sender | PASS | `publishDMReply` via `nip17.PrepareMessage`+`PublishRaw`; `TestMonitor_PublishDMReply_GiftUnwrapsCorrectlyForRecipient` (round-trip decrypt asserted) |
| 9 | `boabot -acp` still builds and passes its existing tests, untouched | PASS | `internal/infrastructure/acp` untouched by this diff's DM/thread logic; `go test -race` run in this review shows the package passing at 90.3% coverage |
| 10 | No private key / conversation key / DM plaintext in logs | PASS | Verified against vendored `nip44.go:170`'s leak path directly, plus every log call site in `dm.go`; `TestMonitor_HandleDMEvent_ValidDM_PlaintextNeverLogged`, `_MalformedDM_NoErrTextLogged`, `TestMonitor_PublishDMReply_LogsNeverContainPlaintextOrKeys` |
| 11 | A persona's own outbound DM does not trigger a self-dispatch loop | PASS | `rumor.PubKey == selfPK` check, verified authenticated via `nip59.GiftUnwrap`'s `seal.PubKey` assignment; `TestMonitor_HandleDMEvent_SelfCopy_NotDispatched` |

Quality gates (re-run in this review): `go fmt`/`go vet`/`golangci-lint run` clean; `go test -race -gcflags=all=-d=checkptr=0 ./...` passes; domain+application aggregate coverage 91.4% (statement-weighted from the coverage profile, `mocks/` excluded) vs. spec.md's 91.3% baseline — not a regression.

---

## Implementation Guidance for Fixes

- **TDD for every fix, no exceptions.** Each finding above gets a failing regression test first (the acceptance criterion describes exactly what that test must assert), then the minimum production change to make it pass, per AGENTS.md's non-negotiable red-green-refactor rule. This applies to FR-301 (P1) as much as the P2 documentation-only items that still specify a test where a code change is involved (FR-304).
- **Brief, independent review per fix.** Each finding is a small, self-contained change — review them individually rather than batching into one large "review fixes" review, so a reviewer can confirm each acceptance criterion is actually met without the noise of five unrelated diffs in one pass.
- **Parallelize independent fixes.** FR-301 (chat-store double-write), FR-302 (dispatchedThreads eviction), FR-303/FR-304 (DM pre-gate cost / startup warning — related enough to do together), and FR-305/FR-306 (documentation-only) touch disjoint code paths and can be implemented concurrently by separate worktrees/agent teammates without merge conflicts. Land them as separate commits (or separate small PRs) referencing this review PRD, not one combined commit — matches this repo's existing review-fix precedent (`specs/archive/260814-boabot-native-daemon-mode-auto-review/`).
- **P0 items block mergeability; there are none here.** This review found 0 P0s — nothing in this list blocks merging the feature branch on correctness or security grounds. FR-301 (P1) should be closed before considering this feature "done" per dev-flow's own rule ("every finding in the review PRD must have a corresponding commit before the step is marked complete... P0 findings that remain open block the PR"), but it does not block the PR itself since it is P1, not P0. The five P2 items are recommended, not required, before closing this pass.
- **Check each finding off explicitly against the commit log**, per AGENTS.md's dev-flow Step 9 instruction, rather than relying on memory — six findings (FR-301 through FR-306) means six commits (or six explicit "documented, no code change" notes for the documentation-only ones) to check off.
