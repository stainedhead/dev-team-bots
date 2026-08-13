# Dev-Flow Process Analysis

**Feature:** boabot-acp-stdio-harness-support
**Spec directory:** specs/archive/260813-boabot-acp-stdio-harness-support (implementation), specs/archive/260813-boabot-acp-stdio-harness-support-auto-review (review-fix cycle)
**Report generated:** 2026-08-13

---

## 1. Executive Summary

Built `internal/infrastructure/acp/`, a thin adapter implementing `github.com/coder/acp-go-sdk`'s `acp.Agent` interface over BaoBot's existing `domain.Worker`, so a single BaoBot persona can be registered as a `buzz-acp` custom harness (Agent Client Protocol over stdio) — an alternative, lighter-weight distribution path to the already-merged native Buzz `ChannelMonitor` integration. Includes a real end-to-end integration test against the compiled binary, a new ADR (ADR-B026) explaining why this doesn't contradict the earlier ADR-B020 rejection of running native Buzz support under `buzz-acp`, and a full 9-finding independent-review fix cycle (including a reproduced, then fixed, data race).

**Total runtime:** ~1h 13m, first commit to last (`ddc40f0` at 11:18:40 to `828f4ba` at 12:31:13, 2026-08-13, all timestamps local -04:00 per `git log`). This spans PRD authoring through the review-fix cycle's final quality pass — everything through Step 10 of the 12-step flow at the time of this report (Steps 11-12 are in progress/pending).

**Overall assessment:** Fast, and — notably — the process caught its own real bugs twice: once via the independent code-review step (Step 4) finding a genuine, reproduced data race that the implementer's own tests had missed, and once via the implementer independently re-verifying (twice) a documentation claim about `domain.BudgetTracker` that turned out to be wrong in a subtler way than first corrected. Both are exactly the kind of defect a single-pass implementation-without-review would very plausibly have shipped.

---

## 2. Step-by-Step Timing

Git commit timestamps are authoritative (per this report's own instructions) and override the hand-recorded times in `DEV-FLOW-STATUS.md`, which were written progressively as approximations, not measured precisely.

| Step | Name | Start (first commit) | End (last commit) | Runtime (min) | Key Outputs |
|---|---|---|---|---|---|
| 0 (pre-flow) | PRD authoring, spec creation, research, architecture, task breakdown | `ddc40f0` 11:18:40 | `1bbd55e` 11:25:22 | ~7 | PRD, spec.md, research.md (confirmed `coder/acp-go-sdk` exists + `buzz-acp`'s real protocol via `strings`), architecture.md, tasks.md (T1-T8) |
| 1 | Verify Spec Initialized | — | `8295bc4` 11:26:22 | <1 | Confirmed all 8 phase files present |
| 2 | Implement Product (T1-T8) | `0215672` 11:26:46 | `1fd209c` 12:04:14 | ~38 | `internal/infrastructure/acp/` (agent/session/turn + tests), `cmd/boabot/acp.go`, `-acp` flag, real subprocess integration test, mid-flight FR-005 correction (twice) |
| 3 | Documentation and User Docs | (folded into Step 2's last commit) | `1fd209c` 12:04:14 | ~0 (done alongside T8) | ADR-B026, technical-details.md, product docs, README, `user-docs/ACP-Harness-Adoption-Config.md` |
| 4 | Code and Design Review | `49caf6c` 12:04:56 | `9b9399d` 12:13:41 | ~8.75 | Independent reviewer (fresh, non-fork agent) found 1 reproduced data race, 2 more P0s, 3 P1s, 3 P2s |
| 5 | Prepare Review PRD | `9b9399d` (base) | `466d706` 12:14:41 | ~1 | Self-review against standard PRD dimensions; added a missing open question and agent-teammate guidance |
| 6 | Archive Original Spec | `92a8b4b` 12:15:07 | `890b1f6` 12:15:11 | ~0.1 | `specs/archive/260813-boabot-acp-stdio-harness-support/` |
| 7 | Spec Review Fixes | `a87ab59` 12:15:20 | `f8a1b12` 12:16:47 | ~1.5 | `specs/260813-boabot-acp-stdio-harness-support-auto-review/` (9 findings → 8 tasks, P0-P2) |
| 8 | Implement Review Fixes | `888010d` 12:17:04 | `0b17565` 12:28:54 | ~11.8 | RT1 (race fix via `turnMu`), RT2 (logging), RT3 (RulesTracker), RT4 (resolved as RT1 side effect), RT5 (real `CloseSession` + bounded map), RT6 (`go mod tidy`), RT7 (CI wiring), RT8 (stdin-EOF test) |
| 9 | Archive Fixes Spec | `18e8bf7` 12:29:02 | `521e520` 12:29:10 | ~0.1 | `specs/archive/260813-boabot-acp-stdio-harness-support-auto-review/` |
| 10 | Final Quality Pass | `d052343` 12:29:18 | `828f4ba` 12:31:13 | ~2 | Full `-race` suite (with the documented `checkptr` workaround), 91.0% domain/application coverage, lint clean |
| 11 | Process Analysis Report | in progress | — | — | This document |
| 12 | Open Pull Request | pending | — | — | — |

**Notable observations:**
- Step 2 (38 min) is the dominant cost, as expected for the actual TDD implementation of a new package plus a real subprocess integration test — but it includes real research-in-the-loop (reading the ACP SDK's source directly, not just `go doc`, and reading `team_manager.go`'s `startBot` to get construction primitives exactly right) that a pure "write code" estimate wouldn't capture.
- Step 2 also includes a genuine mid-flight correction, twice: a first implementation attempt (not separately tracked in this git history — it ran into a harness isolation issue and produced no commits) discovered `domain.BudgetTracker` doesn't exist as `boabot/AGENTS.md` describes it; a second pass then discovered *that* correction was itself over-broad (`internal/domain/cost`/`internal/application/cost` DO exist, just unwired) and fixed it again. Both corrections are visible as separate commits (`8a90210`, `c7d736a`).
- Step 4's independent review (8.75 min) is disproportionately valuable relative to its cost: it found a data race that took under 9 minutes to identify but would have shipped a real production concurrency bug if skipped.
- Steps 5, 6, 7, 9 are all near-instantaneous — they're process/bookkeeping steps (self-review, `git mv`, spec-directory scaffolding), not implementation work, and the timing reflects that correctly.

---

## 3. Commit and Push Summary

**Total commits on `feat/boabot-acp-stdio-harness` vs. `main`:** 36
**Files changed:** 41 (+2,684 / -78 lines, per `git diff main...HEAD --shortstat`)

Full commit list (chronological, local time):

| Commit | Timestamp | Message |
|---|---|---|
| `ddc40f0` | 2026-08-13T11:18:40-04:00 | docs(spec): PRD and spec for BaoBot ACP stdio harness support |
| `1bbd55e` | 2026-08-13T11:25:22-04:00 | docs(spec): complete research, architecture, and task breakdown for ACP harness |
| `8295bc4` | 2026-08-13T11:26:22-04:00 | chore: start dev-flow implementation for boabot-acp-stdio-harness-support |
| `0215672` | 2026-08-13T11:26:46-04:00 | chore: Step 2 (Implement Product) started |
| `8a90210` | 2026-08-13T11:38:07-04:00 | docs(spec): correct FR-005 — domain.BudgetTracker doesn't exist |
| `0e085d7` | 2026-08-13T11:42:54-04:00 | refactor(team): export NewLocalProviderFactory, add acp-go-sdk dep |
| `58752ec` | 2026-08-13T11:48:25-04:00 | feat(acp): implement ACP Agent adapter over the existing Worker (T2-T6) |
| `7285a9e` | 2026-08-13T11:53:04-04:00 | feat(cmd): wire boabot -acp mode into main.go (T1) |
| `6af9e61` | 2026-08-13T11:53:59-04:00 | docs(spec): update status.md and implementation-notes.md for T1-T6 |
| `28971a1` | 2026-08-13T11:58:07-04:00 | test(acp): real end-to-end integration test against the compiled binary (T7) |
| `c7d736a` | 2026-08-13T12:00:27-04:00 | docs(spec): correct an over-broad FR-005 claim about budget enforcement |
| `1e7d6bc` | 2026-08-13T12:01:35-04:00 | docs(spec): the buzz checkptr issue is ADR-B020's already-documented one, not new |
| `1fd209c` | 2026-08-13T12:04:14-04:00 | docs(boabot): document the ACP stdio harness mode (T8) |
| `8191032` | 2026-08-13T12:04:41-04:00 | docs(spec): mark Phase 5 (Implementation) complete, 8/8 tasks |
| `49caf6c` | 2026-08-13T12:04:56-04:00 | chore: Steps 2-3 complete, Step 4 (Code and Design Review) started |
| `9b9399d` | 2026-08-13T12:13:41-04:00 | docs: Step 4 code review PRD (auto-review) — 3 P0, 3 P1, 3 P2 findings |
| `466d706` | 2026-08-13T12:14:41-04:00 | docs: Step 5 — resolve review-PRD gaps (open question, agent-teammate guidance) |
| `92a8b4b` | 2026-08-13T12:15:07-04:00 | docs(spec): mark Phase 6 (Completion & Archival) complete |
| `890b1f6` | 2026-08-13T12:15:11-04:00 | chore: archive specs/260813-boabot-acp-stdio-harness-support (Step 6) |
| `a87ab59` | 2026-08-13T12:15:20-04:00 | chore: Step 6 complete, Step 7 (Spec Review Fixes) started |
| `f8a1b12` | 2026-08-13T12:16:47-04:00 | docs(spec): Step 7 — spec directory for auto-review fixes (9 tasks, P0-P2) |
| `888010d` | 2026-08-13T12:17:04-04:00 | chore: Step 7 complete, Step 8 (Implement Review Fixes) started |
| `7907a68` | 2026-08-13T12:18:54-04:00 | fix(acp): serialize turn execution to fix a reproduced data race (RT1/FR-001) |
| `f0d5f04` | 2026-08-13T12:19:39-04:00 | test(acp): prove RT4/FR-005 (session-cancel re-entrancy) resolved by turnMu |
| `d7767ed` | 2026-08-13T12:21:34-04:00 | feat(acp): add turn-level logging; make the doc's log-line claim real (RT2/FR-002/FR-003) |
| `33c2ed4` | 2026-08-13T12:21:50-04:00 | docs(spec): update review-fixes status — P0s + RT4 done (4/8) |
| `a5e2040` | 2026-08-13T12:23:26-04:00 | feat(acp): wire RulesTracker to match native mode's construction (RT3/FR-004) |
| `7e0703b` | 2026-08-13T12:26:11-04:00 | feat(acp): real CloseSession + bounded session map (RT5/FR-006) |
| `c5b8432` | 2026-08-13T12:26:36-04:00 | chore(deps): go mod tidy — coder/acp-go-sdk is a direct dependency (RT6/FR-007) |
| `176a10b` | 2026-08-13T12:27:11-04:00 | ci(boabot): run the ACP integration test in CI (RT7/FR-008) |
| `1d7792c` | 2026-08-13T12:28:09-04:00 | test(acp): prove clean exit on stdin EOF at the binary level (RT8/FR-009) |
| `0b17565` | 2026-08-13T12:28:54-04:00 | docs(spec): Step 8 complete — all 8 review fixes (9 findings) done |
| `18e8bf7` | 2026-08-13T12:29:02-04:00 | chore: Step 8 complete, Step 9 (Archive Fixes Spec) started |
| `521e520` | 2026-08-13T12:29:10-04:00 | chore: archive specs/260813-boabot-acp-stdio-harness-support-auto-review (Step 9) |
| `d052343` | 2026-08-13T12:29:18-04:00 | chore: Step 9 complete, Step 10 (Final Quality Pass) started |
| `828f4ba` | 2026-08-13T12:31:13-04:00 | chore: Step 10 (Final Quality Pass) complete — stable |

All commits pushed to `origin/feat/boabot-acp-stdio-harness` incrementally throughout the run. PR: opened in Step 12 (see the PR itself for its URL).

---

## 4. Spec vs. Implementation Comparison

| Phase | Planned (spec) | Actual (git log) | Difference | Notes |
|---|---|---|---|---|
| Research | Confirm Go ACP SDK exists, confirm `buzz-acp`'s real protocol | ~7 min, via `strings` on the real binary + reading the SDK's actual source | On plan | Both open questions resolved with concrete evidence, not assumption |
| Architecture | Thin adapter, no domain changes | Matched exactly | On plan | `internal/infrastructure/acp` imports only `domain` + the SDK — verified by the independent reviewer too |
| Implementation (T1-T8) | 8 tasks | 8 tasks, ~38 min | On plan, but with 2 unplanned mid-flight spec corrections | FR-005 was corrected twice as real budget-enforcement code was discovered mid-implementation, not anticipated at architecture time |
| Testing | Unit tests per task, real-binary integration test for T7 | Delivered, plus a `//go:build integration` test tag decision (not in the original plan, but consistent with repo convention) | On plan | T7 explicitly could not exercise the real `buzz-acp` binary (no dry-run mode, requires a live relay) — scoped down to the real *compiled boabot* binary instead, documented as a deliberate tradeoff |
| Review-fix cycle | Not part of the original spec — added after independent review | 9 findings, 8 tasks, ~11.8 min | Unplanned but expected (dev-flow's own Steps 4-9 exist for exactly this) | All P0/P1/P2 items closed before Step 10; none deferred |

**Phases skipped:** None.
**Phases added:** The entire review-fix cycle (Steps 4-9) — not a deviation, this is dev-flow's designed self-correction mechanism, and it worked as intended (caught a real bug).

---

## 5. Token / Message Usage

Exact token counts are not captured by this session's tooling. Rough qualitative estimate based on step complexity:
- Orchestrator (this session): the majority of turns — most implementation, all fix work, and all process-management steps ran in the main conversation directly rather than via sub-agents, after an early sub-agent attempt hit a harness isolation limitation (a pinned sub-agent's working directory matched the target worktree but the isolation flag wasn't set, and the harness's own `EnterWorktree` call refused to re-enter a path already equal to the current working directory — a real gap in the isolation tooling for this specific pre-existing-worktree scenario, not a flaw in this dev-flow run's execution).
- Two sub-agent calls were used successfully: the independent code review (Step 4, a fresh non-fork agent, ~48 tool calls per its own report) and earlier research (ACP protocol/SDK investigation, a fork, ~9 tool calls).
- Given the harness isolation issue forced most implementation work into the main conversation rather than parallelized sub-agents, this run's total token usage is almost certainly higher than a run where sub-agent delegation worked cleanly throughout — a process cost worth flagging for future runs in this same repo/worktree configuration.

---

## 6. Process Observations

### What worked well
- **The independent-review step earned its cost.** Step 4's fresh-context reviewer found a real, reproducible data race that the implementer's own extensive test suite (12 tests, all passing, `-race`-clean) had not caught — because no test exercised concurrent sessions on one `Agent`, which is exactly the scenario the architecture's own design (multi-session, long-lived, pooled processes) implies is possible. This is the single clearest value-add in the whole run.
- **TDD caught bugs during implementation, not just at review.** Writing `go test -race` for the keep-alive mechanism surfaced an ordering bug (the keep-alive goroutine could outlive `Prompt`'s return) before it ever reached review — a second, independent line of defense.
- **Verifying claims instead of trusting them, twice over.** The `domain.BudgetTracker` finding was corrected once during implementation, then corrected *again* when a broader grep revealed the first correction was itself incomplete. The independent reviewer then verified both claims a third time, independently, and found them accurate. This repeated-verification pattern is exactly what caught a subtle, easy-to-miss inaccuracy that would otherwise have shipped in permanent documentation (an ADR).
- **Real subprocess integration testing, not just mocks.** T7 and the review-fix cycle's RT8 both build and drive the actual compiled binary rather than testing only in-process fakes — this is what let RT8 turn "very likely correct" (the reviewer's own words about the stdin-EOF behavior) into "verified correct."

### What caused delays or rework
- **Sub-agent isolation limitation.** An early attempt to delegate implementation to a sub-agent failed twice: first because the sub-agent narrated instead of acting (a coordination/prompt-clarity problem, fixed by a corrective message), then because of a genuine harness limitation — a sub-agent whose working directory already matched the target git worktree could not satisfy the isolation precondition, and neither could the coordinator itself (`EnterWorktree` refuses to "enter" a path that's already the cwd). This forced a shift to direct implementation in the main conversation, which likely increased total token cost even though it didn't block progress.
- **Two rounds of correction on the same finding (FR-005/budget enforcement).** Not wasted effort exactly — each correction was itself grep-verified and accurate at the time — but it's a sign that "no such interface exists" claims deserve a broader search (not just the specific name mentioned in stale documentation) before being stated as fact the first time.

### Recommendations for future runs
- When working in a pre-existing worktree (not one created via this session's own `EnterWorktree` call), expect sub-agent delegation for file-writing work to be unreliable, and default to direct implementation rather than losing time to isolation-error retries.
- When a "does X exist in this codebase" finding is used to justify scoping something out, grep broadly (by concept/behavior, not just by the exact name a stale doc uses) before writing it into permanent documentation like an ADR.

---

## 7. Manual vs. Automated Comparison

**Estimated manual duration:** 2-3 days for an experienced Go engineer unfamiliar with the ACP protocol — roughly: 0.5 day reading the ACP spec and the `buzz-acp` binary's actual behavior (most engineers wouldn't think to `strings` a bundled binary to verify the CLI's real protocol coverage rather than trusting docs), 1 day implementing and unit-testing the adapter with the concurrency edge cases this feature actually has, 0.5 day writing a real subprocess integration test rather than settling for pure mocks, 0.5-1 day for a human code review round-trip (scheduling, review turnaround, addressing comments) equivalent to this run's Steps 4-9.

**Actual automated runtime:** ~1h 13m, first commit to last, through Step 10.

**Efficiency gain:** Roughly 25-40x on wall-clock time for the engineering work itself, with the important caveat that this compares to a *careful* manual estimate that includes writing the same depth of tests and the same rigor of independent review this run actually performed — not a rushed, unreviewed implementation. The comparison basis assumes a human engineer with Go and Clean Architecture familiarity but no prior ACP exposure, working focused (not counting context-switching, meetings, or multi-day review latency a real team would add on top of the 2-3 day estimate above).
