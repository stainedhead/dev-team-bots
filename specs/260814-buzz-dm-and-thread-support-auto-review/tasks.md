# Tasks: Buzz DM/Thread Support — Code Review Fixes

**Created:** 2026-08-15
**Status:** Planning

## Progress Summary

0/6 tasks complete.

## Phase 1 — P1 fix (must close before done)

### T-FR301 — Fix Buzz task reply duplicate chat-store write

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-301. TDD: failing test first (Buzz task's reply appears twice in `sharedChatStore`/`GET /api/v1/chat`), then fix (prefer option a: pass real `ThreadID` through `chatMessageThreadID`, addressing the failure-mode regression the review flagged).

## Phase 2 — P2 fixes (independent, parallelizable)

### T-FR302 — Resolve `dispatchedThreads` unbounded growth

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-302. Either documented as accepted (small entries, bounded in practice) or eviction implemented with a test.

### T-FR303 — Resolve DM pre-gate crypto-cost ordering

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-303. Either documented as an accepted, inherent NIP-17 property, or a size pre-filter added with a test.

### T-FR304 — Add startup warning for fail-open DM gate

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-304. TDD: test asserting the warning fires when DM activates with `!gate.active()`, absent when configured. Mirror `LockDir`-empty warning's exact style.

### T-FR305 — Tighten FR-207 wording; resolve `publishReply`'s single-`p`-tag scope

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-305. Docs-only for the wording fix; code change only if extending `publishReply` is chosen.

### T-FR306 — Document dispatch-failure rollback asymmetry

- **Depends on:** none
- **Acceptance criteria:** per spec.md FR-306. Docs-only, one sentence in implementation-notes.md.
