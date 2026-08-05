# Implementation Notes: Code Review Fixes — BaoBot Buzz Support

**Feature:** boabot-buzz-support-auto-review
**Created:** 2026-08-04

**Purpose:** Record decisions, edge cases, and deviations discovered during Step 9 (Implement Review Fixes) that aren't captured elsewhere. Update this file as each workstream's tasks complete — do not wait until the end. Each workstream should append its own dated section below as it lands, mirroring the original feature's `implementation-notes.md` pattern (`specs/archive/260804-boabot-buzz-support/implementation-notes.md`).

---

## Technical Decisions

Recorded ahead of implementation, during spec creation (Step 8):

- **FR-002/FR-003 shared fix mechanism** (attach-generation counter + `atomic.Bool` for `closed`, not continuous-lock-holding): see `architecture.md` AD-1 for full rationale, including the lock-order hazard that rules out naively combining the review PRD's own options (a) and (b).
- **Doc-file collision resolution** (WS-B5 collects all ADR/technical-details entries after WS-A/C/D land): see `architecture.md` AD-2.
- **FR-004's `os.Link` atomicity confirmed cross-platform** (no Windows/NTFS gap): see `research.md` research question 3.
- **OQ-R1 resolved:** auth-tag secret is a pipe-delimited opaque string (`owner_pubkey_hex|conditions|sig_hex`), no `boabotctl --format` flag. See `research.md`.
- **OQ-R2 resolved:** reconnect backoff defaults are permanent, recorded as a `BuzzConfig` doc comment (FR-007/WS-A4), no new config fields.
- **FR-007 workstream reassignment:** the review PRD's own workstream table omits FR-007 entirely. Assigned to WS-A (as WS-A4), not WS-E, since both FR-001 and FR-007 touch `internal/infrastructure/config/config.go` — file-collision affinity, not silently dropped. See `tasks.md`'s Progress Summary for the full correction note.

## WS-E (FR-006, FR-008) — 2026-08-04

**FR-006 (red/green against a checkable claim, not a Go test):** Red — grepped `ValidateAuthTag|FindAuthTag` across the whole `boabot` module (not just `internal/infrastructure/buzz/`); confirmed no existing statement in `nipoa.go` described the call-site picture. Findings: `ValidateAuthTag`'s only non-test call site is `StaticAuthTagFunc` (`nipoa.go`), which validates a single, locally-configured outbound tag once at construction time; `FindAuthTag` has **no** non-test call site at all today (only `nipoa_test.go`). Neither is invoked against any inbound/attacker-supplied event. Green — added a "Threat-model note (FR-006, review PRD)" paragraph to `nipoa.go`'s package doc comment stating exactly this.

**FR-008 part 1 (`trigger.go`):** Red — confirmed `trigger.go:28` used a bare `9` while `kindChannelMessage = 9` was already defined in `monitor.go` (not moved). Green — swapped the literal for the constant. One-line change, nothing else in `trigger.go` touched. `go build ./...`, `go vet ./...`, `golangci-lint run ./internal/infrastructure/buzz/...` (0 issues), and `go test ./internal/infrastructure/buzz/...` all pass.

**FR-008 part 2 (archived original spec):** Corrected `specs/archive/260804-boabot-buzz-support/spec.md`'s §Scope of Changes: struck through the never-created `internal/infrastructure/buzz/kinds.go` line with a note that kind constants live in `monitor.go` instead (plus `guard.go`'s standalone `reactionKind`), rather than silently deleting the planned-file line. Corrected `specs/archive/260804-boabot-buzz-support/tasks.md`'s Phase F intro and F18/FR-027 row to state explicitly that only the reaction-subscription *shape guard* was built — no reaction publish, no `Monitor` subscribe/consume of `kind:7` — cross-referencing `implementation-notes.md`'s own manual-verification-list line ("no F1–F18 task has `Monitor` actually subscribe to reactions yet") as the precise framing to match.

## Edge Cases & Solutions

To be filled in during Step 9 as each workstream implements its fix — e.g. WS-B's exact generation-counter wraparound behavior (if any), WS-C's temp-file naming collision handling if `AcquireLock` is called concurrently by more than two acquirers, WS-D's exact `maxContentLen` value chosen and why.

## Deviations from Plan

None yet — record here if Step 9 implementation reveals any of this spec's task breakdown, architecture decisions, or research resolutions need revision (e.g. if WS-D's implementer finds a concrete operator need for `maxContentLen` to be `Monitor.Config`-tunable rather than a constant, contradicting AD-4's default).

## Lessons Learned

To be filled in at Step 10 (Archive Fixes Spec) / Step 12 (Process Analysis Report), following the original feature's pattern of capturing what worked and what didn't across the full run.
