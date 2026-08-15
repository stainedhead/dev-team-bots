# Research: Buzz DM/Thread Support — Code Review Fixes

**Created:** 2026-08-15
**Source PRD:** [buzz-dm-and-thread-support-auto-review-PRD.md](./buzz-dm-and-thread-support-auto-review-PRD.md)

## Research Questions

1. FR-301: confirm current `chatMessageThreadID`'s exact behavior for `DirectTaskSourceBuzz` and `handleChatList`'s exact filter logic at current `HEAD`, to pick between option (a) (pass real ThreadID through) and option (b) (filter at read time) — the review favors neither explicitly; check which is lower-risk given the failure-mode regression option (a) must address (a relay-publish failure must still leave a chat record).
2. FR-303: confirm whether `dispatch()`'s channel-path `maxContentLen` pre-filter is easily mirrored for the DM path's raw gift-wrap event, or whether gift-wrap event structure makes a pre-filter awkward (e.g. size is only meaningful post-decrypt).
3. FR-304: confirm the exact wording/style of the existing `LockDir`-empty warning in `Monitor.Start` to mirror it precisely.
4. FR-305: confirm whether extending `publishReply` to carry forward parent `p` tags is a small, contained change or touches the `replyTarget`/`pendingEntry` struct significantly — informs the accept-as-is vs. extend decision.

## Industry Standards

[TBD — not relevant; these are internal bug fixes and doc corrections.]

## Existing Implementations

- The original feature's implementation (`specs/archive/260814-buzz-dm-and-thread-support/`) and its commits (`fd0e7a8`, `e680676`) are the direct object of these fixes.
- The code review itself (`buzz-dm-and-thread-support-auto-review-PRD.md`) already did significant verification work (Acceptance Criteria Cross-Check, Fail-Open DM Author-Gate analysis) — reuse those findings rather than re-deriving them.
- `Monitor.Start`'s existing `LockDir`-empty warning (referenced by FR-304) — reference implementation for the new DM-gate warning's style.

## API Documentation

[TBD — no external APIs involved.]

## Best Practices

[TBD]

## Open Questions

- FR-301's option (a) vs. (b) — resolved during implementation, not a blocking design question (both are valid per the review; pick based on which is lower-risk at current code state).
- FR-303's fix-vs-document choice — same, resolved during implementation.

## References

- Source PRD: [buzz-dm-and-thread-support-auto-review-PRD.md](./buzz-dm-and-thread-support-auto-review-PRD.md)
- Original feature spec (archived): `specs/archive/260814-buzz-dm-and-thread-support/`
