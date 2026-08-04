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
- [x] A1 — `monitors []domain.ChannelMonitor` added to `TeamManager`; `WithSlackMonitor` appends to it instead of setting a dedicated `slackMonitor` field. FR-033.
- [x] A2 — Both result-forwarding call sites (orchestrator-mode and non-orchestrator-mode paths) rewritten to iterate `tm.monitors` via a new `forwardResultToMonitors` helper, calling `HandleResult` on each; the `if tm.slackMonitor != nil` branches are gone. FR-034.

**Known deviation from tasks.md's stated AC for A1/A2:** both tasks' acceptance criteria say "`internal/application` imports no infrastructure package" / the grep `infrastructure/slack|infrastructure/buzz` against `internal/application` returns no matches. That grep still finds one line: `team_manager.go`'s import of `internal/infrastructure/slack` for `WithSlackMonitor(m *slackinfra.Monitor)`'s parameter type. This is intentional and was called out explicitly in this phase's task brief: generalizing `WithSlackMonitor`'s parameter type is out of scope for Phase A — the setter still takes a concrete `*slackinfra.Monitor`, it just appends to the shared `monitors` slice instead of a dedicated field. `internal/application/team` also already imports many other infrastructure packages unrelated to this change (config, cliagent, github/backup, http, local/*, openai), so the "imports no infrastructure package" AC as literally written was already unachievable before this phase started. Flagging here so Phase I's `I4` quality-gate re-run of this grep isn't a surprise — either the grep AC needs rescoping in `tasks.md`, or a later phase (H2, per the task brief) needs to actually remove the concrete Slack import from `WithSlackMonitor`'s signature.

## Blockers

None currently. OQ-1 (multi-instance singleton) was resolved during PRD pre-flight as a process-level lock — see `implementation-notes.md`.

## Recent Activity

- 2026-08-04 — Spec directory created via `/create-spec boabot-buzz-support-PRD.md`, run as part of `/implm-frm-prd` Step 1.
- 2026-08-04 — All 8 phase files populated (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md); PRD moved into this directory. Step 1 complete.
- 2026-08-04 — `/review-spec` (Step 2) run. Initial verdict: Needs revision — scope contradictions between spec.md/plan.md on FR-025–027 and FR-047/048, an unresolved domain `Event`/`Filter` type decision that would have failed a PRD acceptance criterion, several undesigned edge cases (reply-publish failure, provider timeout, pending-map-across-reconnect, presence-during-disconnect, allowlist nil-vs-empty), and `tasks.md` being a stub. All gaps fixed directly in spec.md, data-dictionary.md, architecture.md, plan.md, implementation-notes.md, and a full 57-task breakdown written to tasks.md. Re-review verdict: Implementation-ready.
- 2026-08-04 — Phase A (tasks A1, A2) complete: `domain.ChannelMonitor` gained `HandleResult(ctx, TaskResultPayload)`; `slack.Monitor`'s existing (error-less) `HandleResult` signature was kept as-is and the interface matched to it rather than the reverse. `TeamManager`'s dedicated `slackMonitor *slackinfra.Monitor` field was replaced with `monitors []domain.ChannelMonitor`; `WithSlackMonitor` now appends. Both result-forwarding call sites (orchestrator and non-orchestrator paths) now loop over a closure-captured snapshot of `tm.monitors` via a new `forwardResultToMonitors` helper — no more Slack-only `if != nil` branch. `internal/application/mocks.ChannelMonitor` gained a `HandleResult` method so it keeps satisfying the widened interface; `slack.Monitor` got a `var _ domain.ChannelMonitor = (*Monitor)(nil)` compile-time assertion. TDD followed: new tests in `internal/application/team/internals_test.go` (`TestForwardResultToMonitors_*`, `TestTeamManager_Monitors_*`, `TestTeamManager_WithSlackMonitor_AppendsToMonitors`) written first and confirmed red before implementation. FR-033, FR-034. `go build`, `go vet`, `go test -race -count=1 ./...`, and `golangci-lint run` all pass. See the deviation note above re: the A1/A2 "no infrastructure import" grep AC.
