# Tasks: Buzz DM/Thread Support — Code Review Fixes

**Created:** 2026-08-15
**Status:** Planning

## Progress Summary

6/6 tasks complete.

## Phase 1 — P1 fix (must close before done)

### T-FR301 — Fix Buzz task reply duplicate chat-store write — DONE

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-301. TDD: failing test first (Buzz task's reply appears twice in `sharedChatStore`/`GET /api/v1/chat`), then fix (prefer option a: pass real `ThreadID` through `chatMessageThreadID`, addressing the failure-mode regression the review flagged).
- **Resolution:** option (a) implemented. `chatMessageThreadID` now passes the real Buzz ThreadID through; `Monitor.recordOutbound` moved off the task-completion path (only fires for immediate bridge replies that have no `DirectTask`). See implementation-notes.md for full detail.

## Phase 2 — P2 fixes (independent, parallelizable)

### T-FR302 — Resolve `dispatchedThreads` unbounded growth — DONE

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-302. Either documented as accepted (small entries, bounded in practice) or eviction implemented with a test.
- **Resolution:** documented as accepted, no code change. See implementation-notes.md.

### T-FR303 — Resolve DM pre-gate crypto-cost ordering — DONE

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-303. Either documented as an accepted, inherent NIP-17 property, or a size pre-filter added with a test.
- **Resolution:** documented as accepted, no code change. See implementation-notes.md.

### T-FR304 — Add startup warning for fail-open DM gate — DONE

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-304. TDD: test asserting the warning fires when DM activates with `!gate.active()`, absent when configured. Mirror `LockDir`-empty warning's exact style.
- **Resolution:** fixed. Warning added at the top of `startDMSubscription`. TDD: failing test first (`TestMonitor_StartDMSubscription_GateInactive_WarnsFailOpen`), then fix. `TestMonitor_StartDMSubscription_GateActive_NoWarning` covers the absent case.

### T-FR305 — Tighten FR-207 wording; resolve `publishReply`'s single-`p`-tag scope — DONE

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-305. Docs-only for the wording fix; code change only if extending `publishReply` is chosen.
- **Resolution:** (a) FR-207 wording tightened in the archived spec.md. (b) documented as accepted, no code change. See implementation-notes.md.

### T-FR306 — Document dispatch-failure rollback asymmetry — DONE

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-306. Docs-only, one sentence in implementation-notes.md.
- **Resolution:** sentence added to the archived spec's implementation-notes.md Technical Decisions.
