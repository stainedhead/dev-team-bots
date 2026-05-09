# Tech-Lead Dynamic Subteam — Auto-Review PRD

**Source branch:** feat/tech-lead-dynamic-subteam  
**Review date:** 2026-05-07  
**Overall assessment:** Approve with minor comments — all Must Fix and Warning findings have been resolved in commit `27cdc9a`. Remaining items are informational observations already addressed or trivially low risk.

---

## Executive Summary

The implementation is well-structured and follows Clean Architecture throughout. Domain entities, application services, and infrastructure adapters are cleanly separated. Test coverage on the two new application packages (`subteam`, `pool`) is 90.8% and 91.4% respectively, meeting the ≥90% target. The implementation identified six issues during review — two security/correctness Must Fixes and four Warnings — all of which were resolved by the time this PRD was finalised. No P0 items remain open.

---

## Scope

**In scope:** The 7 correctness, security, and maintainability findings listed below. Each has a corresponding test.

**Non-Goals / Out of Scope:**
- Refactoring unrelated code in the same files
- Changing domain interfaces beyond what the findings require
- Addressing the pre-existing `TestProvider_Timeout` failure in `internal/infrastructure/codeagent` (requires a live `claude` CLI subprocess)
- Architectural changes beyond what each finding prescribes

---

## Open Questions

All open questions resolved — no blocking unknowns remain.

---

## Functional Requirements

### FR-REVIEW-001 — Pool endpoint must require authentication

**Priority:** P0 (blocker — security)  
**Status:** ✅ Resolved (commit `27cdc9a`)

`GET /api/v1/pool` was registered without the `s.auth()` middleware wrapper, unlike every other non-login endpoint. An unauthenticated caller could enumerate the tech-lead pool state, revealing active bot names and item IDs.

**Acceptance Criteria:**
- [ ] `GET /api/v1/pool` returns HTTP 401 when called without a valid Bearer token.
- [ ] `GET /api/v1/pool` returns HTTP 200 with pool data when called with a valid token.
- [ ] A regression test `TestPool_Endpoint_RequiresAuth` exists and passes.

**Resolution:** Changed `mux.HandleFunc("GET /api/v1/pool", s.handlePoolList)` to `mux.HandleFunc("GET /api/v1/pool", s.auth(s.handlePoolList))` and added `TestPool_Endpoint_RequiresAuth`.

---

### FR-REVIEW-002 — TechLeadPool spawn function must be wired in production

**Priority:** P0 (blocker — correctness)  
**Status:** ✅ Resolved (commit `27cdc9a`)

`pool.New()` returns a `Pool` whose `spawnFn` defaults to returning `fmt.Errorf("pool: no spawnFn configured")`. `TeamManager.Run()` created the pool but never called `pool.SetSpawnFn(...)`. Every `Allocate` call in production that required a new instance would silently log an error and return no pool entry, so in-progress board items would never get a tech-lead assigned.

**Acceptance Criteria:**
- [ ] `TeamManager` implements `spawnTechLead(ctx, instanceName)`, `stopTechLead(ctx, instanceName)`, and `isTechLeadRunning(ctx, instanceName)` methods.
- [ ] `TeamManager.Run()` calls `techLeadPool.SetSpawnFn`, `SetStopFn`, and `SetIsRunningFn` before `Reconcile`.
- [ ] Spawned tech-lead goroutines are tracked in `dynamicBots` and cleaned up on context cancellation.
- [ ] `orchestratorName` is stored as a `TeamManager` field so spawn callbacks can use it.

**Resolution:** Added `orchestratorName string`, `dynamicBots map[string]*dynamicBot`, and three pool lifecycle methods to `TeamManager`. Wired them in `Run()` before `Reconcile()`.

---

### FR-REVIEW-003 — Board status-change hook must not be called under write lock

**Priority:** P1 (high — correctness)  
**Status:** ✅ Resolved (commit `27cdc9a`)

`InMemoryBoardStore.Update()` used `defer s.mu.Unlock()`, which meant the `statusChangeHook` was called while the write lock was held. The hook calls `techLeadPool.Allocate/Deallocate`, which is safe today, but any hook that reads from the board (e.g., for audit logging) would deadlock. This is a latent bug that would manifest as a deadlock under future hook implementations.

**Acceptance Criteria:**
- [ ] `Update()` releases `s.mu` before invoking the hook.
- [ ] Existing hook tests (`TestInMemoryBoardStore_StatusChangeHook_*`) continue to pass.

**Resolution:** Changed `Update()` to capture `hook := s.statusChangeHook` under the lock, then call `s.mu.Unlock()` manually before invoking the hook.

---

### FR-REVIEW-004 — Terminated agents must be re-spawnable by name

**Priority:** P1 (high — correctness)  
**Status:** ✅ Resolved (commit `27cdc9a`)

`Manager.markTerminated()` set the agent's `Status` to `Terminated` but did not remove it from the `m.bots` map. `Spawn()`'s duplicate check was `if _, exists := m.bots[name]; exists`, which blocked re-spawning even for terminated agents. The pool's warm-standby deallocate-then-reallocate pattern depends on being able to re-use names.

**Acceptance Criteria:**
- [ ] Calling `Spawn()` with a name that previously existed but is now terminated succeeds.
- [ ] A regression test `TestManager_Spawn_TerminatedAgent_CanBeRespawned` exists and passes.
- [ ] Active (non-terminated) agent duplicate check still returns an error.

**Resolution:** Changed `Spawn()`'s check to `if state, exists := m.bots[name]; exists && state.agent.Status != domain.AgentStatusTerminated`. Added `TestManager_Spawn_TerminatedAgent_CanBeRespawned`.

---

### FR-REVIEW-005 — TearDownAll must not depend on a potentially-cancelled caller context

**Priority:** P1 (high — correctness)  
**Status:** ✅ Resolved (commit `27cdc9a`)

`TearDownAll(ctx)` derived its per-goroutine wait deadline from the incoming `ctx`. If the caller's context was already cancelled (the normal shutdown case — `runCtx.Done()` fires, `Shutdown` calls `TearDownAll(context.Background())`), the `context.WithTimeout(ctx, remaining)` call would immediately time out, producing false `TearDownAll timed out` errors for every active bot.

**Acceptance Criteria:**
- [ ] `TearDownAll` uses `context.Background()` as the base for wait deadlines, not the caller's context.
- [ ] Existing `TearDownAll` tests pass without modification.

**Resolution:** Changed `context.WithTimeout(ctx, remaining)` to `context.WithTimeout(context.Background(), remaining)` inside the wait loop.

---

### FR-REVIEW-006 — Stale session records must be discarded at startup

**Priority:** P2 (medium — correctness)  
**Status:** ✅ Resolved (commit `27cdc9a`)

`WithSessionFile()` stored the file reference but did not inspect pre-existing records. FR-013 in the spec requires graceful handling of stale session records at startup ("reconnect or discard"). Reconnecting goroutines is not supported; silently ignoring stale records would leave the session file perpetually dirty.

**Acceptance Criteria:**
- [ ] `WithSessionFile()` loads any pre-existing records and clears them if non-empty, logging a warning with the count.
- [ ] A regression test `TestManager_WithSessionFile_ClearsStaleRecords` exists and passes.

**Resolution:** Added stale-record loading and `sf.Save([]infrastructure.SessionRecord{})` to `WithSessionFile()`. Added `TestManager_WithSessionFile_ClearsStaleRecords`.

---

### FR-REVIEW-007 — Compile-time interface conformance checks

**Priority:** P2 (medium — maintainability)  
**Status:** ✅ Resolved (commit `27cdc9a`)

Neither `application/subteam.Manager` nor `application/pool.Pool` had compile-time assertions that they satisfy their respective domain interfaces (`domain.SubTeamManager`, `domain.TechLeadPool`). A method signature mismatch would surface only at the injection site, not at the definition site.

**Acceptance Criteria:**
- [ ] `var _ domain.SubTeamManager = (*Manager)(nil)` present in `application/subteam/manager.go`.
- [ ] `var _ domain.TechLeadPool = (*Pool)(nil)` present in `application/pool/pool.go`.
- [ ] Both files compile without errors.

**Resolution:** Added both assertions.

---

## Implementation Guidance

All fixes must follow TDD (Red → Green → Refactor). Each finding has a corresponding new or updated test. Fixes must not introduce Clean Architecture violations (no infrastructure imports in domain or application packages). Prioritise P0 items first.

Conduct a brief code and design review as each fix is completed before moving to the next.

Worker agent teammates and git worktrees may be used for parallel fix workstreams if the volume warrants it.
