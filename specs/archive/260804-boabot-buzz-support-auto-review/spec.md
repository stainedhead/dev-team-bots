# Spec: Code Review Fixes — BaoBot Buzz Support

**Feature:** boabot-buzz-support-auto-review
**Created:** 2026-08-04
**Status:** Ready for implementation
**Source PRD:** [`boabot-buzz-support-auto-review-PRD.md`](./boabot-buzz-support-auto-review-PRD.md) (co-located in this directory)

**Note on FR numbering:** this spec's FR-001–FR-008 are the review PRD's own finding numbers, **not** the original feature's FR-001–FR-054. Where a finding text or this spec references the original feature's requirements, it is written explicitly as "Buzz PRD FR-0xx" to disambiguate. The original spec (with its own FR-001–FR-054) is archived at `specs/archive/260804-boabot-buzz-support/`.

---

## Executive Summary

This is a review-fix pass, not new feature work. `specs/archive/260804-boabot-buzz-support/` shipped BaoBot's Nostr-based "Buzz" channel adapter plus an OS-keystore secret-storage subsystem across nine phases (A–I). An independent code review (`boabot-buzz-support-auto-review-PRD.md`, Steps 5/6 of this dev-flow run) found 2 P0, 1 P1, and 5 P2 findings — all sitting at seams between the original build's phases, exactly where a single-pass cross-phase review is expected to catch what nine phases of individually-green tests did not.

The two P0s block merge: FR-001 is a missing wiring step (the NIP-OA owner-attestation extension point built in Phase E is never called from `main.go`, so the PRD's flagship "join via owner attestation" capability is unreachable in the shipped binary) and FR-002 is a genuine, previously-undiscovered concurrency race in `RelayClient`'s subscription-attach machinery with a credible path to a process-crashing panic. FR-003 (P1) is a second race in the same machinery and must be fixed together with FR-002 per the review PRD's explicit instruction — both stem from `attachSub` lacking a validity/generation check, and splitting them across parallel agents risks two independently-designed locking schemes colliding. FR-004–FR-008 (P2) are a local TOCTOU, a missing input-size bound, two documentation gaps, and one unwired-but-harmless option.

## Problem Statement

The Buzz-support feature was built and self-reviewed across 9 phases (`implementation-notes.md` records advisor-caught fixes at nearly every phase), but a fresh, cross-phase review found defects that no single phase's own tests could have caught: a wiring gap between Phase E's NIP-OA mechanism and Phase H's `main.go` activation, and two concurrency races in Phase D's `RelayClient` that only manifest under real scheduling pressure the existing deterministic `fakeConn` test harness never exercises. These must be fixed, following the same TDD discipline as the original build, before this PR merges.

## Goals / Non-Goals

**Goals:**
- Fix both P0 findings (FR-001, FR-002) — required before merge.
- Fix the P1 finding (FR-003) alongside FR-002, sharing root cause and fix mechanism, per the review PRD's explicit "do not split" instruction.
- Fix all five P2 findings (FR-004–FR-008) in this same pass, since the review PRD does not flag any of them as safe to defer and Step 9's brief expects full closure.
- Keep the original feature's coverage (≥90% domain+application, currently 91.0%) and toolchain-clean status (`go vet`, `golangci-lint`, `gofmt`) unregressed.

**Non-Goals:**
- No new Buzz protocol features (NIP-17 DMs, NIP-50 search, etc. — all remain in the original PRD's Deferred Items).
- No redesign of `SecretStore`, `RelayClient`'s public API shape, or `Monitor`'s dispatch flow beyond what each finding's Green guidance requires.
- OQ-R1 (auth-tag secret format / `boabotctl --format` hint) and OQ-R2 (operator-tunable backoff) are resolved with the PRD's own stated lean in this pass (see `research.md`), not left open into a future run.

## User Requirements — Functional Requirements

Carried as-is from the review PRD (`boabot-buzz-support-auto-review-PRD.md`), already numbered and prioritized at the source. Full Finding text, TDD guidance (Red/Green/Refactor), and Acceptance Criteria for each live in the PRD itself — this section summarizes; do not re-derive from a paraphrase, read the PRD's own FR-00x section before implementing.

### FR-001 (P0): NIP-OA owner-attestation tag is never wired into production
`buzzinfra.WithAuthTagFunc`/`StaticAuthTagFunc` (Phase E, fully built and unit-tested) are never referenced in `cmd/boabot/main.go`'s `buildBuzzMonitor`. No `BuzzConfig` field or `domain.SecretRef` name exists to source a tag from. Result: the PRD's flagship "NIP-AA virtual membership via owner attestation, without explicit enrollment" capability is unreachable in the shipped binary. Fix: add `AuthTagSecretName` (mirroring `PrivateKeySecretName`/`APITokenSecretName`), resolve it through the existing `SecretStore` chain, wire it into `buildBuzzMonitor`'s `opts`, optional (not fail-closed) when absent. Files: `cmd/boabot/main.go`, `internal/infrastructure/config/config.go`, `internal/infrastructure/buzz/{keypair,token}.go`, `user-docs/Buzz-Adoption-Config.md`.

### FR-002 (P0): Concurrent `Subscribe` + reconnect `resubscribeAll` can double-attach a subscription
`RelayClient.Subscribe` registers a `subEntry` before calling its own `attachSub`; a concurrent reconnect's `resubscribeAll` can observe and attach the same not-yet-attached entry independently. `attachSub` has no guard against being called twice for one entry — both calls overwrite `entry.pumpDone` (a single slot) and start a second `pumpSub` goroutine, orphaning the first. The orphaned pump can later send on a channel `removeAndClose` has already closed — unrecovered `panic: send on closed channel`, taking down the entire `boabot` process. Fix: attach-generation counter or continuous lock-holding across register→attach (see `architecture.md` for the resolved choice). Files: `internal/infrastructure/buzz/relay_client.go`, `reconnect.go`.

### FR-003 (P1): `reconnect()` can attach a new pump after `Close()`'s `pumpWG.Wait()` has already returned
Same `attachSub`/`subEntry`/`pumpDone` machinery as FR-002, different trigger: `reconnect()` calls `resubscribeAll`/`attachSub`/`pumpWG.Add(1)` outside `rc.mu`, racing `Close()`'s `pumpWG.Wait()` (which can observe zero in-flight pumps before the new `Add`) — a documented `sync.WaitGroup` misuse pattern, again with a credible path to send-on-closed-channel. **Must be fixed together with FR-002, in one workstream** — same root cause, and FR-003's own Green guidance proposes `closed` as an `atomic.Bool` as the specific mechanism shared with FR-002's fix. Files: same as FR-002.

### FR-004 (P2): Process-singleton lock's stale-lock reclaim has a TOCTOU
`AcquireLock` creates the lock file (`O_CREATE|O_EXCL`) then writes the PID as a separate step; a concurrent acquirer hitting `EEXIST` in that gap reads an empty file, concludes stale, reclaims — both acquirers can then believe they hold the lock. Fix: write to a same-directory temp file, then `os.Link` into place (atomic "create with content" via `link(2)`'s own `EEXIST` semantics) — verify equivalent atomicity on Windows/NTFS, this is a genuine open research question (see `research.md`). Files: `internal/infrastructure/*/lock.go`.

### FR-005 (P2): No application-level bound on inbound Nostr event content/tag size
`dispatch`/`discovery.go` impose no length limit on `evt.Content` or tag count/size. Defense-in-depth: an authenticated channel member can publish an oversized `kind:9` event, spending uncontrolled token/cost if it passes the trigger/author gate. Fix: `maxContentLen` check early in `dispatch` (constant or `Monitor.Config` field, per Refactor note — see `data-dictionary.md`), rejecting with a structured log line. Files: `internal/infrastructure/buzz/monitor.go`, possibly `discovery.go`.

### FR-006 (P2): NIP-OA validation logic's threat-model boundary is undocumented
`ValidateAuthTag`/`FindAuthTag` are well-written but only ever validate BaoBot's own locally-configured outbound tag — nothing calls them against attacker-controlled inbound data today. Not a defect; needs an explicit doc statement so a future feature extending this path knows it's entering new threat-model territory. No code change. Files: `nipoa.go` or `architecture.md`.

### FR-007 (P2): `WithBackoff`/`WithAuthRetryInterval` built but never wired from `BuzzConfig`
Minor, no PRD FR names this as operator-configurable. Resolved this pass (OQ-R2, see `research.md`): the hardcoded default is permanent, recorded as a one-line `BuzzConfig` doc comment — no new fields. Files: `internal/infrastructure/config/config.go` (doc comment only).

### FR-008 (P2): `kinds.go` never created; FR-027's scope description overstated
Two documentation-accuracy gaps in the *original* spec, not this one: `specs/archive/260804-boabot-buzz-support/spec.md`'s planned `kinds.go` file was never created (constants live in `monitor.go`, harmlessly); `trigger.go:28` uses a bare `9` literal instead of `kindChannelMessage`; and the original `spec.md`/`tasks.md`'s FR-027 phasing language overstates what was built (guard-only, no reaction publish/consume). Fix targets the **archived original's** files (absolute paths: `specs/archive/260804-boabot-buzz-support/spec.md`, `.../tasks.md`) plus a trivial `trigger.go` edit — not this review spec's own files. Files: `internal/infrastructure/buzz/trigger.go`, `specs/archive/260804-boabot-buzz-support/spec.md`, `specs/archive/260804-boabot-buzz-support/tasks.md`.

## Non-Functional Requirements

- **TDD is mandatory per finding, no exceptions** (per `AGENTS.md` and the review PRD's own Implementation Process section) — including FR-006/FR-007/FR-008, whose acceptance criteria are checkable facts (a grep, a doc statement) that should be verified failing-then-passing like any other red/green cycle.
- **One commit per FR**, message referencing the FR number (e.g. `fix(buzz): FR-002/FR-003 — guard attachSub against duplicate/stale attach`), each preceded by `go fmt ./...`, `go vet ./...`, `golangci-lint run`, and `go test -race -gcflags=all=-d=checkptr=0 ./...` passing locally.
- **A brief review follows each fix**, before starting the next finding — do not batch multiple findings into one commit.
- Coverage target unregressed: ≥90% on `internal/domain/...` and `internal/application/...` (excluding `mocks/`), currently 91.0%.
- All `-race` runs against `internal/infrastructure/buzz/...` MUST include `-gcflags=all=-d=checkptr=0` (confirmed upstream `fiatjaf.com/nostr` bug, documented in the original `research.md`) — omitting it reproduces a known nondeterministic abort unrelated to these fixes.

## System Architecture

**Affected layers:**
- **Infrastructure** (`internal/infrastructure/buzz/`): `relay_client.go`, `reconnect.go` (FR-002/FR-003), `monitor.go`/`discovery.go` (FR-005), `nipoa.go` (FR-006 doc-only), `trigger.go` (FR-008), `keypair.go`/`token.go` (FR-001 new secret name).
- **Infrastructure** (shared): `internal/infrastructure/*/lock.go` (FR-004).
- **Infrastructure** (`internal/infrastructure/config/`): `config.go` (FR-001 new field, FR-007 doc comment).
- **cmd**: `cmd/boabot/main.go` (FR-001 wiring).
- **Docs**: `user-docs/Buzz-Adoption-Config.md`, `docs/architectural-decision-record.md`, `docs/technical-details.md`, and the **archived original spec's** `spec.md`/`tasks.md` (FR-008).

No new components; no domain-layer changes are anticipated (all fixes are infrastructure-layer or wiring-layer). See `architecture.md` for the FR-002/FR-003 fix-shape decision and the doc-file collision resolution.

## Scope of Changes

**Files to modify (no new packages):**
- `internal/infrastructure/buzz/relay_client.go`, `reconnect.go` — FR-002/FR-003 shared fix.
- `internal/infrastructure/buzz/monitor.go`, `discovery.go` — FR-005.
- `internal/infrastructure/buzz/nipoa.go` or `architecture.md` — FR-006.
- `internal/infrastructure/buzz/trigger.go` — FR-008 (bare literal → constant).
- `internal/infrastructure/buzz/keypair.go`, `token.go` — FR-001 (new `AuthTagSecretName` constant, alongside existing pattern).
- `internal/infrastructure/config/config.go` — FR-001 (new `BuzzConfig` field or doc comment), FR-007 (doc comment).
- `internal/infrastructure/*/lock.go` — FR-004.
- `cmd/boabot/main.go` — FR-001 wiring.
- `user-docs/Buzz-Adoption-Config.md` — FR-001.
- `docs/architectural-decision-record.md`, `docs/technical-details.md` — collected ADR/technical-details entries for all workstreams (see `architecture.md`'s doc-collision resolution).
- `specs/archive/260804-boabot-buzz-support/spec.md`, `.../tasks.md` — FR-008 (documentation-accuracy correction on the **original**, already-archived spec).

**Dependencies:** none new. No new third-party packages required by any finding's Green guidance.

## Breaking Changes

None. FR-001 adds a new optional `SecretRef`/config surface (backward-compatible: absent behaves exactly as today). No API, config-schema-breaking, or schema changes. FR-002/FR-003's fix is internal to `RelayClient`'s concurrency handling, no public API change.

## Success Criteria and Acceptance Criteria

Per-finding acceptance criteria are carried verbatim in the source PRD (`boabot-buzz-support-auto-review-PRD.md`) — implement against those, not a paraphrase. Quality gates (apply to every finding, all must pass before Step 9 closes):
- [ ] Every finding (FR-001–FR-008) has a corresponding commit, checked explicitly against the commit log (not memory), per `AGENTS.md`.
- [ ] `go fmt ./...`, `go vet ./...`, `golangci-lint run` clean on both `boabot` and `boabotctl` modules.
- [ ] `go test -race -gcflags=all=-d=checkptr=0 ./...` passes repo-wide, including new tests for FR-002/FR-003/FR-004/FR-005 under repeated runs (`-count=20` recommended for the concurrency fixes).
- [ ] Coverage on `internal/domain/...` + `internal/application/...` (excluding `mocks/`) remains ≥90%, not regressed from 91.0%.
- [ ] `docs/architectural-decision-record.md` and `docs/technical-details.md` updated once (not four times independently — see `architecture.md`'s collision resolution), covering all workstreams that changed behavior.
- [ ] No P0 finding remains open (blocks PR per `AGENTS.md`).

## Risks and Mitigation

- **Risk:** FR-002/FR-003 fixed independently by two agents, producing two incompatible locking schemes. **Mitigation:** single workstream (WS-B), single design decision, single combined test suite — enforced in `tasks.md`.
- **Risk:** Four workstreams (A, B, C, D) each append to `docs/architectural-decision-record.md`/`docs/technical-details.md` in parallel worktrees, causing merge conflicts. **Mitigation:** WS-B collects all ADR/technical-details entries in one pass after WS-A/C/D land (see `architecture.md`), not written concurrently.
- **Risk:** `os.Link`-based atomic lock-file write (FR-004) behaves differently on Windows/NTFS than POSIX. **Mitigation:** flagged as an explicit research question in `research.md`, resolved before WS-C's Green step, not discovered mid-implementation.
- **Risk:** FR-008's fix accidentally edits this review spec's own `spec.md`/`tasks.md` instead of the archived original's. **Mitigation:** absolute paths stated explicitly above and in `tasks.md`.

## Timeline and Milestones

Single implementation pass (Step 9 of this dev-flow run), five workstreams (WS-A through WS-E, see `tasks.md`), WS-A/C/D/E parallelizable, WS-B (FR-002+FR-003) is the critical-path item given its concurrency-fix complexity. No phased rollout — all fixes land on `worktree-buzz-support-prd` before the PR is opened (Step 14).

## References

- Source PRD: [`boabot-buzz-support-auto-review-PRD.md`](./boabot-buzz-support-auto-review-PRD.md) (this directory)
- Original feature spec (archived): `specs/archive/260804-boabot-buzz-support/` (spec.md, tasks.md, architecture.md, implementation-notes.md, status.md)
- Original feature PRD (archived): `specs/archive/260804-boabot-buzz-support/boabot-buzz-support-PRD.md`
