# Status: BaoBot Buzz Support

**Feature:** boabot-buzz-support
**Created:** 2026-08-04
**Last Updated:** 2026-08-04

---

## Overall Progress

| Phase | Description | Status |
|---|---|---|
| Phase 0 | Initial Research (PRD) | ✅ Complete |
| Phase 1 | Specification (spec.md) | ✅ Complete |
| Phase 2 | Research & Data Modeling | ✅ Complete |
| Phase 3 | Architecture & Planning | ✅ Complete |
| Phase 4 | Task Breakdown | ✅ Complete (57 tasks across Phases A–I, `tasks.md`; all 54 FRs mapped) |
| Phase 5 | Implementation | In Progress (Phase A of A–I complete; see checklist below) |
| Phase 6 | Completion & Archival | Not Started |

## Phase 0 Task Checklist

- [x] Spec directory created (`specs/260804-boabot-buzz-support/`)
- [x] Research questions identified (see `research.md`)
- [x] Phase files initialized (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md)
- [x] PRD moved into spec directory

## Phase 5 Task Checklist (tasks.md, Phases A–I)

### Phase A — `TeamManager` seam fix
- [x] A1 — `monitors []domain.ChannelMonitor` added to `TeamManager`; registration setter appends to it instead of setting a dedicated `slackMonitor` field. FR-033.
- [x] A2 — Both result-forwarding call sites (orchestrator-mode and non-orchestrator-mode paths) rewritten to iterate `tm.monitors` via a new `forwardResultToMonitors` helper, calling `HandleResult` on each; the `if tm.slackMonitor != nil` branches are gone. FR-034.
- [x] Follow-up fix — `WithSlackMonitor(m *slackinfra.Monitor)` renamed to `WithChannelMonitor(m domain.ChannelMonitor)`, removing `internal/infrastructure/slack` from `team_manager.go`'s imports entirely. `grep -r "infrastructure/slack|infrastructure/buzz" internal/application` now returns **no matches** — FR-034's literal AC is satisfied, not just its spirit. `cmd/boabot/main.go`'s one call site and one internal test updated to match; `slackinfra.Monitor` already satisfied `domain.ChannelMonitor` per Phase A's compile-time assertion, so this was a pure rename with no behavior change. Commit `4dd772b`.

The Phase A brief had explicitly deferred this to a later phase, reasoning the AC as literally written ("`internal/application` imports no infrastructure package," full stop) was unachievable regardless since the package legitimately imports config/cliagent/github-backup/http/local-*/openai for other reasons. That broader reading was right, but the *narrower*, actually-load-bearing AC — the FR-034 grep scoped specifically to `infrastructure/slack|infrastructure/buzz` — was a small, achievable fix that didn't need to wait for Phase H's wiring work. Fixed immediately rather than carried as risk into Phase I's quality gate.

## Blockers

None currently. OQ-1 (multi-instance singleton) was resolved during PRD pre-flight as a process-level lock — see `implementation-notes.md`.

## Recent Activity

- 2026-08-04 — Spec directory created via `/create-spec boabot-buzz-support-PRD.md`, run as part of `/implm-frm-prd` Step 1.
- 2026-08-04 — All 8 phase files populated (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md); PRD moved into this directory. Step 1 complete.
- 2026-08-04 — `/review-spec` (Step 2) run. Initial verdict: Needs revision — scope contradictions between spec.md/plan.md on FR-025–027 and FR-047/048, an unresolved domain `Event`/`Filter` type decision that would have failed a PRD acceptance criterion, several undesigned edge cases (reply-publish failure, provider timeout, pending-map-across-reconnect, presence-during-disconnect, allowlist nil-vs-empty), and `tasks.md` being a stub. All gaps fixed directly in spec.md, data-dictionary.md, architecture.md, plan.md, implementation-notes.md, and a full 57-task breakdown written to tasks.md. Re-review verdict: Implementation-ready.
- 2026-08-04 — Phase A (tasks A1, A2) complete: `domain.ChannelMonitor` gained `HandleResult(ctx, TaskResultPayload)`; `slack.Monitor`'s existing (error-less) `HandleResult` signature was kept as-is and the interface matched to it rather than the reverse. `TeamManager`'s dedicated `slackMonitor *slackinfra.Monitor` field was replaced with `monitors []domain.ChannelMonitor`. Both result-forwarding call sites (orchestrator and non-orchestrator paths) now loop over a closure-captured snapshot of `tm.monitors` via a new `forwardResultToMonitors` helper — no more Slack-only `if != nil` branch. `internal/application/mocks.ChannelMonitor` gained a `HandleResult` method so it keeps satisfying the widened interface; `slack.Monitor` got a `var _ domain.ChannelMonitor = (*Monitor)(nil)` compile-time assertion. TDD followed throughout, including for the follow-up fix below. FR-033, FR-034. `go build`, `go vet`, `go test -race -count=1 ./...`, and `golangci-lint run` all pass.
- 2026-08-04 — Follow-up fix (commit `4dd772b`): `WithSlackMonitor(m *slackinfra.Monitor)` → `WithChannelMonitor(m domain.ChannelMonitor)`. Closes the FR-034 grep AC fully — see the Phase A checklist entry above for why this was worth fixing immediately rather than deferring.
