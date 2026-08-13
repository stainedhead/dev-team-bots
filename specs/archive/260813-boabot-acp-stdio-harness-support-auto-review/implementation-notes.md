# Implementation Notes: BaoBot ACP Stdio Harness Support — Auto-Review Fixes

**Created:** 2026-08-13

## Purpose

Living record of implementation decisions, edge cases, and deviations from plan.md.

## Technical Decisions

- **FR-001's fix approach: mutex-serialized turns (`turnMu`)**, chosen over the other two options in the open question (per-turn `Worker`, session-scoped progress routing). Rationale: `buzz-acp` already has `--agents N` to scale via multiple process instances, so a single instance handling one turn at a time is consistent with its own scaling model rather than fighting it; it's also the simplest fix that eliminates the race entirely rather than narrowing it, with no new plumbing (no new map, no decision about whether `Worker` construction is cheap to repeat per turn).
- **RT4/FR-005 turned out to be a side effect of RT1**, not a separate fix — serialization makes "overlapping turns on the same session" impossible by construction. Still added a dedicated test (`TestAgent_Prompt_SameSessionOverlappingTurns_SecondCancelNotClobbered`) rather than relying on RT1's test to implicitly cover it, since the review called it out as its own finding with its own acceptance criterion.
- **RT3/FR-004: wired `WithRulesTracker` but this is presently inert.** ACP-mode tasks always carry `Task.WorkDir == ""` (the original feature's own documented decision — no persona-level single-work-dir config field exists), and `RulesTracker.UpdateForDir` is only consulted when `WorkDir != ""`. So wiring the tracker doesn't change observable behavior today. What it *does* fix: ACP mode's construction pattern is now identical to native mode's (`team_manager.go:836-837`), matching exactly what the review asked for, and closing the risk of future silent divergence if `WorkDir` population is ever added to either code path. Chose to wire it (not defer + document) since the fix is a one-line, zero-risk mirror of existing, already-tested native-mode code — no reason to defer something this cheap and low-risk.
- **RT5/FR-006: both a real fix (`CloseSession`) and a safety net (bounded FIFO eviction).** Implementing `CloseSession` for real is the *correct* protocol behavior regardless of whether `buzz-acp` calls it — but since that's unconfirmed, a size bound (`defaultMaxSessions = 10_000`, `WithMaxSessions` override) guards against unbounded growth even if it never does.

## Edge Cases & Solutions

- **turnMu's residual limitation**, documented on the field itself: `session/cancel` for a session whose turn hasn't started yet (still queued behind another session's in-flight turn under `turnMu`) is a no-op, since that session's `session.cancel` isn't populated until its turn actually begins. Not addressed — no finding required it, and it's a narrow, low-consequence edge case (the queued turn still runs to completion rather than silently vanishing; the client can send another `session/cancel` once it actually starts).
- **`sessionOrder` cleanup**: both `CloseSession` and the FIFO-eviction path in `NewSession` remove entries from `sessionOrder`, not just `sessions` — an earlier draft only cleaned up the map, which would have made `sessionOrder` itself the new unbounded-growth leak.

## Deviations from Plan

None — all 8 tasks (RT1-RT8, covering 9 findings) completed as scoped in the review PRD, no scope changes.

## Lessons Learned

- Writing the failing test first for each fix (in particular `TestAgent_Prompt_ConcurrentSessionsDoNotRace`, which reproduced the exact `-race` failure the independent reviewer described) made the fix's correctness self-evident rather than trusted-by-assertion — the same TDD discipline that caught bugs during the original feature's implementation caught the fix's correctness too.
- `TestACPIntegration_ExitsCleanlyOnStdinEOF` (RT8) is a good example of a finding that looked "probably fine, just untested" in the review but was worth verifying directly anyway: it passed on the first try, but the earlier code path had genuinely never been exercised end-to-end, so "very likely correct" (the review's own words) is now simply "verified correct."
