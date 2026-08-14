# Plan: Buzz DM Support and Full Threaded-Reply Support

**Created:** 2026-08-14
**Status:** Planning

## Development Approach

TDD (Red → Green → Refactor) throughout, per AGENTS.md. Clean Architecture boundaries enforced. Security-sensitive code (DM decrypt/encrypt, author gating) gets extra scrutiny at code-review time given the NFR requirements around key/plaintext handling and NIP-17 privacy-property preservation.

## Phase Breakdown

1. Research (this directory's `research.md`) — resolve RQ1-RQ5, especially the conversation-continuation reuse question (RQ1), before task breakdown.
2. Data modeling (`data-dictionary.md`) — finalize DM-origin labeling and `ThreadID` semantics.
3. Architecture (`architecture.md`) — finalize DM subscribe/decrypt/reply-publish design and thread-continuation trigger design.
4. Task breakdown (`tasks.md`) — concrete TDD tasks.
5. Implementation — DM path, thread-continuation path, NIP-10 outbound completion, ThreadID fix, in that rough dependency order (DM and threading fixes are largely independent of each other and can proceed in parallel).

## Critical Path

DM path (subscribe → decrypt → dispatch → reply-publish) and threading fixes (inbound recognition → ThreadID fix → outbound NIP-10 completion → conversation continuation) are largely independent workstreams sharing the `BuzzTaskBridge` as a common integration point — can be parallelized via worktrees/agent teammates per AGENTS.md guidance, converging at the `BuzzTaskBridge` extension for conversation continuation (which both DM and channel threading need).

## Testing Strategy

- Unit tests (domain/application) for the DM unwrap/decrypt path and the thread-continuation trigger logic, mocking the relay client.
- `-race` tests for concurrent thread/DM-conversation dispatch (extending the pattern already established for multi-persona concurrency in the prior feature).
- Security-specific tests: verify no plaintext/key material reaches log output; verify unauthorized-DM rejection.
- Manual verification against a live Buzz relay for actual NIP-17 gift-wrap round-trip (not fully automatable — relay support itself is RQ3).

## Rollout Strategy

Single PR via dev-flow, same pattern as the prior feature. ACP mode untouched. Existing channel-mention dispatch must not regress — verified via existing test suite plus new regression tests for the `ThreadID` fix.

## Success Metrics

See spec.md Acceptance Criteria — all checklist items must pass.
