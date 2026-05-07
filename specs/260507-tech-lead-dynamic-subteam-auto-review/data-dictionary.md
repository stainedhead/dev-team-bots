# Data Dictionary: Tech-Lead Dynamic Subteam Auto-Review Fixes

**Feature:** tech-lead-dynamic-subteam-auto-review  
**Created:** 2026-05-07

---

## Purpose

Documents the data structures introduced or modified by this fix set.

---

## New Types

### `dynamicBot` (team package, internal)

| Field | Type | Description |
|-------|------|-------------|
| `cancel` | `context.CancelFunc` | Cancels the spawned tech-lead goroutine |
| `done` | `chan struct{}` | Closed when the goroutine exits |

---

## Modified Types

### `TeamManager` (application/team)

New fields added:

| Field | Type | Description |
|-------|------|-------------|
| `orchestratorName` | `string` | Orchestrator bot name; set during `Run()` |
| `dynamicBots` | `map[string]*dynamicBot` | Live pool-spawned instances keyed by instance name |
| `dynamicBotsMu` | `sync.Mutex` | Guards `dynamicBots` map |

---

## Interfaces (unchanged)

- `domain.SubTeamManager` — compile-time check added in `application/subteam/manager.go`
- `domain.TechLeadPool` — compile-time check added in `application/pool/pool.go`
