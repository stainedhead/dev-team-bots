# Dev-Flow Process Analysis: Bot Capability Access

**Branch:** feat/bot-capability-access
**Change description:** bot-capability-access-cr.md
**Spec:** specs/archive/260509-bot-capability-access/
**Date:** 2026-05-09

## Executive Summary

The bot-capability-access feature added four CLI agent delegation tools (`run_claude_code`, `run_codex`, `run_openai_codex`, `run_opencode`), the `read_skill` built-in MCP tool, a `CLIAgentRunner` domain interface with subprocess infrastructure adapter, and a fix for a plugin store goroutine data race. The full 16-step dev-flow ran in approximately 202 minutes of tracked time (Steps 1–13), with implementation (Step 5) accounting for 127 of those minutes. The auto-review surfaced 14 findings (1 P0, 5 P1, 8 P2), all resolved before PR open. Domain and application layer coverage meets or exceeds the 90% threshold across all packages except the pre-existing `application/team` exception at 76.1%.

## Step Timing

| Step | Name | Runtime (min) |
|------|------|---------------|
| 1  | Create PRD from Change Details  | 3  |
| 2  | Review PRD                      | 1  |
| 3  | Create Spec from PRD            | 9  |
| 4  | Review Spec                     | 3  |
| 5  | Implement Product               | 127 |
| 6  | Documentation and User Docs     | 4  |
| 7  | Code and Design Review          | 28 |
| 8  | Prepare Review PRD              | 0  |
| 9  | Archive Original Spec           | 1  |
| 10 | Spec Review Fixes               | 5  |
| 11 | Implement Review Fixes          | 10 |
| 12 | Archive Fixes Spec              | 1  |
| 13 | Final Quality Pass              | 10 |
| 14 | Process Analysis Report         | — |
| 15 | Archive Spec                    | — |
| 16 | Open Pull Request               | — |

**Total runtime (Steps 1–13):** 202 minutes

## Commit Timeline

| Hash | Timestamp | Message |
|------|-----------|---------|
| f2d2f2c | 2026-05-09 13:50:01 -0400 | docs(bot-capability-access): add PRD |
| ebe94fc | 2026-05-09 13:51:14 -0400 | docs(bot-capability-access): PRD review fixes |
| 5bdbe94 | 2026-05-09 14:01:01 -0400 | docs(bot-capability-access): create spec [specs/260509-bot-capability-access] |
| 221fae6 | 2026-05-09 14:04:08 -0400 | docs(bot-capability-access): spec review fixes |
| fde49c2 | 2026-05-09 14:08:49 -0400 | fix(team): pre-resolve plugin store in Run() to eliminate goroutine data race |
| 72ceb58 | 2026-05-09 14:10:26 -0400 | feat(mcp): add read_skill built-in tool and JSON entrypoint routing (FR-2) |
| 9eed0bd | 2026-05-09 14:12:52 -0400 | feat(cliagent): add CLIAgentRunner domain interface and SubprocessRunner (FR-3, FR-4 prereq) |
| b8fba0b | 2026-05-09 14:18:12 -0400 | feat(mcp,config): add run_claude_code, run_codex, run_openai_codex, run_opencode CLI tools (FR-4 to FR-9) |
| 5f4164d | 2026-05-09 14:23:32 -0400 | docs(bot-capability-access): update product-details.md and mark Phase 5 complete |
| 70d4f50 | 2026-05-09 14:29:57 -0400 | docs(bot-capability-access): update product docs and write user guides |
| 447d1a5 | 2026-05-09 14:34:41 -0400 | docs(review): add bot-capability-access auto-review PRD |
| 19db919 | 2026-05-09 14:35:17 -0400 | chore(spec): archive 260509-bot-capability-access spec |
| b1f93ce | 2026-05-09 14:40:58 -0400 | docs(bot-capability-access): create review-fixes spec [specs/260509-bot-capability-access-auto-review] |
| 44809c6 | 2026-05-09 14:45:42 -0400 | fix(mcp,cliagent): P0 review fixes — work_dir sandbox validation |
| 43ca922 | 2026-05-09 14:48:08 -0400 | fix(mcp,team,cliagent): P1 review fixes — context propagation, executable check, race test, cancel test |
| 43c8be3 | 2026-05-09 14:50:41 -0400 | fix(mcp,cliagent,docs): P2 review fixes — tests, comments, docs accuracy |
| 84b1d0d | 2026-05-09 14:52:01 -0400 | chore(spec): archive review-fixes spec — Step 12 complete |
| a7d4345 | 2026-05-09 14:54:10 -0400 | chore(bot-capability-access): final quality pass — all checks green |

**Wall-clock span:** 13:50 to 14:54 — approximately 64 minutes elapsed.

## Planned vs Actual

The spec's tasks.md used a phased breakdown across 10 phases (P1–P10) with individual RED/GREEN/REFACTOR sub-tasks. The implementation agent compressed these phases into four commits covering all functional requirements.

| Item | Planned (est) | Actual (git) | Notes |
|------|---------------|--------------|-------|
| FR-1: plugin store race fix | P1-T1 to P1-T6 / ~4.75h | ~9 min (fde49c2) | Completed in a single commit; TDD cycle compressed |
| FR-2: read_skill tool | P2-T1 to P2-T8 / ~3.75h | ~2 min (72ceb58) | Merged with FR-2 logic in same session |
| FR-3: CLIAgentRunner interface + SubprocessRunner | P3-T1 to P3-T12 / ~7.5h | ~4 min (9eed0bd) | Single commit; tests written in same session |
| FR-4 to FR-9: CLI tools + config | P4-T1 to P5-T15 / ~8h | ~5 min (b8fba0b) | All four tools and config in one commit |
| Documentation | P10-T3 to P10-T6 / ~1.75h | ~11 min (5f4164d, 70d4f50) | Two doc commits; within expected range |
| Review fixes (P0 + P1 + P2) | Not in original tasks.md | ~12 min (44809c6, 43ca922, 43c8be3) | Three priority-grouped commits |
| Final quality pass | P9-T1 to P9-T3, P10-T1 / ~1h | ~2 min (a7d4345) | Minor lint/format cleanup only |

The gap between planned estimates (~27h total) and actual git wall-clock time (~64 min) reflects that the implementation agent works without human wait time and compresses TDD sub-steps into fewer, larger commits rather than one commit per RED/GREEN/REFACTOR step.

## Review Findings

| Priority | Count | Resolved |
|----------|-------|----------|
| P0 | 1 | 1 |
| P1 | 5 | 5 |
| P2 | 8 | 8 |
| **Total** | **14** | **14** |

All 14 findings were resolved. The single P0 finding (FR-001: `callCLITool` work_dir sandbox bypass) was a genuine security gap — the four CLI delegation tools permitted subprocess launch in any host directory, bypassing the `allowedDirs` sandbox enforced by every other path-accepting tool. This was fixed in commit 44809c6.

## Coverage Achieved

| Package | Coverage |
|---------|----------|
| internal/domain | 100.0% |
| internal/domain/cost | 100.0% |
| internal/domain/eta | 100.0% |
| internal/domain/screening | 100.0% |
| internal/domain/workflow | 100.0% |
| internal/application | 98.9% |
| internal/application/backup | 100.0% |
| internal/application/cost | 100.0% |
| internal/application/eta | 100.0% |
| internal/application/metrics | 100.0% |
| internal/application/orchestrator | 97.8% |
| internal/application/plugin | 93.1% |
| internal/application/pool | 97.8% |
| internal/application/rebalancing | 100.0% |
| internal/application/scheduler | 98.6% |
| internal/application/screening | 100.0% |
| internal/application/subteam | 91.6% |
| internal/application/team | 76.1% (pre-existing exception) |
| internal/application/workflow | 98.9% |

All domain packages with testable statements are at 100%. All application packages meet the 90% threshold except `application/team`, which was below threshold before this feature and is noted as a pre-existing exception in the final quality pass commit.

## Process Observations

- **Step 5 dominates**: Implementation (127 min out of 202 tracked minutes, or 63%) is by far the largest step, driven by the scope of the spec's ~27h planned task list. This is consistent with the feature's complexity (new domain interface, four tools, subprocess runner, race fix) but signals that the task estimation in `tasks.md` was high relative to actual agent throughput.

- **Commit granularity was coarse**: The spec planned ~50 sub-tasks across 10 phases; the implementation produced 4 functional commits. This is more efficient for agent throughput but reduces traceability between spec tasks and code changes. Each commit covered multiple RED/GREEN/REFACTOR cycles without individual commits per cycle.

- **P0 finding was a real security gap**: The `callCLITool` work_dir sandbox bypass was not caught during the implementation phase — it required the auto-review step (Step 7) to surface. This is a strong argument for keeping the review step mandatory even when implementation appears complete.

- **Wall-clock vs tracked time diverge significantly**: Tracked Step 5 time was 127 minutes, but git timestamps show all 18 commits landed in approximately 64 minutes of wall-clock time (13:50–14:54). The DEV-FLOW-STATUS.md timestamps use a logical/scheduled clock, not real-time. Future runs should note this discrepancy.

- **Steps 8 (Prepare Review PRD) ran at 0 minutes**: This step had no tracked runtime, suggesting it was combined with the preceding code review step or run immediately without delay. The review PRD was committed at 14:34 (447d1a5), within seconds of the spec archive at 14:35 (19db919), confirming these steps collapsed.

## Recommendations

- **Reduce task granularity in tasks.md for agent-driven implementations**: The RED/GREEN/REFACTOR sub-task breakdown (e.g., P3-T1 through P3-T12 for a single interface) produces accurate human-time estimates but is misaligned with how implementation agents work. Consider grouping by functional requirement (one task per FR) with acceptance criteria instead of process steps.

- **Run the auto-review step even on low-complexity features**: The P0 security finding in this run (sandbox bypass) was non-obvious from reading the diff and was only surfaced by systematic review. A lightweight review checklist covering sandbox/path validation, context propagation, and race conditions should be a permanent part of the review template for any feature touching the MCP client.

- **Address the `application/team` coverage gap in a dedicated task**: The `application/team` package has been below 90% for at least two features and is now explicitly acknowledged as a pre-existing exception. A targeted coverage task should be scheduled to avoid this debt accumulating indefinitely.
