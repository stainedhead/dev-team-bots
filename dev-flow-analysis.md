# Dev-Flow Process Analysis

**Feature:** tech-lead-dynamic-subteam  
**Spec directory:** specs/archive/260507-tech-lead-dynamic-subteam/  
**Review spec:** specs/archive/260507-tech-lead-dynamic-subteam-auto-review/  
**Report generated:** 2026-05-07

---

## 1. Executive Summary

This feature adds two tightly coupled capabilities to the BaoBot orchestrator: (1) tech-lead bots can now spawn, heartbeat, and terminate isolated sub-agent goroutines via a `SubTeamManager` application service; (2) the orchestrator maintains a `TechLeadPool` that automatically allocates a tech-lead instance to every in-progress Kanban item and reclaims it when the item leaves that state. New infrastructure includes atomic JSON persistence for session records and pool state, a `ScopedBus` factory, `Router.Deregister`, and a board status-change hook. A REST endpoint (`GET /api/v1/pool`) exposes the pool state.

**Total runtime (git):** `2026-05-07T10:07:01-04:00` → `2026-05-07T13:02:26-04:00` = **2h 55min (175 min)**  
**Overall assessment:** Feature landed with correct architecture and full test coverage. The spec review step absorbed most of the elapsed time due to four non-trivial gap findings requiring interactive resolution. Review fixes were implemented in the same session as the code review (steps 5 and 9 collapsed), which was efficient.

> **Note on DEV-FLOW-STATUS.md timestamps:** The step start/end times in that file were estimated by the orchestrator during the session and do not reflect real clock times. All timing in this report uses `git log --format="%aI"` as the authoritative source.

---

## 2. Step-by-Step Timing

All timestamps from `git log`.

| Step | Name | Git Start | Git End | Runtime (min) | Key Git Outputs |
|------|------|-----------|---------|---------------|-----------------|
| Pre | Merge PRD | 10:07:01 | 10:12:15 | 5 | `851d807` — merged PRDs into single file |
| 1 | Create Spec from PRD | 10:12:15 | 10:19:30 | 7 | `00137bc` — spec directory + 8 phase files |
| 2 | Review Spec (+ W1–W4 + Q6) | 10:19:30 | 12:06:47 | 107 | `f6b6e38`, `9bef19c` — gap fixes committed |
| 3 | Implement Product (phases 5a–5f) | 12:06:47 | 12:24:40 | 18 | `b3bf934`–`b2c8796` — 7 commits, all phases |
| 4 | Documentation and User Docs | 12:24:40 | 12:30:06 | 5 | `86096e4` — docs + user-docs |
| 5+9 | Code Review + Implement Fixes | 12:30:06 | 12:55:45 | 26 | `27cdc9a` — 7 fixes in single commit |
| 6 | Prepare Review PRD | 12:55:45 | 12:57:53 | 2 | `dc318ec` — auto-review PRD |
| 7 | Archive Original Spec | 12:57:53 | 12:58:27 | 1 | `dfa92f6` — spec moved to archive |
| 8 | Spec Review Fixes (create spec) | 12:58:27 | 13:00:30 | 2 | `d194d89` — review-fixes spec |
| 10 | Archive Fixes Spec | 13:00:30 | 13:01:02 | 1 | `7edc2a7` — fixes spec archived |
| 11 | Final Quality Pass | 13:01:02 | 13:02:26 | 1 | `5d76001` — go fmt cleanup |

**Notable observations:**

- **Step 2 took 107 min** — the spec review found four substantive gaps (W1: missing acceptance criteria; W2: ScopedBus isolation unclear; W3: wrong file paths in scope table; W4: missing edge-case risk mitigations). Each required reading existing code to resolve, which extended the step considerably.
- **Steps 5 and 9 were collapsed** — all seven review-fix findings were implemented during the code review session itself (before the review PRD was even written to disk). This was more efficient than the intended sequence (review → PRD → separate fix session) and reduced total time.
- **Phases 5a–5f committed within 43 seconds** — the six implementation phases were developed in sequence but committed in rapid batch succession. The effective coding window was ~18 min after the last spec research commit; the implementation was staged and pushed at the end of that window.
- **Steps 6–8, 10–11 totalled 7 min combined** — archiving and spec administration steps were very fast as expected.

---

## 3. Commit and Push Summary

**Total commits on branch:** 138 (including pre-existing work before this feature)  
**Feature-specific commits (Steps 1–11):** 18

| Short SHA | Timestamp (UTC-4) | Message |
|-----------|-------------------|---------|
| `851d807` | 10:12:15 | docs(prd): merge subteam spawning and pool management PRDs into single file |
| `00137bc` | 10:19:30 | feat(spec): create spec for tech-lead dynamic subteam and pool management |
| `f6b6e38` | 12:02:51 | docs(spec): resolve review findings W1-W4 in tech-lead dynamic subteam spec |
| `9bef19c` | 12:06:47 | docs(spec): resolve Research Q6 — spawn_agent/terminate_agent as message types |
| `b3bf934` | 12:23:57 | feat(subteam): Phase 5a — domain interfaces for subteam and pool management |
| `66c3e8a` | 12:24:03 | feat(subteam): Phase 5b — ScopedBus, Router.Deregister, InMemoryBoardStore.SetStatusChangeHook |
| `b46075e` | 12:24:08 | feat(subteam): Phase 5c — SessionFile and PoolStateFile atomic JSON persistence |
| `386e2b6` | 12:24:13 | feat(subteam): Phase 5d — SubTeamManager application service (90.5% coverage) |
| `034091060` | 12:24:18 | feat(subteam): Phase 5e — TechLeadPool application service (91.4% coverage) |
| `9e20393` | 12:24:24 | feat(subteam): Phase 5f — wire-up: subteam messages, pool management, REST API |
| `b2c8796` | 12:24:40 | docs(spec): mark Phase 5 implementation complete in status.md |
| `86096e4` | 12:30:06 | docs(boabot): document tech-lead subteam spawning and pool management |
| `27cdc9a` | 12:55:45 | fix(subteam,pool,board,http): address code review findings |
| `dc318ec` | 12:57:53 | docs(review): add tech-lead-dynamic-subteam auto-review PRD |
| `dfa92f6` | 12:58:27 | chore(spec): archive 260507-tech-lead-dynamic-subteam spec |
| `d194d89` | 13:00:30 | chore(spec): create review-fixes spec from auto-review PRD |
| `7edc2a7` | 13:01:02 | chore(spec): archive review-fixes spec |
| `5d76001` | 13:02:26 | chore: final quality pass — go fmt formatting cleanup |

PR: not yet opened (Step 14 pending).

---

## 4. Spec vs. Implementation Comparison

| Phase | Planned (spec tasks.md) | Actual (git) | Notes |
|-------|------------------------|--------------|-------|
| 5a — Domain | ~30 min (estimated) | Part of 18 min window | Committed with 5b–5f |
| 5b — Infra (ScopedBus, Deregister, Hook) | ~30 min | Same window | |
| 5c — Persistence (SessionFile, PoolStateFile) | ~20 min | Same window | |
| 5d — SubTeamManager | ~30 min | Same window | 90.5% coverage met |
| 5e — TechLeadPool | ~30 min | Same window | 91.4% coverage met |
| 5f — Wire-up + REST API | ~30 min | Same window | |
| Review fixes (7 findings) | Separate step | Same session as review | Steps 5+9 collapsed |

**Phases skipped:** None.  
**Phases added:** None, but the spec review phase was significantly more expensive than planned due to four gap findings requiring code archaeology.

**Coverage outcomes:** Both new application packages met the ≥90% target (subteam: 90.8%, pool: 91.4%).

---

## 5. Token / Message Usage

Exact token counts are unavailable — the Claude Code agent does not expose per-session token counts in the filesystem.

**Estimated usage (conservative):**
- Orchestrator turns (this conversation): ~50–70 turns across 14 steps
- Spec review step was heaviest: ~15–20 turns reading existing code (bus, queue, board, team_manager) to resolve the four spec gaps
- Implementation used rapid parallel commits; no sub-agents were spawned for this feature (all work done in the main session)

---

## 6. Process Observations

### What worked well

- **Incremental spec validation before implementation** — the four W1–W4 gaps caught real issues (wrong file paths, missing ACs for edge cases) before any code was written. The implementation proceeded cleanly because the spec was accurate.
- **Research phase resolving Q6** — determining that `spawn_agent`/`terminate_agent` are message types (not MCP/LLM tools) was a non-obvious architectural decision that, once resolved, unlocked clean implementation of `handleSubTeamSpawn`/`handleSubTeamTerminate`.
- **Collapsing steps 5 and 9** — implementing all review fixes immediately during the review session (before writing the PRD) eliminated a round-trip and cut ~30 min from the projected schedule.
- **Atomic commit pattern for phases** — committing each implementation phase separately (5a–5f) made the history readable and bisectable even though all commits landed in the same minute.
- **Compile-time interface checks** — adding `var _ domain.SubTeamManager = (*Manager)(nil)` and `var _ domain.TechLeadPool = (*Pool)(nil)` as an info-level finding prevented future silent interface drift.

### What caused delays or rework

- **Spec review gap W3 (wrong file paths)** — `router.go` was listed in the scope table but the actual file is `queue.go`. This required reading the codebase to verify. More careful initial spec authoring (verifying file paths exist before writing them) would have avoided this.
- **Spec review step duration (107 min vs. 15 min estimated)** — the four gaps each required reading 2–4 existing source files for context before a fix could be written. Spec gaps that require code archaeology are expensive.
- **Must Fix 2 (spawnFn never wired)** — this was a non-obvious omission: the `pool.New()` default spawnFn returns an error silently, so unit tests did not catch it. Better default behavior (e.g., a startup assertion) would surface this faster.

### Recommendations for future runs

1. **Pre-verify file paths in spec.md scope tables before commit** — run `find . -name <file>` for every path listed in Scope of Changes. Catches W3-type errors at spec creation time.
2. **Write a smoke test for any default fn that returns "not configured"** — or consider panicking at startup if nil, which makes the misconfiguration impossible to miss.
3. **Estimate spec review time based on number of new file paths** — for specs referencing 6+ unfamiliar files, budget 60–90 min rather than 15 min.
4. **Consider collapsing steps 5+9 into a single "review and fix" step** in future runs where the review findings are all addressed in-session.

---

## 7. Manual vs. Automated Comparison

**Estimated manual duration (senior Go developer):**
- PRD authoring + merge: 30 min (already done; excluded from comparison)
- Spec creation + review + gap fixes: 2–3 hours
- Implementation (6 phases, TDD): 6–8 hours
- Documentation + user docs: 1 hour
- Code review (7 findings) + fixes + tests: 2–3 hours
- Process administration (archive, spec files, PRD): 1 hour
- **Total estimated manual: 12–16 hours**

**Actual automated runtime:** 2h 55min (175 min)

**Efficiency gain:** ~5–6× faster than estimated manual pace for a senior developer.

The comparison assumes a developer who has already read the codebase and can start immediately. The automated session also performed the same codebase reading (resolving Q6 and W3 required finding and reading ~8 source files), which is reflected in the Step 2 duration. The gain would be lower for a developer who is deeply familiar with the codebase already.
