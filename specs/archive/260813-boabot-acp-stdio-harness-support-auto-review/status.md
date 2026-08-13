# Status: BaoBot ACP Stdio Harness Support — Auto-Review Fixes

**Feature:** boabot-acp-stdio-harness-support-auto-review
**Created:** 2026-08-13

## Overall Progress

| Phase | Status |
|---|---|
| Phase 0 — Initial Research | Complete (review findings are the research) |
| Phase 1 — Specification | Complete |
| Phase 2 — Research & Data Modeling | Complete |
| Phase 3 — Architecture & Planning | Complete |
| Phase 4 — Task Breakdown | Complete |
| Phase 5 — Implementation | Complete (8/8 tasks) |
| Phase 6 — Completion & Archival | Not Started |

## Phase 5 Task Checklist

- [x] RT1 (FR-001, P0) — data race fixed via `turnMu` serialization; proven with `TestAgent_Prompt_ConcurrentSessionsDoNotRace`.
- [x] RT2 (FR-002 + FR-003, P0) — turn-level `slog` logging added; doc's log-line claim now real, verified against the compiled binary.
- [x] RT3 (FR-004, P1) — `RulesTracker` wired, mirroring native mode exactly.
- [x] RT4 (FR-005, P1) — resolved as a side effect of RT1; proven with a dedicated test.
- [x] RT5 (FR-006, P1) — real `CloseSession` + bounded session map (FIFO eviction, default 10,000).
- [x] RT6 (FR-007, P2) — `go mod tidy`; `coder/acp-go-sdk` now direct.
- [x] RT7 (FR-008, P2) — ACP integration test now runs in CI (`.github/workflows/boabot.yml`).
- [x] RT8 (FR-009, P2) — stdin-EOF clean-exit proven with a real subprocess test.

## Blockers

None. All P0/P1/P2 findings resolved.

## Recent Activity

- 2026-08-13: All 8 fixes complete. Full regression suite green (`go build`/`go vet`/`golangci-lint`/`go test ./...` and `-race -gcflags=all=-d=checkptr=0 ./...`); new/changed code additionally passes `-race` on its own. Ready for final quality pass.
- 2026-08-13: RT1/RT2/RT4 (all P0, plus one P1 resolved as a side effect) complete. Moving to remaining P1s (RT3, RT5).
- 2026-08-13: Spec directory created from the auto-review PRD (9 findings: 3 P0, 3 P1, 3 P2).
