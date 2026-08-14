# Research: Buzz DM Support and Full Threaded-Reply Support

**Created:** 2026-08-14
**Source PRD:** [buzz-dm-and-thread-support-PRD.md](./buzz-dm-and-thread-support-PRD.md)

## Research Questions

1. Does the existing worker/agent execution path already support resuming a task's conversation with new context (multi-turn continuation), or does FR-206 require new conversation-state machinery? Check `internal/application` for any existing multi-turn/continuation mechanism (the web-UI chat path may already have one — `ChatTaskManager` or `Worker.Execute`'s task model).
2. Exact current shape of `nip44`/`nip59` usage available in the vendored `fiatjaf.com/nostr` dependency — what functions/types exist for conversation-key derivation, seal, and gift-wrap, and what's the minimal call sequence to unwrap an inbound kind:1059 event into a kind:14 rumor?
3. Does the configured Buzz relay (`wss://feral-sysd.communities.buzz.xyz`) actually support gift-wrap (kind:1059) event kinds — subscription and publish? Not verifiable by static code reading alone; may require checking relay documentation/NIP support list, or deferring to implementation-time verification against the live relay.
4. What is the minimal, safe way to key `DirectTask.ThreadID`/`ChatTaskManager`'s pending-intent map by NIP-10 thread root (or DM conversation identifier) instead of `channelUUID`, without breaking the existing single-thread-per-channel case that works today?
5. Exact current `classifyTrigger`/`rootEventID` control flow (trigger.go) — where precisely does the `@mention` gate short-circuit before thread-root resolution runs, and what's the minimal change to also check "is this a reply within a thread this persona already dispatched in" as an alternate trigger path?

## Industry Standards

- NIP-17 (private direct messages): kind 14 (unsigned DM "rumor") → sealed as kind 13 (`nip44`-encrypted, signed by sender) → gift-wrapped as kind 1059 (encrypted again with an ephemeral key, `p`-tagged to the recipient, timestamp-randomized).
- NIP-10 (thread/reply conventions): `e` tags marked `"root"` and `"reply"`; `p` tags addressing participants.
- NIP-44 (encryption) and NIP-59 (gift wrap) — already vendored in `fiatjaf.com/nostr`, confirmed via module cache.

## Existing Implementations

- `rootEventID` (trigger.go:90-102) — existing NIP-10 root resolution, reused for FR-207/FR-208.
- `publishReply` (monitor.go:531-546) — existing outbound channel reply path, to be extended (FR-207) and used as the template (not directly reused — different tag shape) for the new DM reply path (FR-209).
- `guard.go`'s p-gate (lines 14-18, 43-57) — already anticipates a kind-1059 subscription ("P1's DM work" comment) — confirms this feature was designed for, not just bolted onto, the existing architecture.
- `BuzzTaskBridge` (`internal/application/orchestrator/buzz_task_bridge.go`) — existing dispatch bridge, to be extended for thread-keyed dispatch (FR-208) and reused as-is for DM dispatch (FR-203).

## API Documentation

- `fiatjaf.com/nostr`'s `nip44`/`nip59` package APIs — to be read directly from the vendored module source during implementation (RQ2).

## Best Practices

[TBD — NIP-17's own spec text on preserving privacy properties (ephemeral keys, timestamp randomization) is the primary reference; no additional external standard expected.]

## Open Questions

- RQ1 (conversation-continuation reuse) — significantly affects implementation size; must be resolved before task breakdown (Phase 4).
- RQ3 (relay NIP-17 support) — may not be resolvable without live verification; flagged as an implementation-time check, not a blocker to starting code.
- FR-204's unauthorized-DM-handling decision (silent ignore vs. decline reply) — operator decision, not yet made.
- Whether DM conversations that go dormant should eventually be treated as a new conversation, or always continue the original task indefinitely — not yet decided (PRD Open Questions).

## References

- Source PRD: [buzz-dm-and-thread-support-PRD.md](./buzz-dm-and-thread-support-PRD.md)
- Prior feature precedent: `specs/archive/260814-boabot-native-daemon-mode/research.md` (similar research-question resolution pattern)
