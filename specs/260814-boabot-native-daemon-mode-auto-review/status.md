# Status: Boabot Native Daemon Mode — Code Review Fixes

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
| 5 | Implementation | Complete |
| 6 | Completion & Archival | Not Started |

## Phase 0 Task Checklist

- [x] Spec directory created (`specs/260814-boabot-native-daemon-mode-auto-review/`)
- [x] Review PRD reviewed (`/review-prd`) — verdict: Ready to proceed (guidance and P0 calibration both pass)
- [x] Research questions identified (see `research.md`) — findings are already precisely located by file/function in the review PRD, so "research" here is narrow: confirm current line numbers/behavior before each fix.
- [x] Phase files initialized (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md)

## Blockers

- None. All 10 findings are precisely scoped by the review; no open design questions beyond the 3 already resolved with recommendations in the review PRD's "Open Decisions" section.

## Phase 5 Task Checklist (10 findings)

- [x] T-FR101 — dedup marks "seen" only after dispatch succeeds. TDD: regression test added first (red), fix applied (green). `boabot/internal/application/orchestrator/buzz_task_bridge.go`, `buzz_task_bridge_test.go`.
- [x] T-FR102 — `buzzBoardTitle` rune-safe truncation. TDD: 3 regression tests added first (red), fix applied (green). Same files as above.
- [x] T-FR103 — nil-builder isolation test added. Test passed immediately against existing code (already correct) — pure coverage addition, confirmed via raw coverage profile (`team_manager.go:397.18,398.13` 0→1 hit count). `boabot/internal/application/team/team_manager_test.go`, `export_test.go`.
- [x] T-FR104 — Breaking Changes section in the archived spec now discloses the `chat_provider` behavior change explicitly. `specs/archive/260814-boabot-native-daemon-mode/spec.md`.
- [x] T-FR105 — corrected implementation-notes.md item #11 (archived spec dir) to state the accurate reason: CI already runs `-gcflags=all=-d=checkptr=0` and the whole module including `internal/infrastructure/buzz` is `-race`-clean under it; the test's placement at `internal/application/orchestrator` isolates the store-concurrency guarantee from relay-layer noise, not because the other layer "can't run under -race at all" (that claim was false).
- [x] T-FR106 — `boabot/README.md`'s Package Coverage table caption and root `AGENTS.md`'s coverage rule both now state explicitly that the 90% bar is an aggregate across the domain+application package set (matching CI's `boabot.yml` Coverage-check step), not a per-package minimum.
- [x] T-FR107 — `LoadTeamConfig` reverted to unexported `loadTeamConfig`. Repo-wide grep confirmed no caller outside `team_manager.go`'s own `Run()` and its own package-internal test.
- [x] T-FR108 — all 4 sub-items done in `specs/archive/260814-boabot-native-daemon-mode/`: (1) spec.md's 9 AC checkboxes checked with test references or left unchecked/partial with a one-line reason; (2) FR-008 reworded with a "Known Limitation" callout describing actual shipped scope; (3) implementation-notes.md's Deviations from Plan gained a `monitor.go` (+108/-17) deviation note; (4) data-dictionary.md gained a `TaskPayload.Source` entry.
- [x] T-FR109 — both adoption docs corrected: "Slack DMs" removed from the `chat_provider` doc line, replaced with an explicit note that Slack's dispatch path never tags a task's source, so `chat_provider` doesn't apply there today. Verified via `internal/infrastructure/slack/monitor.go`'s `dispatch` (no `Source` set) vs `internal/infrastructure/http/server.go`'s chat endpoints (do set `domain.DirectTaskSourceChat`).
- [x] T-FR110 — decided: accept `checkAndMarkSeen`'s (formerly `markSeenIfDuplicate`'s) full-map-scan-under-lock as-is; documented the bound and revisit condition in a code comment in `buzz_task_bridge.go`.

10/10 findings closed. Quality gates: `go fmt`/`go vet`/`golangci-lint run` clean, `go test -race -gcflags=all=-d=checkptr=0 ./...` all green, domain+application aggregate coverage 91.3% (CI's exact command), no regression from the review's baseline 91.2%.

One deviation beyond the original plan: FR-101's first-pass fix (peek-then-mark-on-success) was revised after an advisor review identified a logical (non-`-race`-visible) concurrent-duplicate risk; the final fix restores atomic check-and-set semantics via `unmarkEvent` on failure instead, plus a dedicated concurrency regression test. See implementation-notes.md for the full account.

## Recent Activity

- 2026-08-14: Spec directory created from `boabot-native-daemon-mode-auto-review-PRD.md`; PRD moved into spec directory.
- 2026-08-14: P1 batch (FR-101–FR-104) closed. TDD red-green for FR-101/FR-102; FR-103 test added (already-correct code, coverage-only); FR-104 doc disclosure added to archived spec.md.
- 2026-08-14: FR-101 hardened after advisor review flagged a logical concurrent-duplicate race in the first-pass fix; FR-107 and FR-110 closed in the same commit.
- 2026-08-14: All remaining doc-only findings (FR-105, FR-106, FR-108, FR-109) closed. 10/10 findings closed; spec ready for archival.
