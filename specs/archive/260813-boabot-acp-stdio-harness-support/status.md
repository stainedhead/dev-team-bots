# Status: BaoBot ACP Stdio Harness Support

**Feature:** boabot-acp-stdio-harness-support
**Created:** 2026-08-13

## Overall Progress

| Phase | Status |
|---|---|
| Phase 0 — Initial Research | Complete |
| Phase 1 — Specification | Complete |
| Phase 2 — Research & Data Modeling | Complete |
| Phase 3 — Architecture & Planning | Complete |
| Phase 4 — Task Breakdown | Complete |
| Phase 5 — Implementation | Complete (8/8 tasks) |
| Phase 6 — Completion & Archival | Complete |

## Phase 5 Task Checklist

- [x] T1 — Dependency + skeleton (incl. provider_factory export-rename)
- [x] T2 — Handshake (`initialize`, `session/new`)
- [x] T3 — Core turn execution (`session/prompt`)
- [x] T4 — Keep-alive + `session/cancel`
- [x] T5 — Usage field scoped down (no enforcement wired into task path — corrected FR-005)
- [x] T6 — Process lifecycle + panic recovery
- [x] T7 — Real integration test against the compiled binary (not the real `buzz-acp` binary — see implementation-notes.md for why)
- [x] T8 — Documentation (ADR-B026 + docs + user-docs)

## Blockers

None currently. `buzz-acp --mcp-command` semantics remain unresolved (research.md) but are non-blocking for T1–T8.

## Recent Activity

- 2026-08-13: T7 (real subprocess integration test against the compiled binary) and T8 (ADR-B026, technical-details.md, product docs, README, new user-docs/ACP-Harness-Adoption-Config.md) complete. Phase 5 (Implementation) is done, 8/8 tasks.
- 2026-08-13: Corrected an over-broad claim in the FR-005 fix: `internal/domain/cost.CostEnforcer` / `internal/application/cost.EnforceBudgetUseCase` DO exist in this codebase, fully built and tested — grep for `NewEnforceBudgetUseCase` confirms it's simply never constructed/wired into `team_manager.go`'s task path (in either native or ACP mode). "No BudgetTracker-equivalent exists at all" was wrong; "no budget enforcement is wired into the live task path" is the accurate statement, and behaviorally the v1 decision (leave `Usage` nil, don't wire enforcement here) is unchanged. All spec docs corrected to the precise phrasing.
- 2026-08-13: T1–T6 implemented (`internal/infrastructure/acp/{agent,session,turn}.go`, `cmd/boabot/acp.go`, `main.go`'s `-acp` flag). All new/changed code passes `go build`, `go vet`, `golangci-lint`, and `go test -race` (for the acp/team/cmd packages specifically); full `go test ./...` (non-race) is green across the whole repo. Found and fixed a real data race in the keep-alive goroutine's lifecycle during TDD (Prompt didn't wait for it to fully exit before returning) — see turn.go. Coverage on the new acp package is 76.4% (two 0% spots are trivial one-line `cmd/`-style wiring, consistent with AGENTS.md excluding `cmd/` from the coverage gate). Discovered a genuine pre-existing bug, unrelated to this feature: `internal/infrastructure/buzz` has a `checkptr` fatal error under `-race` in the `fiatjaf.com/nostr` dependency (confirmed via zero diff against `origin/main` in that package) — not fixed here, flagged in implementation-notes.md.
- 2026-08-13: Implementation attempt #1 discovered `domain.BudgetTracker` does not exist anywhere in this codebase (grep-verified twice, independently) — `boabot/AGENTS.md`'s description of it is aspirational, not real. FR-005 and all dependent docs (data-dictionary.md, architecture.md, plan.md, tasks.md) corrected: v1 leaves `PromptResponse.Usage` nil; real budget enforcement is out of scope, flagged as a separate follow-up. Also found and documented the real `Worker` construction path (`application.NewExecuteTaskUseCase` + `WithProgressHandler`) and required construction primitives, and a small safe export-rename needed in `provider_factory.go` — folded into T1/architecture.md.

- 2026-08-13: PRD authored, spec directory created, all phase files initialized.
- 2026-08-13: Research completed — confirmed `github.com/coder/acp-go-sdk` as the Go ACP SDK, confirmed `buzz-acp`'s real method set and persistent pooled-process lifecycle via `strings` on the actual binary.
- 2026-08-13: Architecture and data-dictionary finalized — thin adapter over existing `Worker`/`BudgetTracker`, no new domain interfaces. FR-003 refined from "incremental streaming" (not buildable — `Worker.Execute` has no streaming callback) to "keep-alive updates + final result," which also turned out to be a correctness requirement for `buzz-acp`'s idle-timeout, not just a UX nicety.
- 2026-08-13: Task breakdown (T1–T8) written in tasks.md. Beginning implementation.
