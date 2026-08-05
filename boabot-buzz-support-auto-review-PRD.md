# PRD: Code Review Fixes — BaoBot Buzz Support

**Created:** 2026-08-04
**Status:** Draft
**Branch:** `worktree-buzz-support-prd`
**Reviewed range:** `ad7a679..HEAD` (`boabot-buzz-support-PRD.md` FR-001–FR-054; ~14,200 lines across 92 files)

---

## Executive Summary

This feature (BaoBot joining Block's Nostr-based "Buzz" workspace, plus an OS-keystore secret-storage subsystem) was built across nine incremental phases with an unusually high degree of self-documented rigor: `implementation-notes.md` records numerous advisor-caught fixes made before commit, and several of the specific traps this review was asked to hunt for (the nil-vs-empty-allowlist lockout, the file-provider fallback that would have collapsed two bots' secrets onto one key, the `checkptr` CI flag) turned out to already be fixed and were independently re-verified here, not just taken on faith. The secret-storage subsystem in particular is clean: every claim about secret handling (no logging, no subprocess-argv leakage, per-provider timeout isolation) was independently verified by reading the code and the tests that back it, including running the full test/lint/coverage suite ourselves rather than trusting `implementation-notes.md`'s narrative.

The gaps this review found all sit at the seams between phases — exactly where a single-pass, cross-phase review is expected to find things that nine phases' worth of individually-green tests did not catch. Two are P0. First, **the feature's headline capability — a NIP-OA/NIP-AA owner-attested agent joining a channel without being explicitly enrolled — cannot work in the shipped binary.** The extension point that carries an owner-issued attestation tag (`buzz.WithAuthTagFunc`, backed by a fully-built and unit-tested `nipoa.go`) is never called from `cmd/boabot/main.go`'s production wiring, and no config field or named secret exists to source a tag from — `config.go`'s own doc comment even claims a resolution path that was never built. This is not one of the already-disclosed "requires live infrastructure" gaps; it is a missing wiring step, reachable by static inspection alone, and it is not on `implementation-notes.md`'s manual-verification list. Second, **`RelayClient`'s subscription-attach machinery has two independent, previously-undiscovered concurrency races** — a new `Subscribe` call racing a concurrent reconnect's `resubscribeAll`, and a reconnect's re-attach racing a concurrent `Close()` — both with a credible path to an unrecovered `panic: send on closed channel` that would take down the entire `boabot` process (every bot in it, not just Buzz), not just the Buzz monitor. Neither is caught by `-race` today because the existing tests drive the fake connection deterministically rather than under real scheduling pressure.

Five smaller P2 findings round things out: a benign local-only TOCTOU in the process-singleton lock's stale-lock reclaim, no application-level size bound on inbound (attacker-controlled) event content, a threat-model documentation gap around where NIP-OA validation is and isn't exercised against untrusted data, one small dead-wiring gap (reconnect backoff isn't operator-configurable, though nothing requires it to be), and a planned-but-never-created file (`kinds.go`) plus one overstated scope description in `spec.md`/`tasks.md`. None of the P2s are security-critical or block merge on their own. Overall: the implementation is well-engineered and the team's own self-review process caught a great deal — but the findings below are real, independently verified against the code (not the notes describing it), and the two P0s are exactly the kind of cross-phase seam defect a 9-phase incremental build should expect a fresh reviewer to find.

---

## Functional Requirements

---

### FR-001 (P0): NIP-OA owner-attestation tag is never wired into production — NIP-AA virtual membership cannot work

**Finding:** Phase E (`internal/infrastructure/buzz/nipoa.go`) built and thoroughly tested `SignAuthTag`/`ValidateAuthTag`/`StaticAuthTagFunc`, and Phase D (`relay_client.go`) built the `WithAuthTagFunc`/`AuthTagFunc` extension point these compose with — `buildSignFn` appends the configured tag to the AUTH event before signing, exactly as NIP-OA requires. `phase_e_wiring_test.go` proves the mechanism composes correctly end to end. **None of this is ever invoked from `cmd/boabot/main.go`.** `buildBuzzMonitor`'s `opts` slice (`main.go:193-205`) only ever appends `WithLogger`, `WithProfile`, and conditionally `WithAPIToken` — `buzzinfra.WithAuthTagFunc` and `buzzinfra.StaticAuthTagFunc` are never referenced anywhere in `main.go` (confirmed independently three times: by direct reading, and by two separate investigative passes each grepping the whole repo for `WithAuthTagFunc`/`StaticAuthTagFunc` usage outside `internal/infrastructure/buzz/` and finding zero hits). There is no gap anywhere else to compensate: `config.BuzzConfig` (`internal/infrastructure/config/config.go:80-92`) has no field for a pre-issued auth tag or its `conditions` string, and no `domain.SecretRef` name for one exists alongside `buzz_private_key`/`buzz_api_token` (`keypair.go`, `token.go`). `owner_pubkey` in `BuzzConfig` is unrelated — it only feeds `Monitor.Config.OwnerPubkeyHex`, consulted solely by the `!shutdown` gate (`authorGate.allowsShutdown`), never by anything in the AUTH path.

Worse than a simple omission: `config.go`'s own `BuzzConfig` doc comment (lines 57-61) states secret material "the agent's private key/nsec, **an optional NIP-OA auth tag**, and `BUZZ_API_TOKEN` — MUST NOT appear under this block: it resolves only through the FR-002 `domain.SecretStore` credential path... see `internal/infrastructure/buzz.PrivateKeySecretName`/`APITokenSecretName`" — asserting a third, symmetric resolution mechanism that parallels the two that actually exist and are wired. It was never built.

The result: every `boabot` deployment built from this branch authenticates to Buzz as a bare pubkey with no owner attestation. Per the PRD, this means the agent can only participate as an explicitly-enrolled member of every channel — the "join via owner attestation without being explicitly enrolled" workflow the PRD names as this feature's differentiator (spec.md Goal G3; the PRD's own literal acceptance criterion: *"A NIP-OA `auth` tag issued by an owner key grants the agent relay access via NIP-AA without the agent being explicitly enrolled as a relay member"*) is unreachable in the code as shipped, regardless of the `//go:build integration` stub (`TestLiveRelay_NIPOAWritePathUnenrolled`) that exists to exercise this path against real infrastructure — that stub proves the mechanism works when directly configured on a test `RelayClient`, not that a real deployment built from `main.go` can ever reach it. This was not caught by any phase's own tests because each phase's tests exercise the mechanism in isolation (`phase_e_wiring_test.go`) rather than checking that Phase H's wiring actually reaches it, and it is not on `implementation-notes.md`'s "Manual Verification Required" list, which covers live-infrastructure gaps, not static wiring gaps. Root cause traces to `tasks.md`'s H1 task never allocating a `BuzzConfig` field for this in the first place, so H2 ("wire main.go... from BuzzConfig") had nothing to wire from.

**TDD guidance — Red:** Write a test in `cmd/boabot/main_test.go` that configures `BuzzConfig` (or whatever config surface the Green step adds) with a resolvable auth-tag secret and asserts `buildBuzzMonitor`'s constructed `RelayClient` actually presents a non-empty `auth` tag on its AUTH event — e.g. via a recording `dialFunc`/`fakeConn` seam (reusing the Phase D test harness) that captures the signed AUTH event `buildSignFn` produces and asserts a 4-element `["auth", ...]` tag is present when configured, and absent when not (the existing negative-control behavior, which must remain unchanged).

**TDD guidance — Green:** Add a `domain.SecretRef` name (e.g. `buzz_auth_tag`, mirroring `buzz_private_key`/`buzz_api_token`'s convention — `AuthTagSecretName` constant alongside `PrivateKeySecretName`/`APITokenSecretName`) resolving a serialized form of the tag (e.g. `owner_pubkey_hex|conditions|sig_hex`, or a small JSON envelope) through the existing `SecretStore` chain — no new provider needed. In `buildBuzzMonitor`, resolve it, parse it, call `buzzinfra.StaticAuthTagFunc(tag, pk.Hex())`, and append `buzzinfra.WithAuthTagFunc(fn)` to `opts` when present; log and continue without the tag (not fail closed) when absent, matching `LoadAPIToken`'s existing "optional" pattern, since a bot that only needs to act as an explicitly-enrolled member legitimately has no tag to configure.

**TDD guidance — Refactor:** Document the new secret's format in `user-docs/Buzz-Adoption-Config.md` alongside the existing `buzz_private_key`/`buzz_api_token` table, and record the resolution in an ADR entry or `implementation-notes.md` addendum so this doesn't quietly regress again. Consider whether `boabotctl secret set` needs a `--format` hint for this multi-part value, or whether asking the operator to paste an external attestation tool's raw tag output is sufficient — a product decision, not purely mechanical.

Acceptance Criteria:
- [ ] A `boabot` process configured with a resolvable auth-tag secret presents a valid, `ValidateAuthTag`-passing `auth` tag on its NIP-42 AUTH event.
- [ ] A `boabot` process with no such secret configured behaves exactly as today (no tag, no error) — the existing negative-control test continues to pass unmodified.
- [ ] `grep -n "WithAuthTagFunc\|StaticAuthTagFunc" cmd/boabot/main.go` returns at least one match.
- [ ] `user-docs/Buzz-Adoption-Config.md` documents how an operator provisions this secret from a tag produced by an external attestation-issuance tool (per OQ-2/OQ-5's already-accepted scoping — issuance tooling itself remains out of scope; only *consumption* is being fixed here).

---

### FR-002 (P0): Concurrent `Subscribe` + reconnect `resubscribeAll` can double-attach a subscription, risking a process-crashing panic

**Finding:** `RelayClient.Subscribe` (`relay_client.go:478-528`) registers a new `subEntry` into `rc.subs[id]` *before* calling its own `attachSub` on the connection it snapshotted moments earlier (`conn := rc.conn`, read and released under `rc.mu` with no lock held across the subsequent `attachSub` call). `reconnect.go`'s `resubscribeAll` (called after every successful reconnect) independently snapshots `rc.subs` and calls `attachSub` for **every** entry currently registered — including one a concurrent `Subscribe` call has just added but has not yet attached itself.

If a reconnect completes (`rc.conn` swapped, `resubscribeAll` invoked) in the narrow window between `Subscribe` registering its new entry and `Subscribe` calling its own `attachSub`, both call paths call `attachSub(ctx, conn, entry)` for the *same* `entry` — one with the old (dying) connection, one with the new one. `attachSub` (`relay_client.go:540-558`) only guards against attaching an entry that has been *removed*; it has no guard against attaching an entry that is already attached. Both calls pass the membership check, both call `conn.Subscribe(...)`, both overwrite `entry.pumpDone` (the struct's own doc comment concedes this: `pumpDone chan struct{} // set by the most recent attachSub` — a single slot, not generation-tracked — so the second write silently discards the first attach's completion signal), and both start a `pumpSub` goroutine (`relay_client.go:556,560-578`) forwarding onto the *same* `entry.out` channel.

When the subscription is later torn down (`removeAndClose`, `relay_client.go:587-602`), it waits only on `entry.pumpDone` — which now points to the *second* attach's completion channel — and then closes `entry.out`. The *first* attach's `pumpSub` goroutine has no one waiting on its own (orphaned) `pumpDone` and can still be running; its send (`case out <- FromLibraryEvent(evt):`) is guarded only by `rc.closedCh` (closed on full `RelayClient.Close()`, not on this one subscription's teardown), so if that orphaned pump receives an event after `entry.out` has already been closed by `removeAndClose`, it sends on a closed channel — an unrecovered panic that terminates the entire `boabot` process (every bot in it; nothing in `pumpSub` or its callers uses `recover()`).

This requires a specific but realistic timing window: a new channel subscription (e.g. discovery finding a new membership, `discovery.go`'s `subscribeToChannel`) racing a reconnect (a routine network blip) for that same, still-being-attached subscription. No existing test (`reconnect_test.go`, `relay_client_test.go`) exercises concurrent `Subscribe`-during-reconnect timing — the existing tests drive `fakeConn` deterministically and sequentially, so `-race` has never had a chance to observe this interleaving.

**TDD guidance — Red:** Write a test that starts a `RelayClient` against a `fakeConn` allowing controlled timing, then, from two goroutines, (a) calls `Subscribe` for a new filter and (b) simultaneously forces a reconnect (fire `conn.Done()` then let `reconnect()` succeed) such that `resubscribeAll` is highly likely to observe the entry before `Subscribe`'s own `attachSub` call runs — using a synchronization point (e.g. a test hook or a channel-based rendezvous injected via the existing `WithDial`/`WithSleep` seams) to make the race deterministic rather than relying on scheduler luck. Assert either that only one `pumpSub` is ever started per entry, or (functionally) that no more than one copy of any event is delivered on `entry.out` and that closing the subscription never panics.

**TDD guidance — Green:** Give each `subEntry` an attach-generation counter (or a per-entry mutex held across "check membership → call `conn.Subscribe` → register pump") so `attachSub` refuses (or supersedes and tears down) a stale/duplicate attach rather than allowing two live pumps to coexist. A minimal fix: have `attachSub` compare-and-swap a generation number on `entry`, have any pump that discovers it is no longer the current generation exit without sending, and have `removeAndClose` wait on *all* generations ever started for that entry (e.g. a small per-entry `sync.WaitGroup` rather than a single `pumpDone` channel).

**TDD guidance — Refactor:** Consider whether `Subscribe` should hold `subMu` continuously from registration through its own `attachSub` call (closing the window entirely) rather than fixing symptoms downstream — likely the smaller, more auditable fix, provided it doesn't reintroduce a "network call under lock" liveness concern.

Acceptance Criteria:
- [ ] The red test above fails on current `HEAD` and passes after the fix.
- [ ] `go test -race -gcflags=all=-d=checkptr=0 ./internal/infrastructure/buzz/...` passes, including the new test, with no `-race` warnings and no panics under repeated (`-count=20` or similar) runs.
- [ ] `removeAndClose` never closes `entry.out` while any pump for that entry could still be sending on it, verified structurally (not just by the absence of an observed panic in one test run).

---

### FR-003 (P1): `reconnect()` can attach a new subscription pump after `Close()`'s `pumpWG.Wait()` has already returned

**Finding:** A second, distinct race in the same subscription-lifecycle machinery as FR-002, with a different trigger. `reconnect()` (`reconnect.go:145-154`) checks `rc.closed` under `rc.mu`, sets `rc.conn = conn`, unlocks, and only then — **outside the lock** — calls `resubscribeAll(conn)` → `attachSub` → `rc.pumpWG.Add(1)` plus a new `pumpSub` goroutine. `attachSub` gates only on the entry's presence in `rc.subs` (`subMu`), never on `rc.closed`.

Meanwhile `Close()` (`relay_client.go:606-633`) sets `closed = true`, closes `closedCh`, closes the old connection, and calls `rc.pumpWG.Wait()` (line 623) — which can return as soon as the live pump count hits zero — **before** it acquires `subMu` to close every entry's `out` channel (lines 625-630). If `Close()` runs in the gap between `reconnect()`'s unlock and its `resubscribeAll` call, `pumpWG.Wait()` can return 0 first; `attachSub`'s subsequent `pumpWG.Add(1)` then starts a brand-new pump *after* the WaitGroup already believed all work was done — a documented misuse pattern for `sync.WaitGroup` ("new `Add` calls must happen after all previous `Wait` calls have returned") — and if that new pump's `select` happens to choose the `evt, ok := <-inner` case over the (already-closed, always-ready) `<-rc.closedCh` case at the moment it tries to forward an event, it sends on a channel `Close()` is about to (or has just) closed, producing the same class of unrecovered `panic: send on closed channel` as FR-002.

No test exercises `Close()` racing a concurrently-completing `reconnect()` — the fake-relay test harness drives these sequentially/deterministically, so this interleaving has never been observed by CI.

**TDD guidance — Red:** Write a test that triggers a reconnect (fires `conn.Done()`, lets `reconnect()` reach the point immediately after `rc.conn = conn; rc.mu.Unlock()`) and, in that window (via an injected synchronization hook), calls `Close()` concurrently. Assert `Close()` returns without panicking and that no goroutine leak results (e.g. via `goleak` or a bounded wait on a test-visible pump-count signal).

**TDD guidance — Green:** Either (a) have `reconnect()` hold `rc.mu` (or a dedicated "reconnecting" flag checked by `Close()`) across the `resubscribeAll` call so `Close()` cannot proceed until reconnection's re-attach step is fully done, or (b) have `attachSub` re-check "closed" under the same critical section it uses to check `rc.subs[entry.id]` (`subMu`), refusing to attach (and not calling `pumpWG.Add`) once closed — closing the same class of gap FR-002's fix addresses. **Lock-order hazard to avoid:** option (a) nests `subMu` inside `rc.mu`; option (b), read naively (calling `rc.mu.Lock()` for the closed-check from inside `attachSub`'s existing `subMu`-held section), nests the opposite way — combining both as written would deadlock. Make `closed` an `atomic.Bool` (rather than a plain `bool` guarded by `rc.mu`) so it can be read from either critical section without introducing any lock nesting at all; this is what makes "fix FR-002 and FR-003 with one shared mechanism" concretely safe rather than just directionally appealing.

**TDD guidance — Refactor:** If FR-002 and this finding are fixed together (recommended, since both stem from `attachSub` lacking a validity/generation check), a single shared fix and a single combined test suite covering both interleavings is preferable to two independent patches.

Acceptance Criteria:
- [ ] The red test fails on current `HEAD` and passes after the fix.
- [ ] `Close()` racing a concurrent `reconnect()` never panics and never leaks a pump goroutine, verified under `-race -gcflags=all=-d=checkptr=0` with repeated runs.

---

### FR-004 (P2): Process-singleton lock's stale-lock reclaim has a narrow TOCTOU between a slow writer and a fast reader

**Finding:** `AcquireLock` (`lock.go:82-120`) creates the lock file with `O_CREATE|O_EXCL|O_WRONLY` and then, as a **separate** step, writes the PID and closes the file (`lock.go:84-92`). If acquirer A has just created the file but not yet written its PID when acquirer B hits `EEXIST` and calls `readLockPID`, B reads an empty file — which `readLockPID` deliberately (and correctly, for the crash-mid-write case this design targets) treats as PID 0, i.e. stale — concludes the lock is abandoned, removes A's file, and creates its own. A then finishes its own write/close on its now-detached file descriptor and returns a `*Lock` believing it holds the lock. Both A and B now believe they hold the FR-031/OQ-1 singleton lock simultaneously.

The window is microseconds and this is a local, single-host, non-adversarial mechanism (guarding against operator double-start or a botched upgrade, not hostile contention), so real-world impact is low — but it is a genuine TOCTOU that the "empty file = pid 0 = stale" heuristic (added specifically to fix the SIGKILL-mid-write crash case) reintroduces for this different, narrower scenario. It is not covered by the "known limitation" the code's own doc comments call out (PID reuse by an unrelated process), which is a different failure mode.

**TDD guidance — Red:** Write a test with two goroutines racing `AcquireLock` against the same path, where the first is deliberately slowed between its `OpenFile` and `WriteString` calls (via an injectable seam or a temporarily-patched file open), and assert that at most one of the two returns a non-error `*Lock`.

**TDD guidance — Green:** There is no POSIX "create with content" primitive (`open(O_CREATE|O_EXCL)` and `write()` are always two syscalls), so the fix is to make the file visible under its final name only after it is fully written, not to make the write itself atomic. Write the PID to a temp file in the *same directory* as `path` (same-directory matters so the subsequent link is on the same filesystem), then use `os.Link(tmpPath, path)` — `link(2)` fails with `EEXIST` if `path` already exists and succeeds atomically with the content already present, so there is no window where a partially-written file is visible under the lock's real name — then remove the temp file. A reader hitting `EEXIST` on the `Link` call now always sees either a fully-written lock file or none at all.

**TDD guidance — Refactor:** Once the write is atomic, consider whether the empty-file-is-pid-0 special case in `readLockPID` can be simplified or its doc comment tightened to state precisely which crash window it's defending against, now that the write-in-progress window is closed by construction rather than merely "unlikely."

Acceptance Criteria:
- [ ] `AcquireLock` writes the lock file's content in a single atomic operation (no separate create-then-write step observable by a concurrent reader).
- [ ] A concurrency test asserts at most one of two racing `AcquireLock` calls against the same path succeeds, including under an artificially slowed writer.

---

### FR-005 (P2): No application-level bound on inbound Nostr event content/tag size

**Finding:** `translate.go`'s `FromLibraryEvent`/`ToLibraryEvent`, and every downstream consumer (`discovery.go`, `monitor.go`'s `handleChannelEvent`/`dispatch`), impose no length limit on `evt.Content` or the number/size of `evt.Tags`. Once BaoBot is authenticated into a workspace, any other member can publish a `kind:9` event with arbitrarily large content, which — if it passes the trigger/author gate — becomes a `TaskPayload.Instruction` with no cap. This currently relies entirely on the relay's own message-size limits and `fiatjaf.com/nostr`'s WebSocket transport limits, neither of which is verified anywhere in this codebase (`grep -rn "len(evt.Content)\|MaxContent\|maxContentLen\|LimitReader\|MaxBytesReader" internal/infrastructure/buzz/*.go` returns nothing). This is defense-in-depth, not a demonstrated live exploit — the realistic impact is uncontrolled token/cost spend on a single oversized instruction rather than memory exhaustion, given typical WS frame caps — but it is worth an explicit, deliberate bound rather than an implicit dependency on library/relay behavior nobody has confirmed.

**TDD guidance — Red:** Write a `Monitor.handleChannelEvent`/`dispatch` test with a multi-megabyte `evt.Content` and assert it is rejected (logged, not dispatched) rather than silently forwarded as a task instruction.

**TDD guidance — Green:** Add a `maxContentLen` (and, if practical, a max-tag-count) check early in `dispatch` (and/or `discovery.go`'s metadata handling), rejecting oversized content with a structured log line, choosing a bound generous enough for legitimate multi-paragraph messages.

**TDD guidance — Refactor:** Consider whether the bound belongs in `Monitor.Config` (operator-tunable) or as a package constant; document the choice.

Acceptance Criteria:
- [ ] An inbound `kind:9` event exceeding the configured content bound is rejected before becoming a `TaskPayload`, with a structured log line naming the event ID and size.
- [ ] Ordinary-sized messages are unaffected (existing dispatch tests continue to pass unmodified).

---

### FR-006 (P2): NIP-OA validation logic is not exercised against any attacker-controlled network data — clarify the threat model

**Finding:** `ValidateAuthTag`/`FindAuthTag` (`nipoa.go`) are genuinely well-written — Schnorr verification is correctly ordered (hash recomputed from the tag's own bytes, then verified, never trusting cached state), `owner == agent` is checked with `strings.EqualFold` (mixed-case hex can't bypass it), the condition-clause regex is anchored and RE2-based (no ReDoS), and integer parsing uses `strconv.ParseUint`'s own range checking. However, tracing every call site shows the only non-test caller is `StaticAuthTagFunc`, fed a **locally-configured** tag at construction time (and, per FR-001 above, not even wired into production yet) — nothing in this codebase calls this validation logic against an `auth` tag observed on an inbound, attacker-controlled event. Membership/attestation enforcement for *other* parties' identities is the relay's job server-side under the NIP-AA model, not BaoBot's. This is not a defect, but the docs should say so explicitly, since a future reader (or a future feature, e.g. "verify who attested this peer") could otherwise assume this validation path already sits on BaoBot's own attacker-facing surface when it currently does not.

**TDD guidance:** No code change required. Add a short note to `architecture.md` or `nipoa.go`'s package doc stating explicitly that `ValidateAuthTag`/`FindAuthTag` currently validate only BaoBot's own configured outbound tag (via `StaticAuthTagFunc`), and are not invoked against any inbound/attacker-supplied data — so a future change that *does* start parsing peer-supplied auth tags knows it is entering new threat-model territory.

Acceptance Criteria:
- [ ] `nipoa.go` or `architecture.md` states which call sites `ValidateAuthTag`/`FindAuthTag` currently have, and that none of them process attacker-controlled data today.

---

### FR-007 (P2): `WithBackoff`/`WithAuthRetryInterval` are built but never wired from `BuzzConfig`

**Finding:** `WithBackoff(BackoffConfig{Base, Max})` and `WithAuthRetryInterval` (`relay_client.go:204,209`) exist as tested `Option`s but are never called from `main.go`'s `NewRelayClient(bc.RelayURL, sk, opts...)` call — `BuzzConfig` has no corresponding fields, so production always uses the hardcoded defaults (1s/30s backoff, 200ms auth retry). This is minor: no FR in the PRD names reconnect backoff as operator-configurable, and the defaults are reasonable per FR-012's "bounded exponential backoff and jitter" requirement, which is satisfied regardless. Flagged for completeness, as one instance of the class of question this review was specifically asked to check ("does Phase H's wiring call every Option Phases D–G exposed, or are some silently unused").

**TDD guidance:** Only if an operator need is identified — add `BackoffBase`/`BackoffMax` (as `config.Duration`) to `BuzzConfig`, thread them through `buildBuzzMonitor` into `WithBackoff`. Not required to merge; genuinely optional.

Acceptance Criteria:
- [ ] Either `BuzzConfig` gains backoff fields wired to `WithBackoff`, or this is explicitly recorded as an intentional, permanent default (a one-line doc comment on `BuzzConfig` suffices) so a future reader doesn't rediscover the same question.

---

### FR-008 (P2): `kinds.go` named in `spec.md` was never created; `FR-027`'s scope description overstates what was built

**Finding:** Two small, related documentation-accuracy gaps, neither a functional defect:

1. `spec.md` §Scope of Changes lists a planned new file `internal/infrastructure/buzz/kinds.go` ("event-kind constants and per-kind handling"). It was never created — kind constants ended up in `monitor.go`, with a separate, disconnected `reactionKind = 7` in `guard.go`. Functionally harmless (all constants correct and consistently used) except `trigger.go:28`'s `if evt.Kind != 9` uses a bare literal instead of referencing `kindChannelMessage` (defined in `monitor.go`) — a minor magic-number/consistency nit, not a bug.
2. `spec.md` §Timeline and `tasks.md` both describe FR-025/FR-026/FR-027 as pulled forward from PRD P1 into this run's scope, phrasing FR-027 as "reaction publishing and subscription" being implemented. FR-027's own literal PRD text ("any reaction subscription MUST be `{"kinds":[7],"#h":[...]}`... MUST NOT be kinds-only") is fully satisfied by `guard.go`'s validation logic. But no actual reaction *publish* function and no `Monitor` code that subscribes to or handles kind:7 reactions exists anywhere — only the defensive shape-guard around a subscription that is never actually made. This is already partially disclosed on `implementation-notes.md`'s manual-verification list ("no F1–F18 task has Monitor actually subscribe to reactions yet"), but the phasing language in `spec.md`/`tasks.md` reads more expansively than that, which could mislead a future reader into thinking reactions are usable today.

**TDD guidance:** No code change required for either. (1) Optionally replace the bare `9` in `trigger.go:28` with `kindChannelMessage` for consistency — trivial, no test needed beyond existing coverage. (2) Update `spec.md`/`tasks.md`'s FR-027 phasing language to match `implementation-notes.md`'s own more precise "guard only, no publish/consume" framing.

Acceptance Criteria:
- [ ] `trigger.go` references `kindChannelMessage` rather than a bare `9`.
- [ ] `spec.md`/`tasks.md`'s FR-027 description matches what was actually built (subscription-shape guard only), or `kinds.go`'s planned existence is removed from `spec.md` if the constants-in-`monitor.go` layout is being kept.

---

## Implementation Guidance

| Priority | FRs | Rationale |
|---|---|---|
| P0 (block PR) | FR-001, FR-002 | FR-001: the PRD's flagship capability (NIP-AA virtual membership via owner attestation) does not work in the shipped binary — a scope-defeating wiring gap, not a live-infrastructure limitation, and not on the disclosed manual-verification list. FR-002: a realistic, previously-undiscovered concurrency race with a credible path to a full-process-crashing panic. |
| P1 (must fix) | FR-003 | A second, distinct concurrency race in the same subscription-lifecycle code as FR-002, with a different trigger (reconnect vs. `Close()` rather than `Subscribe` vs. reconnect) — recommended to fix alongside FR-002 with one shared mechanism. |
| P2 (should fix) | FR-004 – FR-008 | A narrow, low-impact local TOCTOU; defense-in-depth input validation; threat-model documentation; one small unwired-but-harmless `Option`; and two documentation-accuracy nits. None block merge on their own. |

P0 findings must be resolved before this PR is merged. P1 is strongly recommended in the same PR since it shares root cause and fix shape with FR-002. P2 findings may be deferred to a follow-up if agreed with the team.

---

## What Was Checked and Found Clean (not re-litigated above)

Documented here per this review's own standard of showing its work, not as findings requiring action. This review ran three independent investigative passes (security/cryptography, concurrency, wiring/composition) plus direct verification by the primary reviewer, and cross-checked each pass's claims against the code rather than against `implementation-notes.md`'s narrative.

- **Secret handling** (`internal/infrastructure/secret/`, `boabotctl/internal/commands/secret.go`): no secret value reaches a log line, a returned error string, or a subprocess argument list anywhere in the reviewed code — verified by reading every provider's `Lookup`/`Set`/`Delete` and every `logger.`/`slog.` call site directly. `keystore.Provider.Set` places the secret only in `zalando/go-keyring`'s `password` parameter (never `service`/`user`, the two arguments that map to darwin's `-s`/`-a` argv flags), confirmed both by reading the code and by `TestProvider_Set_ConstructsCallWithSecretOnlyInPasswordSlot`. `boabotctl secret set` reads only via masked stdin, never a flag/argument; `get` reports presence only, never the value. `LoadKeypair`/`parseSecretKey` (`keypair.go`) never wrap an underlying library error that could embed the raw nsec — confirmed by reading the code and `TestLoadKeypair_NeverLogsSecret`.
- **`.github/workflows/boabot.yml`'s `checkptr=0` flag**, claimed fixed by a prior self-audit, was independently re-confirmed present on both the `Test` and `Coverage check` `go test -race` invocations.
- **Clean Architecture boundaries**: `grep -rn "fiatjaf.com/nostr\|zalando/go-keyring\|godbus\|wincred" internal/domain/ internal/application/` returns nothing (re-run independently, not just quoted from the notes). `internal/domain/buzz.go` and `secret.go` import only `context`. `internal/application/team/team_manager.go`'s refactor (`WithChannelMonitor`) actually *removed* a pre-existing `infrastructure/slack` import from the application layer rather than adding a new violation, and both Slack and Buzz wiring in `main.go` exercise the generalized seam.
- **`guard.go`'s p-gate/h-gate subscription filter** is enforced unconditionally inside `RelayClient.Subscribe` itself (not a bypassable caller-side helper), checked against every kind in a multi-kind filter (not just `kinds[0]`), confirmed by reading the call site directly.
- **Tag-indexing safety** against attacker-supplied Nostr tags: every site found (`trigger.go`, `discovery.go`, `nipoa.go`) checks tag length before indexing; no panic-on-malformed-tag path was found. `translate.go`'s outbound `ToLibraryEvent`/`ToLibraryFilter` bound kind values to `[0, 65535]`.
- **`Publish`/`Authenticate`/`Subscribe`** read `rc.conn` once into a local under `rc.mu` before use — no torn/reread access across a concurrent reconnect's connection swap (distinct from the two subscription-attach races reported as FR-002/FR-003).
- **`Monitor`'s pending-task map and typing-indicator shutdown** (`HandleResult`, `typingLoop`): the map pop and `typingDone` channel close happen atomically under one `m.mu` critical section; `typingLoop` only ever reads `done`, never closes it — no double-close risk found.
- **`BuzzConfig` field-to-wiring cross-reference**: `Enabled`, `RelayURL`, `BotName`, `OwnerPubkey`, `RespondTo`, `RespondToAllowlist`, `PresenceInterval` are all correctly threaded through `buildBuzzMonitor` into `buzzinfra.Config`; `LockDir` is correctly set to the shared `memoryRoot`, not a per-bot directory. No dead `BuzzConfig` fields beyond the already-documented, deliberate omission of `Channels`.
- **TODO-shaped gaps**: a grep for TODO/FIXME/XXX/"not implemented"/"unimplemented" across `buzz/`, `secret/`, `domain/buzz.go`, `domain/secret.go`, `main.go`, `config.go`, and `boabotctl`'s `secret.go` (excluding tests) returns zero hits.
- **Coverage and toolchain claims**: independently re-ran `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run` (both modules, zero issues each), and `go test -race -gcflags=all=-d=checkptr=0 ./...` (all packages pass). Re-ran the coverage computation exactly as CI does it: domain+application aggregate is 91.0%, `internal/application/team` sits at 77.8% (confirmed pre-existing, unrelated to this feature) — both figures match `implementation-notes.md`'s claims exactly. All three PRD grep-based acceptance criteria (`fiatjaf.com/nostr` absent from domain/application; `infrastructure/slack|infrastructure/buzz` absent from application; `go-keyring|godbus` absent from domain/application) independently re-run and pass.

---

## Reviewer Methodology

This review combined direct reading of the diff (`ad7a679..HEAD`, ~14,200 lines) and full local toolchain runs by the primary reviewer with three parallel, independently-instructed investigative passes (security/cryptography focus, concurrency focus, wiring/composition focus), each of which was told to verify claims in `implementation-notes.md` against the code rather than trust them. Findings were cross-checked against each other and, where two passes independently reached the same conclusion (as with FR-001, found independently by the primary reviewer, the security pass, and the wiring pass), that agreement is noted in the finding text. No source code, test, or configuration file was modified as part of this review — findings are documentation only, per this repository's review-code process.
