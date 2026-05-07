# Tasks: Tech-Lead Dynamic Subteam Auto-Review Fixes

**Feature:** tech-lead-dynamic-subteam-auto-review  
**Created:** 2026-05-07  
**Status:** Complete

---

## Progress Summary

7/7 tasks complete.

---

## Phase 5 — Fix Implementation

| ID | Task | Status | Commit |
|----|------|--------|--------|
| T-001 | Add `s.auth()` to `GET /api/v1/pool` + `TestPool_Endpoint_RequiresAuth` | ✅ | 27cdc9a |
| T-002 | Wire `SetSpawnFn`/`SetStopFn`/`SetIsRunningFn` in `TeamManager.Run()` + implement lifecycle methods | ✅ | 27cdc9a |
| T-003 | Release write lock before calling `statusChangeHook` in `board.go Update()` | ✅ | 27cdc9a |
| T-004 | Fix `Spawn()` duplicate check to allow terminated-name re-use + `TestManager_Spawn_TerminatedAgent_CanBeRespawned` | ✅ | 27cdc9a |
| T-005 | Fix `TearDownAll` to use `context.Background()` for wait deadlines | ✅ | 27cdc9a |
| T-006 | Clear stale records in `WithSessionFile()` + `TestManager_WithSessionFile_ClearsStaleRecords` | ✅ | 27cdc9a |
| T-007 | Add compile-time interface assertions to `manager.go` and `pool.go` | ✅ | 27cdc9a |
