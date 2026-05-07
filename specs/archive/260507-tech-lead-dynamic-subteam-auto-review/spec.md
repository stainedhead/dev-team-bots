# Spec: Tech-Lead Dynamic Subteam — Auto-Review Fixes

**Feature:** tech-lead-dynamic-subteam-auto-review  
**Created:** 2026-05-07  
**Source PRD:** `specs/260507-tech-lead-dynamic-subteam-auto-review/tech-lead-dynamic-subteam-auto-review-PRD.md`

---

## Executive Summary

Seven correctness, security, and maintainability findings were identified during automated code review of the `feat/tech-lead-dynamic-subteam` branch. This spec drives the implementation of fixes for all seven findings. Two are P0 blockers (unauthenticated pool endpoint, unwired pool spawn function), two are P1 correctness issues (board hook under write lock, terminated agent re-spawn), and three are P1/P2 hardening items (TearDownAll context, stale session records, compile-time interface checks). All fixes follow TDD and Clean Architecture.

---

## Problem Statement

The `feat/tech-lead-dynamic-subteam` implementation contains security and correctness defects discovered during code review that must be resolved before the branch can merge. The most critical: the pool endpoint is publicly accessible without authentication, and the TechLeadPool's spawn function is never wired in production, meaning no tech-lead instances would ever be spawned for in-progress board items.

---

## Goals / Non-Goals

**Goals:**
- Fix all 7 review findings before the branch merges.
- Each fix has a corresponding passing test.
- No Clean Architecture violations introduced.

**Non-Goals:**
- Refactoring unrelated code in the same files.
- Changing domain interfaces beyond what findings require.
- Addressing the pre-existing `TestProvider_Timeout` failure in `codeagent`.

---

## User Requirements

### FR-001 — Pool endpoint requires authentication (P0 — security)

`GET /api/v1/pool` must be protected by `s.auth()` middleware. Unauthenticated callers must receive HTTP 401.

**Acceptance Criteria:**
- AC-001a: `GET /api/v1/pool` returns 401 without a valid Bearer token.
- AC-001b: `GET /api/v1/pool` returns 200 with pool data with a valid token.
- AC-001c: `TestPool_Endpoint_RequiresAuth` exists and passes.

### FR-002 — TechLeadPool spawn function wired in production (P0 — correctness)

`TeamManager.Run()` must call `techLeadPool.SetSpawnFn`, `SetStopFn`, and `SetIsRunningFn` with real implementations before `Reconcile()`. Allocating a new pool entry must start a real tech-lead goroutine.

**Acceptance Criteria:**
- AC-002a: `TeamManager` implements `spawnTechLead`, `stopTechLead`, `isTechLeadRunning`.
- AC-002b: `Run()` wires all three functions before `Reconcile()`.
- AC-002c: Spawned tech-lead goroutines tracked in `dynamicBots`, cleaned up on cancel.
- AC-002d: `orchestratorName` stored as `TeamManager` field for use by callbacks.

### FR-003 — Board hook called outside write lock (P1 — correctness)

`InMemoryBoardStore.Update()` must release `s.mu` before invoking `statusChangeHook` to prevent future deadlocks.

**Acceptance Criteria:**
- AC-003a: `Update()` captures hook under lock, releases lock, then invokes hook.
- AC-003b: Existing `TestInMemoryBoardStore_StatusChangeHook_*` tests pass.

### FR-004 — Terminated agents can be re-spawned by name (P1 — correctness)

`Manager.Spawn()` must allow re-use of a name whose previous agent has terminated.

**Acceptance Criteria:**
- AC-004a: `Spawn()` with a previously terminated name succeeds.
- AC-004b: `TestManager_Spawn_TerminatedAgent_CanBeRespawned` exists and passes.
- AC-004c: Spawning an active (non-terminated) agent still returns an error.

### FR-005 — TearDownAll uses context.Background() for waits (P1 — correctness)

`TearDownAll`'s per-goroutine wait must use `context.Background()` as base, not the potentially-cancelled caller context.

**Acceptance Criteria:**
- AC-005a: `context.WithTimeout(context.Background(), remaining)` used in wait loop.
- AC-005b: Existing `TearDownAll` tests pass.

### FR-006 — Stale session records cleared at startup (P2 — correctness)

`WithSessionFile()` must discard pre-existing records with a warning log.

**Acceptance Criteria:**
- AC-006a: Pre-existing records cleared and logged on `WithSessionFile()` call.
- AC-006b: `TestManager_WithSessionFile_ClearsStaleRecords` exists and passes.

### FR-007 — Compile-time interface conformance assertions (P2 — maintainability)

`application/subteam.Manager` and `application/pool.Pool` must have `var _ domain.X = (*Y)(nil)` assertions.

**Acceptance Criteria:**
- AC-007a: `var _ domain.SubTeamManager = (*Manager)(nil)` in `manager.go`.
- AC-007b: `var _ domain.TechLeadPool = (*Pool)(nil)` in `pool.go`.
- AC-007c: Both compile without errors.

---

## Non-Functional Requirements

- All new tests use `t.Parallel()`.
- Coverage on `application/subteam` and `application/pool` must remain ≥90% after fixes.
- `go vet ./...` and `golangci-lint run` must pass with zero issues.

---

## System Architecture

**Affected layers:**
- Infrastructure adapter: `internal/infrastructure/http/server.go`
- Infrastructure adapter: `internal/infrastructure/local/orchestrator/board.go`
- Application service: `internal/application/subteam/manager.go`
- Application service: `internal/application/pool/pool.go`
- Application service: `internal/application/team/team_manager.go`

**No new packages** — all fixes are in existing files.

---

## Scope of Changes

| File | Change |
|------|--------|
| `boabot/internal/infrastructure/http/server.go` | Add `s.auth()` to pool route |
| `boabot/internal/infrastructure/http/server_test.go` | Add `TestPool_Endpoint_RequiresAuth`; update no-pool test |
| `boabot/internal/infrastructure/local/orchestrator/board.go` | Release lock before hook call in `Update()` |
| `boabot/internal/application/subteam/manager.go` | Fix `Spawn` duplicate check, `WithSessionFile` stale clearing, `TearDownAll` context, interface check |
| `boabot/internal/application/subteam/manager_test.go` | Add re-spawn and stale record tests |
| `boabot/internal/application/pool/pool.go` | Add interface check |
| `boabot/internal/application/team/team_manager.go` | Add `orchestratorName`, `dynamicBots`, pool lifecycle methods, wire `SetSpawnFn`/`SetStopFn`/`SetIsRunningFn` |

---

## Breaking Changes

None — all changes are internal. The pool endpoint now requires auth (previously a bug, not a feature).

---

## Success Criteria

- All 7 FRs resolved with corresponding passing tests.
- `go test -race ./...` passes (excluding pre-existing `codeagent` failure).
- `golangci-lint run` passes with 0 issues.
- `application/subteam` and `application/pool` coverage ≥90%.

---

## Risks and Mitigation

| Risk | Mitigation |
|------|-----------|
| `dynamicBots` map concurrent access | Guarded by `dynamicBotsMu sync.Mutex` |
| Pool wiring changes team_manager tests | Export stubs in `export_test.go` isolate real I/O |

---

## References

- Source PRD: `specs/260507-tech-lead-dynamic-subteam-auto-review/tech-lead-dynamic-subteam-auto-review-PRD.md`
- Original spec: `specs/archive/260507-tech-lead-dynamic-subteam/`
