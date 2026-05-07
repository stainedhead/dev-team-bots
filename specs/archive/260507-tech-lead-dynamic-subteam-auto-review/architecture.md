# Architecture: Tech-Lead Dynamic Subteam Auto-Review Fixes

**Feature:** tech-lead-dynamic-subteam-auto-review  
**Created:** 2026-05-07  
**Status:** Draft

---

## Architecture Overview

All fixes are targeted changes within existing files. No new packages, no new domain interfaces, no new infrastructure adapters.

---

## Component Architecture

| Layer | Component | Change |
|-------|-----------|--------|
| Infrastructure | `http/server.go` | Auth middleware added to pool route |
| Infrastructure | `local/orchestrator/board.go` | Lock released before hook invocation |
| Application | `application/subteam/manager.go` | Spawn check, WithSessionFile, TearDownAll, interface assertion |
| Application | `application/pool/pool.go` | Interface assertion |
| Application | `application/team/team_manager.go` | Pool lifecycle methods, dynamicBots map, orchestratorName |

---

## Layer Responsibilities

No changes to layer boundaries. All fixes correct behaviour within the existing architecture.

---

## Data Flow

**Pool spawn flow (before fix):**
```
Board hook → Pool.Allocate → spawnFn() → error "no spawnFn configured"
```

**Pool spawn flow (after fix):**
```
Board hook → Pool.Allocate → TeamManager.spawnTechLead → runBotWithRestart goroutine
```

---

## Architectural Decisions

**AD-001:** `dynamicBots` map uses `done chan struct{}` rather than a WaitGroup per-bot so `isTechLeadRunning` can non-blockingly check if a goroutine is still alive via a select.

**AD-002:** `spawnTechLead` calls `tm.router.Register(instanceName, 0)` before starting the goroutine so the routing channel exists before the bot attempts to receive messages.

---

## Integration Points

| Component | Integration |
|-----------|-------------|
| `pool.Pool` | Calls `tm.spawnTechLead` / `tm.stopTechLead` / `tm.isTechLeadRunning` |
| `InMemoryBoardStore` | Hook now fired outside write lock |
| `http.Server` | Pool endpoint behind auth middleware |
