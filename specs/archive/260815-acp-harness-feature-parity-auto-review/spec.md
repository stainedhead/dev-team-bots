# Spec: ACP Harness Feature Parity — Code Review Fixes

**Created:** 2026-08-15
**Status:** Draft
**Source PRD:** [acp-harness-feature-parity-auto-review-PRD.md](./acp-harness-feature-parity-auto-review-PRD.md)

## Executive Summary

Closes the 5 findings (1 P0 / 1 P1 / 3 P2) from the code-and-design review of the ACP harness feature parity work (`specs/archive/260815-acp-harness-feature-parity/`). Overall assessment was "Request changes" — a genuine P0 blocker, not a rubber-stamped pass. The P0 is a real cross-process concurrent-write hazard on `board.json`, reachable via `buzz-acp`'s own documented agent-pool operation (not a contrived scenario), that this feature's board-store wiring newly introduces. Must be resolved before this branch is considered mergeable.

## Problem Statement

`InMemoryBoardStore` (`internal/infrastructure/local/orchestrator/board.go`) has no cross-process concurrency protection — it reads its full state from disk once at construction, and every mutation does a full-file overwrite with no re-read, no lock. Before this feature, `board.json` had exactly one writer (native mode's `TeamManager`). This feature adds a second writer type: `boabot -acp`, which constructs its own `InMemoryBoardStore` at the same path formula. Critically, `buzz-acp` (the external harness driving ACP mode) runs a **persistent pool of long-lived agent processes** (per `docs/architectural-decision-record.md` ADR-B026, `--agents`/`--lazy-pool` flags) — meaning multiple `boabot -acp -agent orchestrator` processes with an identical `board.json` path can and do run concurrently by design, confirmed against this session's own observation of the live deployment (10 concurrent `boabot -acp -agent orchestrator` processes). Two independently-writing processes silently clobber each other's board state on the next write (last-writer-wins, full-file overwrite) — a real data-loss hazard on state the orchestrator dashboard depends on, not a hypothetical edge case.

Four lower-priority findings round this out: an inaccurate "exact mirror of team_manager.go" claim for plugin/CLI-tool config scope (P1 — ACP mode reads the running persona's own config, native mode reads the team's orchestrator entry's config and shares it team-wide); a field-precision nit in the board-store gate comparison (P2); an overstated "no differences from native mode" doc heading immediately contradicted by its own caveats (P2); and duplicated gating logic across two implementations with no compiler/test enforcement keeping them in sync (P2, matches existing precedent, not a new anti-pattern).

## Goals

- Close FR-1 (P0) — board.json must be safe under concurrent multi-process writes, or ACP mode's board activation must be constrained (pool size 1) with explicit, prominent documentation of why.
- Close FR-2 (P1) — correct the "exact mirror" claim's scope inaccuracy across every place it appears (spec.md, architecture.md, research.md, ADR-B030, adoption doc).
- Close as many of FR-3–FR-5 (P2) as practical; each is independent and low-risk.

## Non-Goals

- Not re-litigating the P0's severity or reachability — independently verified twice now (implementation-notes.md's own analysis, then the code review's independent re-derivation against the actual `buzz-acp` binary's documented pool behavior). This spec's job is to close it, not re-argue it.
- Not expanding scope beyond the 5 findings.
- Not attempting to replicate native mode's team-wide plugin/CLI-tool config sourcing in ACP mode (would require reading `team.yaml`, which the original feature's Non-Goals correctly rule out) — FR-2's fix is documentation accuracy, not a behavior change.

## User Requirements / Functional Requirements

**FR-1 (P0):** `board.json` is safe under concurrent writes from multiple processes sharing the same path. Concrete design (resolved during research, see `research.md` RQ1/RQ2): extract `internal/infrastructure/buzz/lock.go`'s atomic-publish/stale-check primitive (temp-file+`os.Link`, PID-liveness check — stdlib-only, portable) into a small reusable helper; wrap it in a retry-with-backoff loop (wait-for-lock, not fail-fast like the singleton lock) acquired around each `persist()` via a sibling `persistPath + ".lock"` path; combine with re-reading `persistPath` from disk immediately before each write and merging by item `ID` (union of disk ∪ in-memory, own touched item(s) win) rather than blindly overwriting with stale in-memory state. `Reorder`'s true concurrent-same-column-reorder race is a documented, accepted limitation, not solved by this fix.

**FR-2 (P1):** The "mirrors team_manager.go's exact condition" claim for plugin/CLI-tool config is corrected everywhere it appears (`spec.md`, `architecture.md`, `research.md`, ADR-B030, `ACP-Harness-Adoption-Config.md`) to state the actual scope: ACP mode reads the running persona's own `config.yaml`; native mode reads the team's orchestrator entry's `config.yaml` and shares it team-wide. A new adoption-doc bullet tells operators to copy `orchestrator.plugins.install_dir`/`orchestrator.cli_tools.*` into a non-orchestrator persona's own config if they want ACP mode to activate those tools for that persona.

**FR-3 (P2):** FR-402/architecture.md's board-gate "exact condition" language is softened to "equivalent by convention" (or a code comment on `buildACPMCPOptions` states the convention explicitly), since ACP mode compares `cfg.Bot.BotType` (the loaded persona's own field) where native mode compares `entry.Type` (the team.yaml entry's field) — equivalent today only because every real persona's `bot.type` matches its own directory name.

**FR-4 (P2):** `ACP-Harness-Adoption-Config.md`'s "No filesystem/tool differences from native mode" heading is reworded to not overclaim parity immediately before two (soon three, post-FR-2) caveats walk it back — e.g. "Same tool/provider mechanisms as native mode, with scope differences (see below)."

**FR-5 (P2, optional, no fix required to merge):** Document (or, if pursued, implement) extracting the three gating decisions (board/plugin/CLI-tool activation) into small, pure, shared functions both `team_manager.go` and `acp.go` can call, reducing the two-implementation drift risk. Matches existing precedent in this codebase (`newBuzzMonitorBuilder`), so explicitly not required to close this spec.

## Non-Functional Requirements

- **Reliability (the core of FR-1):** the fix must genuinely prevent silent data loss under real concurrent access, verified by a test that spins up two store instances against the same path, has each mutate independently, and asserts neither's write is silently discarded by the other.
- **Documentation accuracy:** FR-2/FR-3/FR-4 are "make the docs match reality" fixes — each claim corrected must be re-verified against current code, not just reworded to sound safer.
- **No regressions:** All existing tests, `-race` (with CI's `-gcflags=all=-d=checkptr=0` flag), `golangci-lint`, `go vet`, `gofmt` stay clean throughout. If FR-1's fix touches `InMemoryBoardStore`, it must not regress native mode's existing single-process board behavior.

## System Architecture

No new components for FR-2–FR-5 (documentation/wording only, except FR-5's optional refactor). FR-1's fix, if option (a) is pursued, touches `internal/infrastructure/local/orchestrator/board.go` (the shared store implementation both native mode and ACP mode use) — a genuinely shared-infrastructure fix, not an ACP-mode-only patch, which is appropriate since the underlying store type is shared.

## Scope of Changes

- Files to modify (FR-1, option a): `boabot/internal/infrastructure/local/orchestrator/board.go` and its tests.
- Files to modify (FR-1, option b, if (a) infeasible): `boabot/user-docs/ACP-Harness-Adoption-Config.md`.
- Files to modify (FR-2/FR-3/FR-4): `specs/archive/260815-acp-harness-feature-parity/spec.md`, `architecture.md`, `research.md`, `boabot/docs/architectural-decision-record.md` (ADR-B030), `boabot/user-docs/ACP-Harness-Adoption-Config.md`.
- Files to modify (FR-5, if pursued): `boabot/cmd/boabot/acp.go`, `boabot/internal/application/team/team_manager.go`.

## Breaking Changes

None expected. FR-1's fix (if option a) changes internal locking/consistency behavior of `InMemoryBoardStore`, not its public interface or on-disk format.

## Success Criteria and Acceptance Criteria

- [ ] FR-1: a new test constructs two `InMemoryBoardStore` instances sharing one `persistPath`, has each `Create`/`Update` a distinct item concurrently via goroutines (using a deterministic test-only hook to force the race window, per the `lock_race_test.go` template — not relying on scheduler luck), and asserts the final on-disk file contains both items. Test fails against current `persist()`, passes after the fix.
- [ ] FR-2: every occurrence of the inaccurate "exact mirror" claim corrected; new adoption-doc bullet added for plugin/CLI-tool scope.
- [ ] FR-3: board-gate language softened or code-commented.
- [ ] FR-4: adoption-doc heading reworded.
- [ ] FR-5: decision recorded (fix now, or explicitly deferred matching precedent) — not required to close.

**Quality gates:** `go fmt`, `go vet`, `golangci-lint run`, `go test -race -gcflags=all=-d=checkptr=0 ./...` all clean; no coverage regression on `internal/domain`/`internal/application` aggregate (currently 92.2%).

## Risks and Mitigation

| Item | Type | Notes | Mitigation |
|------|------|-------|------------|
| FR-1's technical fix touching shared `board.go` | Risk | This store is used by native mode too — a locking/re-read change must not regress or slow down native mode's existing single-process usage. | TDD with both single-process (existing) and multi-process (new) test coverage; run full suite including native-mode board tests after the change. |
| `Reorder`'s true concurrent-same-column-reorder race | Risk (accepted, not fixed) | A naive per-item merge doesn't resolve two processes reordering the same column concurrently — positions could collide/gap. | Documented as a known limitation in implementation-notes.md, not solved — matches FR-1's original scope (stop file-level clobbering, not full concurrent-edit semantics). |

## Timeline and Milestones

[TBD] — tracked via `status.md`.

## References

- Source PRD: [acp-harness-feature-parity-auto-review-PRD.md](./acp-harness-feature-parity-auto-review-PRD.md)
- Original feature spec (archived): `specs/archive/260815-acp-harness-feature-parity/`
- Prior codebase precedent for cross-process protection: ADR-B024 / FR-031 (Buzz private-key process-singleton lock) — same class of problem, already solved for a sibling resource.
