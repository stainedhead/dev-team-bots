# Implementation Notes: Tech-Lead Dynamic Subteam Auto-Review Fixes

**Feature:** tech-lead-dynamic-subteam-auto-review  
**Created:** 2026-05-07

---

## Purpose

Records technical decisions, edge cases, and deviations from plan encountered during fix implementation.

---

## Technical Decisions

**T-002 — spawn lifecycle tracking:**  
Used `done chan struct{}` per dynamic bot rather than `sync.WaitGroup` so `isTechLeadRunning` can non-blockingly check status via `select { case <-db.done: return false; default: return true }`.

**T-002 — router pre-registration:**  
`spawnTechLead` calls `tm.router.Register(instanceName, 0)` before starting the goroutine. This ensures the routing channel exists before the bot attempts to receive.

**T-004 — terminated agents stay in map:**  
Rather than deleting terminated agents from `m.bots` in `markTerminated`, the duplicate check in `Spawn` was changed to permit re-use when `status == Terminated`. This preserves the terminated agent in `ListAgents` until the name is explicitly reused.

**T-006 — stale record discard vs reconnect:**  
Goroutine reconnect is not supported (goroutines cannot be serialized to disk). `WithSessionFile` discards stale records with a warning log to satisfy FR-013 ("discard gracefully").

---

## Edge Cases & Solutions

**Pool auth test:** `TestPool_Endpoint_NoPool_Returns200Empty` was updated to send an auth header — the test was exercising "nil pool → empty array", not "no auth", so auth is correct behavior for this endpoint.

**TearDownAll cancelled context:** The fix uses `context.Background()` as the wait base. The `ctx` parameter to `TearDownAll` may be the already-cancelled `runCtx` during shutdown. Using a fresh background context for the 10-second wall-clock deadline is correct.

---

## Deviations from Plan

None — all 7 tasks implemented as specified.

---

## Lessons Learned

- Default spawnFn that errors should log loudly, not silently. Consider making the default panic rather than return an error to catch misconfiguration at startup.
- `defer mu.Unlock()` is a common source of "hook called under lock" bugs — prefer explicit unlock when callbacks follow the critical section.
