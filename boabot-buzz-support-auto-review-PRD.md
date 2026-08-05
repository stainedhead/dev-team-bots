# PRD: Code Review Fixes — BaoBot Buzz Support

**Created:** 2026-08-04
**Status:** Draft
**Branch:** `worktree-buzz-support-prd`
**Reviewed range:** `ad7a679..HEAD` (`boabot-buzz-support-PRD.md` FR-001–FR-054; ~14,200 lines across 92 files)

---

## Executive Summary

This feature (BaoBot joining Block's Nostr-based "Buzz" workspace, plus an OS-keystore secret-storage subsystem) was built across nine incremental phases with an unusually high degree of self-documented rigor: `implementation-notes.md` records numerous advisor-caught fixes before commit, and several of the specific traps this review was asked to hunt for (the nil-vs-empty-allowlist lockout, the file-provider fallback that would have collapsed two bots' secrets onto one key, the `checkptr` CI flag) turned out to already be fixed and were independently re-verified here, not just taken on faith. The secret-storage subsystem in particular is clean: every claim about secret handling (no logging, no subprocess-argv leakage, timeout isolation per provider) was independently verified by reading the code and the tests that back it, not by trusting the notes.

The gap between phases is where this review found its two most significant findings, both structural rather than cosmetic. First, **the feature's headline capability — a NIP-OA/NIP-AA owner-attested agent joining a channel without being explicitly enrolled — cannot work in the shipped binary**, because the extension point that carries the owner-issued attestation tag (`buzz.WithAuthTagFunc`) is fully built and unit-tested in Phase E but is never called from `cmd/boabot/main.go`'s production wiring, and no config/secret field exists to source a tag from. This is not one of the already-disclosed "requires live infrastructure" gaps — it is a missing wiring step that no phase caught because each phase's own tests pass in isolation. Second, **a genuine, previously-undiscovered concurrency race** in `RelayClient.Subscribe` vs. the reconnect path's `resubscribeAll` can start two concurrent goroutines pumping onto the same subscription channel, with a realistic path to an unrecovered `panic: send on closed channel` that would crash the entire `boabot` process (all bots, not just Buzz) — not something `-race` would have caught given how the existing tests drive `fakeConn` deterministically rather than under real timing pressure.

Two smaller, P2-level input-validation/documentation gaps round out the findings. Overall: the implementation is well-engineered and the team's own self-review process caught a lot, but the two P0s below are real, and both are exactly the kind of cross-phase seam defect that a single-pass review across 9 phases' worth of work should be expected to surface.

---

## Functional Requirements

---

### FR-001 (P0): NIP-OA owner-attestation tag is never wired into production — NIP-AA virtual membership cannot work

**Finding:** Phase E (`internal/infrastructure/buzz/nipoa.go`) built and thoroughly tested `SignAuthTag`/`ValidateAuthTag`/`StaticAuthTagFunc`, and Phase D (`relay_client.go`) built the `WithAuthTagFunc`/`AuthTagFunc` extension point these compose with — `buildSignFn` appends the configured tag to the AUTH event before signing, exactly as NIP-OA requires. `phase_e_wiring_test.go` proves the mechanism composes correctly end to end. **None of this is ever invoked from `cmd/boabot/main.go`.** `buildBuzzMonitor`'s `opts` slice (`main.go:193-205`) only ever appends `WithLogger`, `WithProfile`, and conditionally `WithAPIToken` — `buzzinfra.WithAuthTagFunc` and `buzzinfra.StaticAuthTagFunc` are never referenced anywhere in `main.go`. There is no corresponding gap anywhere else either: `config.BuzzConfig` (`internal/infrastructure/config/config.go:80-92`) has no field for a pre-issued auth tag or its `conditions` string, and no `domain.SecretRef` name for one exists alongside `buzz_private_key`/`buzz_api_token` (`keypair.go`, `token.go`). `owner_pubkey` in `BuzzConfig` is unrelated — it only feeds `Monitor.Config.OwnerPubkeyHex`, consulted solely by the `!shutdown` gate (`authorGate.allowsShutdown`), not by anything in the AUTH path.

The result: every `boabot` deployment built from this branch authenticates to Buzz as a bare pubkey with no owner attestation. Per the PRD, this means the agent can only participate as an explicitly-enrolled member of every channel — the entire "join via owner attestation without being explicitly enrolled" workflow the PRD names as this feature's differentiator (and that task E3's own acceptance criterion states verbatim: *"NIP-OA `auth` tag issued by an owner key grants the agent relay access via NIP-AA without the agent being explicitly enrolled"*) is unreachable in the code as shipped. This was not caught by any phase's own tests because each phase's tests exercise the mechanism in isolation (Phase E's own tests, `phase_e_wiring_test.go`) rather than checking that Phase H's wiring actually reaches it — and it is not on `implementation-notes.md`'s "Manual Verification Required" list, which covers live-infrastructure gaps, not wiring gaps reachable by static inspection alone.

Root cause: `tasks.md`'s own H1 task (`BuzzConfig` field list) never allocated a field for this, so H2 ("wire main.go... from BuzzConfig") had nothing to wire from — the gap traces back to task planning, not just phase-H execution.

**TDD guidance — Red:** Write a test in `cmd/boabot/main_test.go` that configures `BuzzConfig` with a resolvable auth-tag secret (or equivalent config surface, per the Green step below) and asserts `buildBuzzMonitor`'s constructed `RelayClient` actually presents a non-empty `auth` tag on its AUTH event — e.g. via a recording `dialFunc`/`fakeConn` seam (reusing the Phase D test harness) that captures the signed event `buildSignFn` produces and asserts a 4-element `["auth", ...]` tag is present when configured, and *absent* when not configured (the existing negative-control behavior, which should remain unchanged).

**TDD guidance — Green:** Add a `domain.SecretRef` name (e.g. `buzz_auth_tag`, mirroring `buzz_private_key`/`buzz_api_token`'s convention) resolving a serialized form of the tag (e.g. `owner_pubkey_hex|conditions|sig_hex`, or a small JSON envelope) through the existing `SecretStore` chain — no new provider needed. In `buildBuzzMonitor`, resolve it, parse it, call `buzzinfra.StaticAuthTagFunc(tag, pk.Hex())`, and append `buzzinfra.WithAuthTagFunc(fn)` to `opts` when present; log and continue without the tag (not fail closed) when absent, matching `LoadAPIToken`'s existing "optional" pattern, since a bot that only needs to act as an explicitly-enrolled member legitimately has no tag to configure.

**TDD guidance — Refactor:** Document the new secret's format in `user-docs/Buzz-Adoption-Config.md` alongside the existing `buzz_private_key`/`buzz_api_token` table, and record the resolution in an ADR entry or `implementation-notes.md` addendum so this doesn't quietly regress again. Consider whether `boabotctl secret set` needs a dedicated `--format` hint for this multi-part value, or whether asking the operator to paste `buzz-admin`'s raw tag output is sufficient — this is a product decision, not purely mechanical.

Acceptance Criteria:
- [ ] A `boabot` process configured with a resolvable auth-tag secret presents a valid, `ValidateAuthTag`-passing `auth` tag on its NIP-42 AUTH event.
- [ ] A `boabot` process with no such secret configured behaves exactly as today (no tag, no error) — the existing negative-control test continues to pass unmodified.
- [ ] `grep -n "WithAuthTagFunc\|StaticAuthTagFunc" cmd/boabot/main.go` returns at least one match.
- [ ] `user-docs/Buzz-Adoption-Config.md` documents how an operator provisions this secret from a tag produced by an external attestation-issuance tool (per OQ-2/OQ-5's already-accepted scoping — issuance tooling itself remains out of scope, only *consumption* is being fixed here).

---

### FR-002 (P0): Concurrent `Subscribe` + reconnect `resubscribeAll` can double-attach a subscription, risking a process-crashing panic

**Finding:** `RelayClient.Subscribe` (`relay_client.go:478-528`) registers a new `subEntry` into `rc.subs[id]` *before* calling its own `attachSub` on the connection it snapshotted moments earlier (`conn := rc.conn`, read and released under `rc.mu` with no lock held across the subsequent `attachSub` call). `reconnect.go`'s `resubscribeAll` (called after every successful reconnect) independently snapshots `rc.subs` and calls `attachSub` for **every** entry currently registered — including one a concurrent `Subscribe` call has just added but has not yet attached itself.

If a reconnect completes (`rc.conn` swapped, `resubscribeAll` invoked) in the narrow window between `Subscribe` registering its new entry and `Subscribe` calling its own `attachSub`, both call paths call `attachSub(ctx, conn, entry)` for the *same* `entry` — one with the old (dying) connection, one with the new one. `attachSub` (`relay_client.go:540-558`) only guards against attaching an entry that has been *removed*; it has no guard against attaching an entry that is already attached. Both calls pass the membership check, both call `conn.Subscribe(...)`, both set `entry.pumpDone` (the second write silently overwrites the first, orphaning the first attach's completion signal), and both start a `pumpSub` goroutine (`relay_client.go:556,560-578`) forwarding onto the *same* `entry.out` channel.

When the subscription is later torn down (`removeAndClose`, `relay_client.go:587-602`), it waits only on `entry.pumpDone` — which now points to the *second* attach's completion channel — and then closes `entry.out`. The *first* attach's `pumpSub` goroutine has no one waiting on its own (orphaned) `pumpDone` and can still be in its `select` loop; `pumpSub`'s send (`case out <- FromLibraryEvent(evt):`) is guarded only by `rc.closedCh` (closed on full `RelayClient.Close()`, not on this one subscription's teardown), so if that orphaned pump receives an event from its own `inner` channel after `entry.out` has already been closed by `removeAndClose`, it sends on a closed channel — an unrecovered panic that terminates the entire `boabot` process (every bot in it, not just the Buzz monitor; nothing in `pumpSub` or its caller uses `recover()`).

This requires a specific but realistic timing window: a new channel subscription (e.g. discovery finding a new membership, `discovery.go`'s `subscribeToChannel`) racing against a reconnect (a routine network blip) for that same, still-being-attached subscription. No existing test (`reconnect_test.go`, `relay_client_test.go`) exercises concurrent `Subscribe`-during-reconnect timing — the existing tests drive `fakeConn` deterministically and sequentially, so `-race` has never had a chance to observe this interleaving.

**TDD guidance — Red:** Write a test that starts a `RelayClient` against a `fakeConn` allowing controlled timing, then, from two goroutines, (a) calls `Subscribe` for a new filter and (b) simultaneously forces a reconnect (fire `conn.Done()` then let `reconnect()` succeed) such that `resubscribeAll` is highly likely to observe the entry before `Subscribe`'s own `attachSub` call runs — using a synchronization point (e.g. a test hook or a channel-based rendezvous injected via the existing `WithDial`/`WithSleep` seams) to make the race deterministic rather than relying on scheduler luck. Assert either that only one `pumpSub` is ever started per entry, or (functionally) that no more than one copy of any event is delivered on `entry.out` and that closing the subscription never panics.

**TDD guidance — Green:** Give each `subEntry` an attach-generation counter (or a per-entry mutex held across "check membership → call `conn.Subscribe` → register pump") so `attachSub` refuses (or supersedes and tears down) a stale/duplicate attach rather than allowing two live pumps to coexist. A minimal fix: have `attachSub` compare-and-swap a generation number on `entry` and have any pump that discovers it's no longer the current generation exit without sending; `removeAndClose` should wait on *all* generations that were ever started for that entry, not just the last one (e.g. track outstanding pumps for the entry with a small per-entry `sync.WaitGroup` rather than a single `pumpDone` channel).

**TDD guidance — Refactor:** Consider whether `Subscribe` should hold `subMu` continuously from registration through its own `attachSub` call (closing the window entirely) rather than fixing symptoms downstream — this is likely the smaller, more auditable fix, provided it doesn't reintroduce the "network call under lock" liveness concern noted below.

Acceptance Criteria:
- [ ] The red test above fails on current `HEAD` and passes after the fix.
- [ ] `go test -race -gcflags=all=-d=checkptr=0 ./internal/infrastructure/buzz/...` passes, including the new test, with no `-race` warnings and no panics under repeated (`-count=20` or similar) runs.
- [ ] `removeAndClose` never closes `entry.out` while any pump for that entry could still be sending on it, verified structurally (not just by the absence of an observed panic in one test run).

---

### FR-003 (P2): No application-level bound on inbound Nostr event content/tag size

**Finding:** `translate.go`'s `FromLibraryEvent`/`ToLibraryEvent`, and every downstream consumer (`discovery.go`, `monitor.go`'s `handleChannelEvent`/`dispatch`), impose no length limit on `evt.Content` or the number/size of `evt.Tags`. Once BaoBot is authenticated into a workspace, any other member can publish a `kind:9` event with arbitrarily large content, which — if it passes the trigger/author gate — becomes a `TaskPayload.Instruction` with no cap. This currently relies entirely on the relay's own message-size limits and `fiatjaf.com/nostr`'s WebSocket transport limits, neither of which is verified anywhere in this codebase (`grep -rn "len(evt.Content)\|MaxContent\|maxContentLen\|LimitReader\|MaxBytesReader" internal/infrastructure/buzz/*.go` returns nothing). This is defense-in-depth, not a demonstrated live exploit — the realistic impact is uncontrolled token/cost spend on a single oversized instruction rather than memory exhaustion, given typical WS frame caps — but it is worth an explicit, deliberate bound rather than an implicit dependency on library/relay behavior nobody has confirmed.

**TDD guidance — Red:** Write a `Monitor.handleChannelEvent`/`dispatch` test with a multi-megabyte `evt.Content` and assert it is rejected (logged, not dispatched) rather than silently forwarded as a task instruction.

**TDD guidance — Green:** Add a `maxContentLen` (and, if practical, a max-tag-count) check early in `dispatch` (and/or `discovery.go`'s metadata handling), rejecting oversized content with a structured log line, choosing a bound generous enough for legitimate multi-paragraph messages (e.g. the same bound, if any, `screening.ScreenContentUseCase` already assumes for other untrusted-content paths).

**TDD guidance — Refactor:** Consider whether the bound belongs in `Monitor.Config` (operator-tunable) or as a package constant; document the choice.

Acceptance Criteria:
- [ ] An inbound `kind:9` event exceeding the configured content bound is rejected before becoming a `TaskPayload`, with a structured log line naming the event ID and size.
- [ ] Ordinary-sized messages are unaffected (existing dispatch tests continue to pass unmodified).

---

### FR-004 (P2): NIP-OA validation logic is not exercised against any attacker-controlled network data — clarify the threat model

**Finding:** `ValidateAuthTag`/`FindAuthTag` (`nipoa.go`) are genuinely well-written — Schnorr verification is correctly ordered (hash recomputed from the tag's own bytes, then verified, never trusting cached state), `owner == agent` is checked with `strings.EqualFold` (so mixed-case hex can't bypass it), the condition-clause regex is anchored and RE2-based (no ReDoS), and integer parsing uses `strconv.ParseUint`'s own range checking rather than anything overflow-prone. However, tracing every call site (`grep -rn "ValidateAuthTag\|FindAuthTag" internal/infrastructure/buzz/*.go`) shows the only non-test caller is `StaticAuthTagFunc`, which is fed a **locally-configured** tag at construction time (and, per FR-001 above, isn't even wired into production yet) — nothing in this codebase calls this validation logic against an `auth` tag observed on an inbound, attacker-controlled event. Membership/attestation enforcement for *other* parties' identities is the relay's job server-side under the NIP-AA model, not BaoBot's. This isn't a defect, but the PRD/architecture docs should say so explicitly, since a future reader (or a future feature, e.g. a "verify who attested this peer" capability) could otherwise assume this validation path is already part of BaoBot's own attacker-facing surface when it currently is not.

**TDD guidance:** No code change required. Add a short note to `architecture.md` or `nipoa.go`'s package doc stating explicitly that `ValidateAuthTag`/`FindAuthTag` currently validate only BaoBot's own configured outbound tag (at startup, via `StaticAuthTagFunc`), and are not invoked against any inbound/attacker-supplied data — so a future change that *does* start parsing peer-supplied auth tags knows it is entering new threat-model territory, not exercising an already-hardened path.

Acceptance Criteria:
- [ ] `nipoa.go` or `architecture.md` states which call sites `ValidateAuthTag`/`FindAuthTag` currently have, and that none of them process attacker-controlled data today.

---

## Implementation Guidance

| Priority | FRs | Rationale |
|---|---|---|
| P0 (block PR) | FR-001, FR-002 | FR-001: the PRD's flagship capability (NIP-AA virtual membership via owner attestation) does not work in the shipped binary — a scope-defeating wiring gap, not a live-infrastructure limitation. FR-002: a realistic, previously-undiscovered concurrency race with a credible path to a full-process-crashing panic. |
| P2 (should fix) | FR-003, FR-004 | Defense-in-depth input validation and threat-model documentation; neither is a demonstrated live exploit today. |

P0 findings must be resolved before this PR is merged. P2 findings may be deferred to a follow-up if agreed with the team.

---

## What Was Checked and Found Clean (not re-litigated above)

Documented here per this review's own standard of showing its work, not as findings requiring action:

- **Secret handling** (`internal/infrastructure/secret/`, `boabotctl/internal/commands/secret.go`): no secret value reaches a log line, a returned error string, or a subprocess argument list anywhere in the reviewed code — verified by reading every provider's `Lookup`/`Set`/`Delete` and every `logger.`/`slog.` call site directly, not by trusting `implementation-notes.md`'s claims. `keystore.Provider.Set` places the secret only in `zalando/go-keyring`'s `password` parameter (never `service`/`user`, the two arguments that map to darwin's `-s`/`-a` argv flags), confirmed both by reading the code and by `TestProvider_Set_ConstructsCallWithSecretOnlyInPasswordSlot`. `boabotctl secret set` reads only via masked stdin, never a flag/argument; `get` reports presence only.
- **`.github/workflows/boabot.yml`'s `checkptr=0` flag**, claimed fixed by a prior self-audit, was independently re-confirmed present on both the `Test` and `Coverage check` `go test -race` invocations.
- **Clean Architecture boundaries**: `grep -rn "fiatjaf.com/nostr|zalando/go-keyring|godbus|wincred" internal/domain/ internal/application/` returns nothing. `internal/application/team/team_manager.go`'s refactor (`WithChannelMonitor`) actually *removed* a pre-existing `infrastructure/slack` import from the application layer rather than adding a new violation.
- **`guard.go`'s p-gate/h-gate subscription filter** is enforced unconditionally inside `RelayClient.Subscribe` itself (not a bypassable caller-side helper), checked against every kind in a multi-kind filter, confirmed by reading the call site directly.
- **Tag-indexing safety** against attacker-supplied Nostr tags: every site found (`trigger.go`, `discovery.go`, `nipoa.go`) checks tag length before indexing; no panic-on-malformed-tag path was found.
- **Coverage claims**: `internal/application/team` at 77.8% (implementation-notes.md's disclosed, pre-existing gap) and `internal/domain`/`internal/application` aggregate coverage were independently re-run and match the documented figures.
- `go build ./...` and the full `buzz`/`secret`/`domain` test suites (`-race -gcflags=all=-d=checkptr=0`) pass as of this review.
