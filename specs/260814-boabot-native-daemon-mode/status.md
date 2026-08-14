# Status: Boabot Native Daemon Mode (Multi-Agent Buzz Support)

**Created:** 2026-08-14
**Last Updated:** 2026-08-14

## Overall Progress

| Phase | Description | Status |
|-------|-------------|--------|
| 0 | Initial Research (PRD/Feature Research) | Complete |
| 1 | Specification (spec.md) | Complete |
| 2 | Research & Data Modeling | Complete |
| 3 | Architecture & Planning | Complete |
| 4 | Task Breakdown | Complete |
| 5 | Implementation | Complete (P1.0-P3.2; P4.1 out of scope — operator action, no code) |
| 6 | Completion & Archival | Not Started |

## Phase 0 Task Checklist

- [x] Spec directory created (`specs/260814-boabot-native-daemon-mode/`)
- [x] PRD reviewed (`/review-prd`) — verdict: Ready for spec
- [x] Research questions identified (see `research.md`)
- [x] Phase files initialized (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md)

## Phases 2-4 Task Checklist

- [x] RQ1 (`DirectTaskSource` value) resolved — `DirectTaskSourceBuzz`, additive, `execute_task.go:100` updated per design
- [x] RQ2 (`buildBuzzMonitor` per-bot factory shape) resolved — loop in `run()`, team-entry loading exposed from `team_manager.go`
- [x] RQ3 (JSON-store concurrency) resolved — already `sync.RWMutex`-protected, no hardening needed
- [x] RQ4 (NL scheduling mechanism) resolved — reuse `ParseScheduleNL`/`ChatTaskManager`
- [x] RQ5 (second demo persona) narrowed — 3 personas already `buzz:`-configured (`orchestrator`, `architect`, `tech-lead`); final pick is an operator decision, does not block implementation
- [x] `data-dictionary.md`, `architecture.md` populated with concrete findings
- [x] `tasks.md` populated with 9-task Phase 1-4 breakdown, TDD-first
- [x] Edge Cases section added to `spec.md` (duplicate registration, incomplete config, relay replay, cancel race, malformed schedule text)
- [x] DM (direct message) scope question raised mid-review — resolved as Non-Goal, documented in PRD/spec/research

## Blockers

- None currently. Remaining open item (exact current Buzz→`Worker.Execute` call site) is scoped as first implementation task (P1.0), not a spec-blocking unknown.

## Recent Activity

- 2026-08-14: Spec directory created from `boabot-native-daemon-mode-PRD.md`; PRD moved into spec directory.
- 2026-08-14: `/review-spec` run; codebase research resolved RQ1-4 concretely, narrowed RQ5; spec.md, research.md, data-dictionary.md, architecture.md, tasks.md, plan.md updated with findings. Spec now implementation-ready.
- 2026-08-14: Step 3 implementation started. P1.0 traced and documented in implementation-notes.md (exact current Buzz -> Worker.Execute path). P2.1 done (`DirectTaskSourceBuzz` domain value + `execute_task.go:100` provider-selection check extended, with a supporting `TaskPayload.Source` fix required to make the check reachable at all -- see implementation-notes.md). P2.2 done (`BuzzTaskBridge` application-layer use case in `internal/application/orchestrator/buzz_task_bridge.go`, reusing `ChatTaskManager` for NL scheduling, with event-ID dedup). `go test -race`, `go vet`, `golangci-lint run`, `gofmt` all clean at this checkpoint. Commit `d771f75` pushed.
- 2026-08-14: Step 3 implementation completed. P1.2 (`team.LoadTeamConfig` exported), P1.1 (`Router.Lookup` added; `TeamManager.WithBuzzMonitorBuilder` hook invoked from `Run()` after shared stores exist, before any bot goroutine starts; `buildBuzzMonitor` reworked to use `Lookup`-before-`Register`; `main.go`'s single process-wide Buzz block replaced with the builder-closure registration; a real latent `router.Register` double-registration panic -- pre-existing, exposed by this feature, not introduced by it -- fixed via idempotent pre-registration in `Run()`), Buzz `Monitor.dispatch()` now routes through the bridge via a new `WithTaskDispatcher` option (direct-queue path kept as an opt-out fallback, not removed, to avoid rewriting ~20 pre-existing Monitor unit tests unrelated to the bridge), `boardTracksSource` extension so Buzz-originated board items update on task completion (P2.2 completion), P2.3 (multi-persona no-cross-talk `-race` test at the `internal/application/orchestrator` layer -- see implementation-notes.md item 11 for why not in `internal/infrastructure/buzz`), P3.1/P3.2 (satisfied entirely by `ChatTaskManager` reuse -- see implementation-notes.md item 8 for the scope resolution on "update/cancel"). All 9 tasks (P1.0-P3.2) complete; P4.1 out of scope (operator action). Final gates: `gofmt`, `go vet`, `golangci-lint run` all clean; `go test ./...` all green; `go test -race ./...` green everywhere except a confirmed pre-existing, unrelated third-party (`fiatjaf.com/nostr`) `checkptr` crash in `internal/infrastructure/buzz` (reproduced on the pre-feature base commit in an isolated worktree -- see implementation-notes.md item 10).
