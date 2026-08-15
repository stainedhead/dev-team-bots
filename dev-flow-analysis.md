# Dev-Flow Process Analysis

**Feature:** ACP Harness Feature Parity
**Spec directory:** specs/archive/260815-acp-harness-feature-parity/ (+ specs/archive/260815-acp-harness-feature-parity-auto-review/)
**Report generated:** 2026-08-15

---

## 1. Executive Summary

Closed two of three confirmed wiring gaps between `boabot -acp` (ACP mode) and native daemon mode's task execution — both share the identical `ExecuteTaskUseCase.Execute` loop, but ACP mode never got `models.chat_provider` benefit and had zero board/plugin/CLI-tool infrastructure standing up in its process. This PRD originated from a live production observation (the `orchestrator` persona repeatedly hitting `exceeded max tool iterations (50)` via ACP mode but not native mode) diagnosed via direct code comparison earlier in this session, then closed via the full dev-flow pipeline.

**Total runtime:** ~1h 19m (first dev-flow commit `7316ae8` at 10:36:57-04:00 to the Step 11 completion commit `506627f` at 11:56:12-04:00, per git log).
**Overall assessment:** The shortest of the three dev-flow runs in this session by wall-clock, but the most consequential code review — this is the first cycle across all three features where the review found a genuine P0, not a documentation gap or minor correctness nit. The P0 (a cross-process concurrent-write hazard on `board.json`) was reachable "by construction" via `buzz-acp`'s own documented agent-pool operation, not a contrived scenario, and was confirmed independently twice: once by the implementing agent's own risk analysis, and again by the code review's independent re-derivation against the actual `buzz-acp` binary's documented behavior.

---

## 2. Step-by-Step Timing

(Source: `DEV-FLOW-STATUS.md`, cross-checked against git commit timestamps.)

| Step | Name | Start (UTC) | End (UTC) | Runtime (min) | Key Outputs |
|---|---|---|---|---|---|
| 1 | Create Spec from PRD | 14:34:49 | 14:36:43 | 2 | `specs/260815-acp-harness-feature-parity/` created, 8 phase files + PRD moved in |
| 2 | Review Spec | 14:36:43 | 14:41:16 | 5 | All 4 research questions resolved; most consequential: `orchestrator.enabled` deliberately NOT reused as the tool-wiring signal (confirmed it only means "start the dashboard" and doesn't gate board wiring in native mode either) |
| 3 | Implement Product | 14:41:16 | 15:06:23 | 25 | 6 tasks (FR-401–406) implemented TDD-first, 3 commits; caught and fixed a scope gap (`WithChatProvider` was never wired in ACP mode at all, making the literal FR-401 fix inert without it) |
| 4 | Documentation and User Docs | 15:06:23 | 15:08:02 | 2 | README coverage table refreshed (found stale from *before* the prior feature), ACP-mode bullets updated in README/product-summary |
| 5 | Code and Design Review | 15:08:02 | 15:23:00 | 15 | 5 findings (1 P0/1 P1/3 P2) — first genuine P0 in this session's three review cycles, independently re-derived against `buzz-acp`'s documented process-pool behavior |
| 6 | Prepare Review PRD | 15:23:00 | 15:24:20 | 1 | Review PRD validated — no gaps found, P0 calibration confirmed justified |
| 7 | Archive Original Spec | 15:24:20 | 15:24:56 | 1 | Original spec moved to `specs/archive/` |
| 8 | Spec Review Fixes | 15:24:56 | 15:29:53 | 5 | Fix-spec created; the P0's fix mechanism resolved concretely via a dedicated research pass (reuse `lock.go`'s primitive, not a new `flock` dependency) before implementation began |
| 9 | Implement Review Fixes | 15:29:53 | 15:55:03 | 25 | All 5 findings closed, 2 commits; TDD genuinely followed — the regression test was confirmed failing deterministically before the fix, not assumed |
| 10 | Archive Fixes Spec | 15:55:03 | 15:55:45 | 1 | Fix-spec moved to `specs/archive/` |
| 11 | Final Quality Pass | 15:55:45 | 15:56:01 | 0 | Independent re-verification, including manually reverting and restoring the P0 fix to confirm the regression test's red/green states were genuine, not just claimed |

**Notable observations:**
- Step 3 and Step 9 (25 min each) are tied as the largest steps, unusual — in the prior two features, implementation (Step 3) was consistently larger than the review-fix step (Step 9). Here they're equal because Step 9 had to implement genuinely new infrastructure (a file-locking package) from scratch, not just wire existing pieces together, unlike the prior features' review-fix cycles which were mostly documentation and narrower code changes.
- Step 5 (Code Review, 15 min) found a P0 despite being the shortest review step of the three cycles in this session (the DM/thread feature's review took 11 min for 6 findings including deep crypto verification; the multi-agent Buzz feature's took 19 min for 10 findings). Review duration doesn't correlate with finding severity — this cycle's reviewer moved efficiently to the load-bearing question (is the board-write collision actually reachable in production?) rather than spreading effort evenly across surface area.
- No step failed or required a full retry. The P0 was caught, scoped, and closed within the normal Step 5→9 flow — the pipeline's design (independent review before merge) worked exactly as intended: catching a real production-data-loss bug before it shipped, not after.

---

## 3. Commit and Push Summary

**Total commits (Steps 1–11):** 14

| Commit | Timestamp | Message |
|---|---|---|
| `7316ae8` | 2026-08-15T10:36:57-04:00 | docs: create spec from acp-harness-feature-parity PRD (dev-flow Step 1) |
| `af4ca01` | 2026-08-15T10:41:31-04:00 | docs: resolve spec review gaps with concrete codebase research (dev-flow Step 2) |
| `3fcab64` | 2026-08-15T10:57:58-04:00 | feat(boabot): wire chat provider, board/plugin/CLI tools into ACP mode (FR-401-405) |
| `4f32eab` | 2026-08-15T11:01:13-04:00 | docs(boabot): document ACP mode tool/provider parity and the mid-task-question non-goal (FR-406) |
| `5808807` | 2026-08-15T11:04:26-04:00 | fix(boabot): plugin e2e test now exercises the plugin-tool dispatch path; document per-persona board scope |
| `894e7da` | 2026-08-15T11:08:15-04:00 | docs(boabot): refresh README coverage table, note ACP tool/provider parity (dev-flow Step 4) |
| `a013bae` | 2026-08-15T11:23:27-04:00 | docs: add code-and-design review PRD for acp-harness-feature-parity (dev-flow Step 5) |
| `fc778be` | 2026-08-15T11:24:55-04:00 | chore: archive original acp-harness-feature-parity spec (dev-flow Step 7) |
| `8fbbad8` | 2026-08-15T11:30:10-04:00 | docs: create and resolve spec for review-fix implementation (dev-flow Step 8) |
| `eba1ca7` | 2026-08-15T11:45:10-04:00 | fix(boabot): make board.json safe under concurrent multi-process writes |
| `ef1edb0` | 2026-08-15T11:51:48-04:00 | docs(boabot): correct ACP-vs-native config-scope claims; record T-FR5 defer |
| `cadcccf` | 2026-08-15T11:55:14-04:00 | docs: update dev-flow dashboard — Step 9 complete, Step 10 starting |
| `a8c9f79` | 2026-08-15T11:55:41-04:00 | chore: archive review-fixes spec (dev-flow Step 10) |
| `506627f` | 2026-08-15T11:56:12-04:00 | docs: update dev-flow dashboard — Step 11 final quality pass complete |

All 14 commits pushed to `feat/acp-harness-feature-parity` immediately after creation. No PR opened yet — that is dev-flow Step 14, not yet run at the time of this report.

---

## 4. Spec vs. Implementation Comparison

| Phase | Planned (spec) | Actual (git log) | Difference | Notes |
|---|---|---|---|---|
| Research | 4 research questions (RQ1–RQ4) | All 4 resolved concretely, including a consequential correction to the PRD's own assumption (do NOT reuse `orchestrator.enabled`) | On scope, exceeded expected rigor | The research step actively improved on the PRD's stated plan rather than just filling in blanks — the PRD had assumed reuse was the likely path; research found the opposite and the spec was corrected before implementation started |
| Implementation | 6 tasks (FR-401–406) | All 6 completed, plus one documented scope addition (wiring `WithChatProvider`, not named in FR-401's literal text but required to make it non-inert) | Slightly exceeded planned scope, for a correctness reason | Mirrors the exact same class of finding as ADR-B028 (the original multi-agent Buzz feature's `chat_provider` dead-code fix) — the implementing agent recognized the pattern and closed the gap rather than shipping a technically-compliant-but-inert fix |
| Review Fixes | Not originally planned (emerges from Step 5) | 5 findings, 1 P0 + 4 lower priority, all 5 closed | Added scope (expected) — but this is the first cycle in this session where that added scope included genuinely new infrastructure (a file-locking package), not just fixes to existing code | The P0's resolution required building something that didn't exist before (extracted from an existing sibling primitive, not invented from scratch) — a larger review-fix undertaking than either prior feature's cycle |
| Security/reliability verification | Not separately planned; folded into Step 5's review dimensions | The reviewer explicitly traced reachability against `buzz-acp`'s actual documented process-pool behavior (ADR-B026) rather than treating the finding as merely theoretical | Exceeded planned scope | This is the payoff of the "independently verify claims, don't just trust" instruction pattern established across all three of this session's review cycles — it's what turned a plausible-sounding architectural risk into a confirmed, prioritized P0 with a concrete reachability path |

**Phases skipped:** None. **Phases added:** A dedicated, focused research pass specifically for the P0's fix mechanism (between Step 8 and Step 9's implementation), narrower in scope than a full spec-review research pass but following the same pattern — resolving "what's the right technical approach" concretely before implementation began, rather than leaving it for the implementing agent to figure out mid-fix.

---

## 5. Token / Message Usage

Exact orchestrator-level token counts unavailable; sub-agent usage from task notifications, as a rough proxy for effort distribution:

| Step | Sub-agent(s) | Approx. tokens (sub-agent) | Tool calls |
|---|---|---|---|
| 2 (research) | 1 research agent | ~64k | 19 |
| 3 | 1 implementation agent | ~261k | 182 |
| 5 | 1 review agent | ~181k | 61 |
| 8 (P0 fix-mechanism research) | 1 research agent | ~69k | 14 |
| 9 | 1 implementation agent | ~244k | 125 |

Step 3 and Step 9 are again the largest sub-agent consumers, consistent with the timing observations in Section 2 — they're the only steps writing production Go code, and this cycle's Step 9 involved building genuinely new infrastructure rather than smaller targeted fixes.

---

## 6. Process Observations

### What worked well
- **The review step's "independently verify, don't trust" instruction pattern paid off decisively this cycle.** The P0 finding depended on connecting two facts that were each individually documented but never previously connected: (1) `board.go` has no cross-process protection, and (2) `buzz-acp` runs a process pool per ADR-B026. Neither fact alone was new information — the review's value was in the connection, made possible by instructing the reviewer to verify reachability concretely rather than accept "this seems like it could be a problem" as sufficient.
- **The implementing agent's own risk analysis (in the original feature's implementation-notes.md) had already partially flagged this exact class of concern** ("multiple ACP-mode processes... running concurrently") before the review step ran — it was documented as an accepted risk at the time, correctly per that step's narrower scope, but the connection to `buzz-acp`'s actual pool behavior wasn't made until the dedicated review step. This shows the multi-stage pipeline catching what a single implementation pass, however careful, reasonably deprioritized within its own scope.
- **The P0's fix was scoped precisely to the actual bug**, not over-engineered: full concurrent-edit-of-the-same-item semantics (e.g., true conflict resolution for `Reorder`) were explicitly and correctly ruled out of scope, since the actual production risk was file-level clobbering of *unrelated* items, not fine-grained edit conflicts. Solving only the real problem kept the fix's blast radius small.
- **The fix reused an existing primitive (`lock.go`) rather than introducing a new dependency**, preserving this codebase's deliberate stdlib-only/cross-platform-portability constraint — a constraint that wouldn't have been visible without the dedicated research pass reading `lock.go`'s own doc comments.

### What caused delays or rework
- Nothing that reads as delay or rework in the negative sense — the "delays" in this cycle (the dedicated P0 fix-mechanism research pass, the two-attempt verification of the regression test's red state) are exactly the deliberate, valuable slowness a P0 correctness fix warrants, not inefficiency.
- One minor process note: the implementing agent's own advisor review (within Step 9) caught two problems before commit — an adoption-doc claim that contradicted an adjacent existing bullet, and a hook-based test that didn't actually verify contention on its first attempt. Both were caught and fixed within the same step, but represent a first-pass imperfection a second self-review pass corrected — consistent with this session's now-repeated pattern (also seen in both prior features) of self-review catching real issues before external review does.

### Recommendations for future runs
- Given this cycle demonstrated the review step's independent-verification instruction catching a genuine P0 by connecting two previously-separate documented facts, consider making "cross-reference this finding against every existing ADR that might be relevant" a standing instruction for code review steps generally, not just as an ad hoc addition — it was pivotal here and cost little extra to request.
- The pattern of "research the fix mechanism concretely before implementing" (used for this P0, and for the original feature's harder design questions) consistently produces higher-confidence implementation passes with fewer mid-implementation surprises — worth continuing as a standing practice for any P0/P1 finding, not just optionally.

---

## 7. Manual vs. Automated Comparison

**Estimated manual duration:** 2–3 working days for a senior Go engineer — this feature is narrower in scope than the prior two (no new protocol, no new cryptography), but the P0's discovery and correct resolution would itself typically consume a meaningful fraction of a manual review cycle: recognizing that a documented process-pool behavior (in one part of the system) interacts badly with a documented lack of concurrency protection (in another part) is exactly the kind of cross-cutting concern that's easy to miss in a single-reviewer pass under time pressure, and the fix (extracting and adapting an existing locking primitive correctly, with genuine TDD) is not trivial work.

**Actual automated runtime:** ~1h 19m for Steps 1–11, continuous, no waiting on human review turnaround, including the full P0 discovery-to-fix cycle within that time.

**Efficiency gain:** Comparable to or exceeding the prior two features' estimates on a pure wall-clock basis, but the more notable result this cycle is *catching a real bug before merge* that a less rigorous process — automated or manual — might plausibly have missed, given it required connecting two separately-documented facts rather than spotting an obvious local defect. This is a qualitative outcome the efficiency ratio alone doesn't capture: the pipeline's value here wasn't just speed, it was catching something a faster-but-shallower process would likely have shipped.
