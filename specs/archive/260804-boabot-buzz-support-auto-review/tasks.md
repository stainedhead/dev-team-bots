# Tasks: Code Review Fixes — BaoBot Buzz Support

**Feature:** boabot-buzz-support-auto-review
**Created:** 2026-08-04
**Status:** Ready for implementation (Step 9 of this dev-flow run executes directly against this file)

---

## Progress Summary

**0 / 18 tasks complete.** Derived from the review PRD's (`boabot-buzz-support-auto-review-PRD.md`) "Implementation Process" section, which proposes a 5-workstream partition (WS-A through WS-E) covering 7 of the review's 8 findings. **Correction found during this breakdown (verified against the PRD's own Functional Requirements section, not assumed from the workstream table alone): the PRD's workstream table omits FR-007 entirely** — WS-A covers FR-001, WS-B covers FR-002+FR-003, WS-C covers FR-004, WS-D covers FR-005, WS-E covers FR-006+FR-008, and nothing in the table names FR-007. FR-007 is a doc-comment-only fix (per its own "no code change required unless an operator need is identified" TDD guidance and OQ-R2's resolution in `research.md`) that touches `internal/infrastructure/config/config.go` — the same file WS-A already edits for FR-001's new `BuzzConfig` doc comment/secret-resolution path. **Assigned to WS-A (as WS-A4), not WS-E**, so no two workstreams touch `config.go` in the same window (file-collision affinity, the same reasoning the review PRD itself used to batch WS-E's doc-only findings). This is noted explicitly so Step 9 doesn't silently drop FR-007 by trusting the PRD's table at face value, and so `config.go` isn't independently edited by two parallel worktrees.

Task IDs use `WS-<letter><n>` to avoid colliding with both the review PRD's own `FR-00x` numbering and the original feature's `tasks.md` `A`–`I` phase-letter numbering.

**FR sweep:** every finding FR-001–FR-008 is the named acceptance-criterion owner of at least one task below — verified by extracting every `FR-00x` reference in this file's task table and diffing against the review PRD's full FR-001–FR-008 range.

---

## Workstream Partition (from the review PRD, corrected)

| Workstream | Findings | Primary files | Can run parallel with |
|---|---|---|---|
| WS-A | FR-001, **FR-007** (reassigned here — see Progress Summary) | `cmd/boabot/main.go`, `internal/infrastructure/config/config.go`, `internal/infrastructure/buzz/{keypair,token}.go`, `user-docs/Buzz-Adoption-Config.md` | WS-C, WS-D, WS-E |
| WS-B | FR-002 **and** FR-003 together — do not split | `internal/infrastructure/buzz/relay_client.go`, `reconnect.go` | WS-A, WS-C, WS-D, WS-E (WS-B5 sub-task waits on WS-A/C/D) |
| WS-C | FR-004 | `internal/infrastructure/*/lock.go` | WS-A, WS-B, WS-D, WS-E |
| WS-D | FR-005 | `internal/infrastructure/buzz/monitor.go`, possibly `discovery.go` | WS-A, WS-B, WS-C, WS-E |
| WS-E | FR-006, FR-008 | `nipoa.go`/`architecture.md`, `trigger.go`, `specs/archive/260804-boabot-buzz-support/spec.md`, `.../tasks.md` | WS-A, WS-B, WS-C, WS-D |

Each workstream runs in its own git worktree (`git worktree add ../<ws-name> worktree-buzz-support-prd`), fixes land as commits on this same branch. See `architecture.md` AD-2 for the doc-collision resolution: **WS-B5 collects all ADR/technical-details entries for WS-A/B/C/D in one pass, after WS-A, WS-C, and WS-D have landed** — not written concurrently by each workstream. WS-E's findings are doc-only/trivial and do not constitute the kind of "behavior change" `AGENTS.md`'s Documentation Requirements targets, so WS-E does not feed into WS-B5's collection pass.

---

## WS-A — FR-001 (P0) + FR-007 (P2, reassigned): NIP-OA auth-tag wiring and config.go doc comment

FR-007 batched into this workstream (not WS-E) solely because both touch `internal/infrastructure/config/config.go` — see Progress Summary. No shared root cause between FR-001 and FR-007; WS-A4 is independent of WS-A1–A3 and could run first or last within this workstream.

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| WS-A1 *(Red)* | Write a failing test in `cmd/boabot/main_test.go` using the Phase D test harness's recording `dialFunc`/`fakeConn` seam: configure `BuzzConfig`/secret surface (added in WS-A2) with a resolvable auth-tag secret, assert `buildBuzzMonitor`'s constructed `RelayClient` presents a non-empty, well-formed `["auth", ...]` tag on its signed AUTH event; assert the existing negative-control test (no tag configured ⇒ no tag present) still exists and still passes | — | 2h | Test compiles, fails against current `HEAD` (`buildBuzzMonitor` has no auth-tag path yet) |
| WS-A2 *(Green)* | Add `AuthTagSecretName` constant (`internal/infrastructure/buzz/token.go`, alongside `PrivateKeySecretName`/`APITokenSecretName`); resolve via existing `SecretStore` chain; parse the pipe-delimited `owner_pubkey_hex\|conditions\|sig_hex` format (per `research.md` OQ-R1 resolution, `data-dictionary.md`) — **before finalizing the parse, confirm the exact field order/encoding against `nipoa.go`'s actual `SignAuthTag`/`ValidateAuthTag` signatures**, since `data-dictionary.md`'s format is a spec-time proposal, not verified against the real function signatures; in `cmd/boabot/main.go`'s `buildBuzzMonitor`, call `buzzinfra.StaticAuthTagFunc(tag, pk.Hex())` and append `buzzinfra.WithAuthTagFunc(fn)` to `opts` when present; log-and-continue (not fail-closed) when absent, matching `LoadAPIToken`'s existing optional pattern | WS-A1 | 4h | WS-A1's test passes; `grep -n "WithAuthTagFunc\|StaticAuthTagFunc" cmd/boabot/main.go` returns at least one match; existing negative-control test passes unmodified; parsed format confirmed to round-trip through `SignAuthTag`/`ValidateAuthTag` in a test |
| WS-A3 *(Refactor)* | Document the new secret's pipe-delimited format in `user-docs/Buzz-Adoption-Config.md`'s existing `buzz_private_key`/`buzz_api_token` table; leave the ADR entry for WS-B5's collection pass (do not edit `docs/architectural-decision-record.md` here — see `architecture.md` AD-2) | WS-A2 | 1h | `user-docs/Buzz-Adoption-Config.md` documents how an operator provisions the auth-tag secret from an external attestation tool's raw output, per OQ-R1/OQ-2/OQ-5's scoping |
| WS-A4 | FR-007: confirm no operator need for tunable backoff has surfaced (per `research.md` OQ-R2 resolution); add a one-line doc comment on `BuzzConfig` (`internal/infrastructure/config/config.go`) recording the hardcoded-default-is-permanent decision. Independent of WS-A1–A3; sequence after them only to avoid a same-file merge inside this workstream's own worktree, not because of a real dependency | — | 1h | `BuzzConfig`'s doc comment records the decision so a future reader doesn't rediscover the same question |

## WS-B — FR-002 (P0) + FR-003 (P1): `attachSub` double-attach and post-Close attach races

**Must be fixed together, one workstream, one design decision** — both stem from `attachSub` lacking a validity/generation check (review PRD's explicit instruction; see `architecture.md` AD-1 for the resolved fix shape). **Commit-count exception:** `spec.md`'s NFR "one commit per FR, do not batch multiple findings into one commit" rule does not apply here — FR-002 and FR-003 land in **one** commit whose message names both (e.g. `fix(buzz): FR-002/FR-003 — guard attachSub against duplicate/stale attach`, the review PRD's own example), since splitting them would defeat the "one shared fix" instruction above.

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| WS-B1 *(Red)* | Write a deterministic-timing test (using an injected synchronization hook via the existing `WithDial`/`WithSleep` seams, not scheduler luck) that runs `Subscribe` and a forced reconnect (`conn.Done()` → `reconnect()`) concurrently, arranging `resubscribeAll` to observe the entry before `Subscribe`'s own `attachSub` call. Assert only one `pumpSub` is ever live per entry and closing the subscription never panics | — | 3h | Test compiles and fails (panics or double-delivers) against current `HEAD` |
| WS-B2 *(Red)* | Write a second deterministic-timing test: trigger a reconnect, pause it immediately after `rc.conn = conn; rc.mu.Unlock()` (via an injected hook), call `Close()` concurrently in that window. Assert `Close()` returns without panicking and no pump goroutine leaks (e.g. via `goleak` or a bounded wait on a test-visible pump-count signal) | — | 3h | Test compiles and fails (panics or leaks) against current `HEAD` |
| WS-B3 *(Green)* | Implement the AD-1 fix: add an attach-generation counter to `subEntry`, incremented by `attachSub` on each attach; a superseded-generation pump exits without sending; replace the single-slot `pumpDone` channel with a per-entry `sync.WaitGroup` (or equivalent) that `removeAndClose`/`Close` wait on, covering every generation ever started; change `RelayClient.closed` from a plain `bool` to `atomic.Bool` so `attachSub` can refuse to attach post-close without acquiring `rc.mu` from inside `subMu` | WS-B1, WS-B2 | 6h | WS-B1 and WS-B2 both pass; `go test -race -gcflags=all=-d=checkptr=0 -count=20 ./internal/infrastructure/buzz/...` passes with no `-race` warnings and no panics; `subEntry`'s doc comment states the "wait for every attach generation" invariant explicitly |
| WS-B4 *(Refactor)* | Code-review checklist item (not just test-passing): confirm no new lock-ordering dependency between `rc.mu` and `subMu` was introduced — no `subMu.Lock()` call appears inside a section already holding `rc.mu`, and vice versa, verified by direct code reading, not inferred from the absence of an observed deadlock | WS-B3 | 1h | Checklist confirmed and recorded in `implementation-notes.md`; combined FR-002+FR-003 test suite covers both interleavings in one file, per the review PRD's Refactor note |
| WS-B5 | Collect ADR/technical-details entries for WS-A, WS-B (this workstream), WS-C, and WS-D into `docs/architectural-decision-record.md` and `docs/technical-details.md` in one pass — per `architecture.md` AD-2's doc-collision resolution | WS-A3, WS-B4, WS-C3, WS-D3 | 2h | One combined commit touching `docs/architectural-decision-record.md` and `docs/technical-details.md`, covering all four workstreams' behavior changes; no other workstream's task edits either file independently |

## WS-C — FR-004 (P2): Process-singleton lock TOCTOU

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| WS-C1 *(Red)* | Write a test with two goroutines racing `AcquireLock` against the same path, the first deliberately slowed between its file-create and content-write steps (via an injectable seam or temporarily-patched open), asserting at most one of the two returns a non-error `*Lock` | — | 2h | Test compiles and fails (both succeed) against current `HEAD` |
| WS-C2 *(Green)* | Change `AcquireLock`'s write path: write PID to a temp file in the same directory as `path`, then `os.Link(tmpPath, path)` (atomic `EEXIST`-checked publish). **Before relying on this cross-platform: `research.md` research question 3's Windows/NTFS equivalence claim was written from general knowledge, not verified against Go's actual `os.Link` behavior on `windows` GOOS in this toolchain — confirm empirically (unit test under `GOOS=windows` cross-compile at minimum, real Windows run if available) that `os.Link` returns an `fs.ErrExist`-compatible error when the target exists, before depending on it.** If it does not hold on Windows, fall back to `O_CREATE|O_EXCL` + write + `fsync` + atomic `os.Rename` from the same-directory temp file (rename is POSIX- and Windows-atomic for same-volume, same-directory moves, which is a better-established primitive than link if `os.Link`'s Windows behavior doesn't check out), then remove the temp file if rename fails. No separate create-then-write step observable by a concurrent reader | WS-C1 | 4h | WS-C1's test passes; `AcquireLock` writes the lock file's content in a single atomic operation; the chosen primitive (`os.Link` or `os.Rename` fallback) is verified — not assumed — to behave atomically on all three target OSes |
| WS-C3 *(Refactor)* | Tighten `readLockPID`'s doc comment to state precisely which crash window (SIGKILL mid-write, now closed by construction) it defends against, now that the write-in-progress window no longer exists; leave the ADR entry for WS-B5's collection pass | WS-C2 | 1h | Doc comment updated; no `docs/architectural-decision-record.md` edit here |

## WS-D — FR-005 (P2): Inbound event content/tag size bound

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| WS-D1 *(Red)* | Write a `Monitor.handleChannelEvent`/`dispatch` test with a multi-megabyte `evt.Content`, asserting it is rejected (logged, not dispatched) rather than forwarded as a task instruction | — | 2h | Test compiles and fails (content is dispatched) against current `HEAD` |
| WS-D2 *(Green)* | Add a `maxContentLen` bound (package constant per `architecture.md` AD-4's default, promotable to a `Monitor.Config` field if a concrete need surfaces during implementation) checked early in `dispatch`, rejecting oversized content with a structured log line naming the event ID and size | WS-D1 | 3h | WS-D1's test passes; ordinary-sized messages unaffected (existing dispatch tests pass unmodified) |
| WS-D3 *(Refactor)* | Document the constant-vs-config-field choice (per AD-4) in a doc comment; leave the ADR entry for WS-B5's collection pass | WS-D2 | 1h | Choice documented; no `docs/architectural-decision-record.md` edit here |

## WS-E — FR-006 (P2), FR-008 (P2): Documentation-only fixes

Batched per the review PRD's own rationale (avoid multiple agents touching `spec.md`/`architecture.md` in the same window); "no code change required" findings still follow red/green discipline against their checkable acceptance criterion (a grep or doc statement), per `AGENTS.md`. FR-007 is **not** here — it moved to WS-A4 (see Progress Summary and WS-A's file-collision note).

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| WS-E1 | FR-006: confirm (red: grep/read finds no such statement today; green: add it) a short note in `nipoa.go`'s package doc comment or `architecture.md` stating `ValidateAuthTag`/`FindAuthTag` currently validate only BaoBot's own configured outbound tag via `StaticAuthTagFunc`, and are not invoked against any inbound/attacker-supplied data | — | 1h | `nipoa.go` or `architecture.md` states the call sites and the "no attacker-controlled data today" fact explicitly |
| WS-E2 | FR-008 part 1: replace `trigger.go:28`'s bare `9` literal with the existing `kindChannelMessage` constant (defined in `monitor.go`) | — | 0.5h | `trigger.go` references `kindChannelMessage` rather than a bare `9`; existing tests pass unmodified |
| WS-E3 | FR-008 part 2: update the **archived original spec's** `specs/archive/260804-boabot-buzz-support/spec.md` (remove the never-created `kinds.go` from §Scope of Changes, or confirm the constants-in-`monitor.go` layout is being kept and note that) and `specs/archive/260804-boabot-buzz-support/tasks.md` (correct FR-027's phasing language to match `implementation-notes.md`'s more precise "guard only, no publish/consume" framing) — **not** this review spec's own `spec.md`/`tasks.md` | — | 1h | Archived original's `spec.md`/`tasks.md` FR-027 description matches what was actually built; `kinds.go`'s planned existence is either removed or explicitly reconciled |

---

## Deferred Items (not scheduled this run)

None — all 8 findings (including the corrected FR-007 assignment) are scheduled in this run's five workstreams. P2 findings could in principle be deferred per the review PRD's own Implementation Guidance ("P2 findings may be deferred to a follow-up if agreed with the team"), but no such agreement is recorded, so all are scheduled here per Step 9's brief expecting full closure.

## Coverage Verification Note

**FR sweep (mechanical, re-run after every edit to this file):** `grep -oE 'FR-[0-9]{3}' tasks.md` restricted to the task tables covers FR-001 through FR-008 with no gaps — verified by diffing against the review PRD's full range, including the FR-007 reassignment correction noted in the Progress Summary above.

**Doc-collision re-check:** WS-A3, WS-C3, WS-D3 each explicitly defer their ADR/technical-details edit to WS-B5; only WS-B5 touches `docs/architectural-decision-record.md`/`docs/technical-details.md`. Re-verify this against the commit log at Step 9's close, per `AGENTS.md`'s "check each finding off explicitly against the commit log — do not rely on memory."
