# Spec: Buzz DM/Thread Support — Code Review Fixes

**Created:** 2026-08-15
**Status:** Draft
**Source PRD:** [buzz-dm-and-thread-support-auto-review-PRD.md](./buzz-dm-and-thread-support-auto-review-PRD.md)

## Executive Summary

Closes the 6 findings (0 P0 / 1 P1 / 5 P2) from the code-and-design review of the Buzz DM/threading feature (`specs/archive/260814-buzz-dm-and-thread-support/`). Overall assessment was "Approve with minor comments." One real user-facing bug (P1, duplicate chat rows) and five minor/documentation items, including a nuanced independent finding about the fail-open DM author-gate's real-world exposure (already accepted by the operator with full disclosure — this spec's job is the cheap runtime-visibility hardening (FR-304), not re-litigating that decision).

## Problem Statement

The code review found: every Buzz-dispatched task's reply is written to the shared chat store twice, and both copies pass the global chat feed's filter — visible duplication in the operator's UI (FR-301, P1). Five P2 items round this out: unbounded `dispatchedThreads` map growth in a long-running daemon (FR-302); gift-unwrap crypto work running before the author gate on every inbound DM, an inherent-but-worth-noting property of NIP-17 addressing (FR-303); no startup warning when DM listening activates with an unconfigured author gate, despite an existing precedent pattern (`LockDir`-empty warning) for exactly this kind of thing (FR-304); outbound NIP-10 tagging narrower than full multi-hop convention plus a spec-wording tightening (FR-305); and an undocumented (but likely intentional) asymmetry in dispatch-failure rollback (FR-306).

## Goals

- Close FR-301 (P1) with a TDD-first fix — a Buzz task's reply must appear exactly once in the operator's chat feed.
- Close as many of the 5 P2 findings as practical; each is independent and low-risk.
- Add the FR-304 startup warning, since the review's independent analysis concluded the fail-open DM gate's real-world exposure differs meaningfully from the channel path's, even though the code-level gate function is identical — this is cheap, high-value hardening the review explicitly recommended.

## Non-Goals

- Not re-litigating the fail-open DM author-gate default itself — the repo owner already accepted this risk with full documentation in place; this spec only adds the cheap runtime-visibility hardening the review recommended (FR-304), not a design change to the gate's default behavior.
- Not expanding scope beyond the 6 findings.
- Not touching `internal/infrastructure/acp` (ACP mode) — untouched by the original feature, stays untouched here.

## User Requirements / Functional Requirements

**FR-301 (P1):** A Buzz-dispatched task's reply appears exactly once in `GET /api/v1/chat`'s response — either `chatMessageThreadID` passes the real Buzz `ThreadID` through (removing the duplicate write) with the failure-mode regression addressed, or `handleChatList`/`ListByBot` additionally exclude the `ThreadID: ""` copy for Buzz-sourced tasks.

**FR-302 (P2):** `dispatchedThreads`' unbounded growth is either explicitly accepted and documented (bounded in practice, small entries), or given a bounded eviction policy.

**FR-303 (P2):** The gift-unwrap-before-gate-check ordering on the DM path is explicitly documented as an accepted, inherent property of NIP-17 addressing, or a cheap pre-filter (event size) is added mirroring the channel path's `maxContentLen` check.

**FR-304 (P2):** A `Warn`-level log line is emitted when DM listening activates with `!m.gate.active()`, mirroring the existing `LockDir`-empty warning's style and greppability.

**FR-305 (P2):** `spec.md`'s FR-207 wording is tightened to scope the three-tag NIP-10 requirement to channel replies only (cross-referencing the DM line); `publishReply`'s single-`p`-tag behavior (vs. full multi-hop NIP-10 convention) is either accepted as-is with documented rationale or extended.

**FR-306 (P2):** One sentence added to `implementation-notes.md` explicitly documenting that `dispatchedThreads`/`ChatStore` state is intentionally not rolled back on a failed dispatch attempt (distinguishing it from the `eventID` dedup rollback).

## Non-Functional Requirements

- **Correctness:** FR-301 is a data-integrity/UI-correctness bug — fix must be verified by a test reproducing the duplication before the fix, per TDD.
- **Reliability:** FR-302's resolution (whichever chosen) must not affect `KnownThread`'s correctness for live/recent conversations.
- **Observability:** FR-304 directly improves observability — a fail-open DM gate becomes greppable in running-process logs, not just discoverable in documentation.
- **Documentation accuracy:** FR-303, FR-305, FR-306 are "make the docs/spec match reality or add an explicit accepted-tradeoff note" fixes.
- **No regressions:** All existing tests, `-race` (with CI's `-gcflags=all=-d=checkptr=0` flag), `golangci-lint`, `go vet`, `gofmt` stay clean throughout.

## System Architecture

No new components. All fixes localized to files already touched by the original feature:
- `boabot/internal/application/team/team_manager.go` (`chatMessageThreadID`) — FR-301, if option (a) chosen.
- `boabot/internal/infrastructure/http/server.go` (`handleChatList`) — FR-301, if option (b) chosen.
- `boabot/internal/application/orchestrator/buzz_task_bridge.go` (`dispatchedThreads`) — FR-302.
- `boabot/internal/infrastructure/buzz/dm.go` (`handleDMEvent`) — FR-303, if a pre-filter is added.
- `boabot/internal/infrastructure/buzz/monitor.go`/`main.go` (`Start`/`buildBuzzMonitor`) — FR-304.
- `boabot/internal/infrastructure/buzz/monitor.go` (`publishReply`) — FR-305, if extended.
- `specs/archive/260814-buzz-dm-and-thread-support/spec.md`, `implementation-notes.md` — FR-305, FR-306 (docs-only sub-items).

## Scope of Changes

- Files to modify: see System Architecture above. Exact set depends on which option is chosen for FR-301/302/303/305 (each has a "fix" vs. "document as accepted" branch) — decided during task breakdown/implementation, not pre-committed here.
- Dependencies: none new.

## Breaking Changes

None expected. FR-301's fix changes internal chat-store write behavior, not any public API/config schema.

## Success Criteria and Acceptance Criteria

- [ ] FR-301: new test drives a Buzz-dispatched task through `HandleResult` end-to-end and asserts `sharedChatStore` contains exactly one message with the bot's output content.
- [ ] FR-302: growth policy decision made and documented, or eviction implemented with a test.
- [ ] FR-303: gift-unwrap-before-gate ordering explicitly documented as accepted, or a pre-filter added with a test.
- [ ] FR-304: warning emitted when DM activates with an inactive gate; test asserts present/absent correctly.
- [ ] FR-305: spec.md FR-207 wording tightened; `publishReply`'s single-`p`-tag behavior decision documented or extended.
- [ ] FR-306: implementation-notes.md gains the one-sentence rollback-asymmetry rationale.

**Quality gates:** `go fmt`, `go vet`, `golangci-lint run`, `go test -race -gcflags=all=-d=checkptr=0 ./...` all clean; no coverage regression on `internal/domain`/`internal/application` aggregate (currently 91.4%).

## Risks and Mitigation

| Item | Type | Notes | Mitigation |
|------|------|-------|------------|
| FR-301's fix touching the generic `WithTaskResultHandler` path | Risk | This handler serves every bot's task results, not just Buzz — a change here must not affect non-Buzz task result recording. | Scope the fix narrowly (check `DirectTaskSourceBuzz` specifically) and run the full suite, not just Buzz-specific tests. |
| FR-303's tradeoff (fix vs. document) | Risk (low) | Adding a pre-filter changes DM-path behavior for oversized events; documenting as-is changes nothing. | Default toward documenting as accepted per the review's own framing ("no demonstrated exploit... inherent to how gift-wrap addressing works") unless implementation reveals a concrete problem. |
| FR-304's warning message wording | Risk (low) | Must be accurate and not alarmist/misleading, consistent with the existing `LockDir` warning's tone. | Mirror the existing warning's exact style, per the review's explicit suggestion. |

## Timeline and Milestones

[TBD] — tracked via `status.md`; expected to be a short spec given 0 P0 and mostly independent, narrow fixes.

## References

- Source PRD: [buzz-dm-and-thread-support-auto-review-PRD.md](./buzz-dm-and-thread-support-auto-review-PRD.md)
- Original feature spec (archived): `specs/archive/260814-buzz-dm-and-thread-support/`
