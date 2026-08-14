# Implementation Notes: Boabot Native Daemon Mode — Code Review Fixes

**Created:** 2026-08-14

Record technical decisions, edge cases, deviations from the plan, and lessons learned as implementation proceeds. Update this file continuously during Phase 5 — do not leave it until the end.

## Technical Decisions

- **FR-101 fix shape:** split `markSeenIfDuplicate` into two methods — `isDuplicateEvent` (peek-only, still does the lazy-eviction sweep since it's called on every `Dispatch`) and `markEventSeen` (side-effecting, called only from `Dispatch`'s success paths: the `chatMgr` "handled" branch and the plain-dispatch success branch, both right before their `return`). `markEventSeen` no-ops on an empty `eventID` to preserve the original "no dedup for empty event ID" behavior without needing an `if eventID != ""` guard at every call site.
- **FR-102 fix shape:** `buzzBoardTitle` now compares `utf8.RuneCountInString(title) > boardItemTitleMaxLen` and slices `[]rune(title)[:boardItemTitleMaxLen]` instead of `title[:80]`. `boardItemTitleMaxLen` (80) is now a rune count, not a byte count — matches the acceptance criterion's intent (a short *readable* title) better than a byte cap anyway.
- **FR-103:** wrote the nil-builder-for-one-persona test per the acceptance criterion (two Buzz-enabled personas, builder returns `nil` for one and a real `*mocks.ChannelMonitor` for the other). It passed immediately against the existing code — the `if mon == nil { continue }` branch was already correct, just uncovered (confirmed via the raw coverage profile: `team_manager.go:397.18,398.13` went from hit-count 0 to 1). No production code change. Added a `Monitors()` test-only accessor to `export_test.go` (mirrors the existing `ResolvedPluginStore()`/`ResolvedInstallDir()` pattern) so the test can assert exactly one monitor was registered.

## Edge Cases & Solutions

- FR-101: the regression test uses the same `fakeScheduledDispatcher` the rest of the suite already uses, mutating its `err` field to `nil` between the first (failing) and second (retried) `Dispatch` call with the identical event ID — this reproduces "transient failure, then relay redelivers" without needing a new fake.
- FR-102: verified rune-safe truncation against three cases per the acceptance criterion — a 78-ASCII-byte + 10-rune-Japanese-suffix string (mid-rune boundary under the old byte-based cap), an all-whitespace instruction (empty-fallback path), and a 100-char pure-ASCII instruction (must truncate identically to the old byte-based behavior). All three verified via `buzz.getCreated()[0].Title` since `buzzBoardTitle` itself is unexported and the test file is in `orchestrator_test` (external test package).

## Deviations from Plan

- None yet beyond what's noted above (FR-103 needing no production fix was anticipated by the review PRD itself as the likely outcome).

## Lessons Learned

[TBD — will be filled in once P2 items are underway.]
