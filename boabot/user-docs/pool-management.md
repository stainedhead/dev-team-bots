# Orchestrator Tech-Lead Pool Management

The orchestrator maintains a dynamic pool of tech-lead instances, one per In Progress kanban item. The pool ensures that every active work item has a dedicated tech-lead available to coordinate it, with warm-standby reuse to eliminate cold-start latency.

## What It Is and When to Use It

Pool management is an orchestrator-mode feature that runs automatically. You do not need to trigger it manually. When an item on the kanban board transitions to `in-progress`, the orchestrator allocates a tech-lead from the pool to that item. When the item leaves `in-progress`, the tech-lead is released back.

The pool is useful in teams with multiple concurrent work items: each in-progress item gets a dedicated tech-lead goroutine that can spawn its own sub-agents and coordinate implementation without sharing context with other items.

Pool management requires `orchestrator.enabled: true` in `config.yaml`.

## How It Works Automatically

The orchestrator hooks directly into the board mutation path. No polling is involved.

**When a board item transitions to `in-progress`:**
1. `TechLeadPool.Allocate(ctx, itemID)` is called.
2. The pool checks for an existing idle entry. If one exists (warm standby), it is promoted to `allocated` and associated with the item — no new process is started.
3. If no idle entry exists, a new `tech-lead-N` instance is spawned (e.g., `tech-lead-1`, `tech-lead-2`) and added to the pool.
4. The `PoolEntry` is returned, including the instance name and bus ID for routing messages to the tech-lead.

**When a board item leaves `in-progress`:**
1. `TechLeadPool.Deallocate(ctx, itemID)` is called.
2. If this is the only entry in the pool, the instance is kept alive as an idle warm standby (see [Warm Standby Behaviour](#warm-standby-behaviour) below).
3. If there are multiple entries, the instance is stopped and its record removed from the pool.

All allocation and deallocation operations are serialised by a mutex to prevent double-allocation when multiple board updates arrive concurrently.

## Warm Standby Behaviour

The pool guarantees that at least one tech-lead instance remains running at all times once the pool has been used. When `Deallocate` would remove the last entry, the instance is instead set to `idle` status and kept running:

- `ItemID` is cleared.
- Status changes from `allocated` to `idle`.
- The instance is not stopped.

When the next `Allocate` is called, this idle instance is reused immediately — no spawn delay. The warm standby is always the first entry reused.

The warm standby is not removed even if there are no in-progress items. It persists until the orchestrator process exits.

## Pool State File Location and Format

Pool state is persisted to `pool.json` in the orchestrator's memory directory. The default memory path is `<binary-dir>/memory`, configurable via `memory.path` in `config.yaml`.

```
<memory>/pool.json
```

The file contains a JSON array of pool state records:

```json
[
  {
    "instance_name": "tech-lead-1",
    "status":        "allocated",
    "item_id":       "item-42",
    "bus_id":        "bus-tech-lead-1",
    "allocated_at":  "2026-05-07T14:30:00Z"
  },
  {
    "instance_name": "tech-lead-2",
    "status":        "idle",
    "bus_id":        "bus-tech-lead-2"
  }
]
```

**Field notes:**
- `item_id` and `allocated_at` are omitted from the JSON when empty (idle entries).
- `status` values: `idle`, `allocated`, `terminating`.
- The file is written atomically (write to `.tmp` then `os.Rename`) on every pool mutation.
- If the file is missing on load, an empty list is returned. If the file is corrupt, a warning is logged and an empty list is returned.

## REST API — GET /api/v1/pool

The current pool state is exposed at `GET /api/v1/pool`. This endpoint does not require authentication.

**Request:**
```
GET /api/v1/pool HTTP/1.1
Host: localhost:8080
```

**Response:**
```json
{
  "pool": [
    {
      "InstanceName": "tech-lead-1",
      "Status":       "allocated",
      "ItemID":       "item-42",
      "AllocatedAt":  "2026-05-07T14:30:00Z",
      "BusID":        "bus-tech-lead-1"
    },
    {
      "InstanceName": "tech-lead-2",
      "Status":       "idle",
      "ItemID":       "",
      "AllocatedAt":  "0001-01-01T00:00:00Z",
      "BusID":        "bus-tech-lead-2"
    }
  ]
}
```

If the pool is not configured or is empty, the response is:
```json
{ "pool": [] }
```

**Status values:**

| Value | Meaning |
|---|---|
| `idle` | Instance is running but not assigned to any item. Ready for immediate reuse. |
| `allocated` | Instance is assigned to `ItemID`. |
| `terminating` | Instance is in the process of being stopped. |

## Startup Reconciliation

On orchestrator startup, `TechLeadPool.Reconcile(ctx)` runs automatically. It:

1. Loads `pool.json`.
2. For each record, calls the configured `isRunFn` predicate to check whether the instance is still running.
3. Discards records for instances that did not survive the restart.
4. Rebuilds the live pool from the surviving records.
5. Saves the reconciled state back to `pool.json`.

Reconciliation means a process restart does not corrupt the pool — stale records from instances that did not survive are removed, and live instances (if any) are reclaimed. The `isRunFn` is injected at construction time, so the definition of "running" is implementation-specific to the deployment environment.

## What Happens When a Tech-Lead Crashes

If a tech-lead instance crashes (panics or is killed externally) while allocated to an item:

- The pool record retains `status: allocated` until the next `Deallocate` call or the next startup reconciliation.
- On reconciliation, `isRunFn` returns false for the crashed instance and the record is discarded.
- The item on the board is not automatically marked as blocked — this is the responsibility of the board integration layer that calls `Deallocate`. If the board integration detects that the assigned tech-lead is gone, it should call `Deallocate` and update the item status accordingly.

The pool itself has no liveness monitor for allocated instances between restarts. If you require automatic crash detection, implement a separate watchdog that polls `GET /api/v1/pool` and reconciles against the board's in-progress items.

## Soft Pool Limit

When the pool reaches 10 entries, the following warning is logged:

```
level=WARN msg="pool: soft pool limit exceeded" limit=10 current=11
```

Allocation is not blocked. The warning is advisory — it surfaces unusually high parallel work volume. Review whether all in-progress items are making progress if this warning appears repeatedly.
