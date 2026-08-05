# Architecture: Code Review Fixes — BaoBot Buzz Support

**Feature:** boabot-buzz-support-auto-review
**Created:** 2026-08-04
**Status:** Decided (fix-shape choices resolved below, not left for Step 9 to improvise)

---

## Architecture Overview

This is a fix pass on existing infrastructure-layer code (`internal/infrastructure/buzz/`, `internal/infrastructure/*/lock.go`, `internal/infrastructure/config/`) plus one wiring change in `cmd/boabot/main.go`. No new components, no domain-layer changes, no changes to Clean Architecture boundaries (all fixes stay within the infrastructure layer or the `cmd/` wiring layer that already depends on it). See `specs/archive/260804-boabot-buzz-support/architecture.md` for the unchanged system architecture this pass fixes seams in.

## Component Architecture

Affected components, unchanged shape, internal-logic fixes only:
- `RelayClient` (`relay_client.go`) — `attachSub`/`Subscribe`/`Close` internals (FR-002/FR-003).
- `reconnect()` (`reconnect.go`) — lock-scope around `resubscribeAll` (FR-002/FR-003).
- `Lock`/`AcquireLock` (`lock.go`) — write-path internals (FR-004).
- `Monitor.dispatch` (`monitor.go`) — new size-bound check (FR-005).
- `buildBuzzMonitor` (`cmd/boabot/main.go`) — new `opts` entry (FR-001).

## Layer Responsibilities

Unchanged from the original feature. All fixes are infrastructure-layer (concurrency correctness, file I/O atomicity, input validation) or `cmd/` wiring — none require new domain interfaces or application use cases.

## Architectural Decisions

### AD-1: FR-002/FR-003 shared fix mechanism — attach-generation counter + `atomic.Bool`, not continuous-lock-holding

The review PRD's FR-002 Refactor note poses an open choice: hold `subMu` continuously across `Subscribe`'s register→attach sequence (closing the race window entirely), versus a generation/validity-check mechanism at `attachSub` itself. **Decided: generation counter + `atomic.Bool`, not continuous-lock-holding.**

**Rejected alternative — continuous-lock-holding:** `Subscribe` would hold `subMu` from `rc.subs[id] = entry` through its own `attachSub` call, including `attachSub`'s internal `conn.Subscribe(...)` network call. This "network call under lock" shape is exactly what the review PRD's own Refactor note flags as a "liveness concern" — a slow or stuck `conn.Subscribe` would block every other `subMu`-guarded operation (including a concurrent `reconnect()`'s own `resubscribeAll`, which needs `subMu` to snapshot `rc.subs`), risking a new deadlock class in exchange for closing the original race.

**Adopted — attach-generation counter on `subEntry`, `closed` as `atomic.Bool`:**
- Each `subEntry` gains a generation counter, incremented by `attachSub` on every attach attempt. A pump (`pumpSub`) checks it still holds the current generation before each send; if superseded, it exits without sending — this closes FR-002's double-attach-both-pumps-live window without holding any lock across a network call.
- `removeAndClose` waits on *all* generations ever started for an entry (a per-entry `sync.WaitGroup` rather than the single-slot `pumpDone` channel) before closing `entry.out` — this closes the "orphaned pump sends after close" panic path.
- `RelayClient.closed` changes from a plain `bool` (guarded by `rc.mu`) to `atomic.Bool`, so it can be read from `attachSub`'s `subMu`-held critical section without acquiring `rc.mu` — this is precisely what FR-003's Green guidance identifies as making a combined fix safe: **`rc.mu` and `subMu` must never nest in either direction.** Option (a) from FR-003 (hold `rc.mu` across `resubscribeAll`) nests `subMu` inside `rc.mu`; option (b) read naively (call `rc.mu.Lock()` from inside `attachSub`'s `subMu`-held section) nests the opposite way. Combining both as written deadlocks. `atomic.Bool` sidesteps the question entirely — no lock nesting is introduced by this fix in either direction.
- **Acceptance criterion carried onto the WS-B task in `tasks.md`:** the fix introduces no new lock-ordering dependency between `rc.mu` and `subMu` — verified by code review (no `subMu.Lock()` call appears anywhere within a section already holding `rc.mu`, and vice versa) as an explicit review checklist item, not merely inferred from the absence of an observed deadlock in one test run.

### AD-2: Doc-file collision — WS-B collects all ADR/technical-details entries in one pass

Per the review PRD's own "Doc-file collision hazard" note, four workstreams (A, B, C, D) each produce a behavior change requiring `docs/technical-details.md` and `docs/architectural-decision-record.md` updates. Running four parallel worktrees each editing these two files independently guarantees a merge conflict. **Decided: option (b) from the review PRD — WS-B collects all four workstreams' ADR/technical-details entries in one pass, after WS-A/C/D have landed (merged back to `worktree-buzz-support-prd`), since WS-B owns the two P0/P1-adjacent findings with the most architectural weight.** This is now a concrete task (WS-B5 in `tasks.md`) with explicit dependencies on WS-A, WS-C, and WS-D's completion — not a "pick one before starting" instruction left unresolved for Step 9 to rediscover.

### AD-3: FR-004's atomic-write mechanism — `os.Link` primary, `os.Rename` fallback, cross-platform behavior OPEN pending WS-C verification

Per `research.md`'s research question 3, whether `os.Link`'s `EEXIST`-atomic "create with content via same-directory temp file + link" pattern holds identically on Windows/NTFS as on POSIX filesystems is **not yet verified** — an earlier draft of this decision asserted it was confirmed from general knowledge alone, which has been retracted (no tool call backed the claim). **Decided as a two-step plan, not a settled mechanism:** WS-C1/WS-C2 must empirically verify `os.Link`'s behavior on `windows` GOOS before relying on it; if it does not provide the required atomicity/`EEXIST` guarantee, fall back to a same-directory temp file + `fsync` + atomic `os.Rename`. Either primitive satisfies FR-004's actual requirement ("no separate create-then-write step observable by a concurrent reader") — the choice between them is an implementation detail to be settled empirically during WS-C, not asserted here. No build-tag-specific code is anticipated to be needed either way, but that too should be confirmed rather than assumed.

### AD-4: FR-005's bound placement — package constant, not `Monitor.Config` field (default; may be revised during WS-D if a concrete operator need surfaces)

Consistent with OQ-R2's resolution for FR-007 (don't add operator-tunable surface without a demonstrated need), `maxContentLen` defaults to a package constant. WS-D may promote it to a `Monitor.Config` field if implementation reveals a concrete reason (e.g. an existing test fixture that needs a non-default bound) — this is the one Green-guidance choice explicitly left to the implementer's judgment by the review PRD itself ("Consider whether the bound belongs in `Monitor.Config`... or as a package constant; document the choice").

## Data Flow / Sequence Diagrams / Integration Points

Unchanged from the original feature's `architecture.md` for FR-004/FR-005/FR-006/FR-007/FR-008 (no new flows). For FR-001, the new flow is: `buildBuzzMonitor` → `SecretStore.Get(AuthTagSecretName)` → parse pipe-delimited tag → `buzzinfra.StaticAuthTagFunc(tag, pk.Hex())` → `buzzinfra.WithAuthTagFunc(fn)` appended to `opts` → `RelayClient`'s existing `buildSignFn` (Phase D5/E3, unchanged) appends the tag to the AUTH event before signing. For FR-002/FR-003, see AD-1 above — no new data flow, an internal correctness fix to the existing attach/reconnect/close sequence.
