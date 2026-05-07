# Tech-Lead Subteam Spawning

Tech-lead bots can spawn named sub-agent goroutines at runtime. Each sub-agent runs in complete isolation with its own message bus and queue router. The tech-lead coordinates the sub-agents by sending them tasks and keeping them alive via heartbeats.

## What It Is and When to Use It

Subteam spawning lets a tech-lead distribute parallel workstreams across multiple specialised bots without involving the orchestrator or modifying `team.yaml`. It is designed for situations where a single body of work can be broken into independent sub-tasks that do not need to share context — for example, two implementer bots working on separate modules simultaneously, or a dedicated reviewer bot running alongside an implementer.

Sub-agents exist only for the lifetime of their spawning tech-lead. They are not registered in the global bot registry and are not visible to the orchestrator. When the tech-lead terminates or tears down, all its sub-agents are also terminated.

## Triggering Spawn and Terminate

Sub-agents are controlled by sending typed messages to the tech-lead's in-process queue.

### Spawn a Sub-Agent

Send a `subteam.spawn` message to the tech-lead's queue:

```json
{
  "id":        "msg-001",
  "type":      "subteam.spawn",
  "from":      "orchestrator",
  "to":        "tech-lead-alpha",
  "payload":   {
    "bot_type": "implementer",
    "name":     "impl-auth",
    "work_dir": "/workspace/auth-service"
  },
  "timestamp": "2026-05-07T14:00:00Z"
}
```

**Payload fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `bot_type` | string | Yes | The bot type to spawn. Must match a directory under `bots/` (e.g., `bots/implementer/config.yaml` must exist). |
| `name` | string | Yes | A unique name for this sub-agent within the tech-lead's session. Names must be unique across currently active sub-agents. |
| `work_dir` | string | No | Working directory for the sub-agent. Omit or set to `""` to use the tech-lead's working directory. |

On success, the `SubTeamManager` returns a `SpawnedAgent` record. The spawned bot's bus ID is `bus-<name>` (e.g., `bus-impl-auth`).

**Error conditions:**
- A sub-agent with the same `name` is already active — returns an error; no new goroutine is created.
- The `bot_type` directory does not contain a `config.yaml` — returns an error.

### Terminate a Sub-Agent

Send a `subteam.terminate` message to the tech-lead's queue:

```json
{
  "id":        "msg-002",
  "type":      "subteam.terminate",
  "from":      "orchestrator",
  "to":        "tech-lead-alpha",
  "payload":   {
    "name": "impl-auth"
  },
  "timestamp": "2026-05-07T14:45:00Z"
}
```

`Terminate` cancels the sub-agent's context and waits up to 5 seconds for the goroutine to exit. If the goroutine does not exit in time, a timeout error is returned. The sub-agent should be considered unreliable after a timeout error.

## Heartbeat Behaviour

The tech-lead sends heartbeat signals to all live sub-agents on a 30-second interval by calling `SubTeamManager.SendHeartbeat`. Each sub-agent has an internal watchdog timer set to 90 seconds. Every time a heartbeat arrives, the timer resets.

If a sub-agent does not receive a heartbeat within 90 seconds (three missed intervals), it self-terminates. This automatic cleanup fires even if the tech-lead becomes busy or loses context — sub-agents do not linger indefinitely.

The heartbeat is triggered by sending a `subteam.heartbeat` message to the tech-lead's queue:

```json
{
  "id":        "msg-hb-1",
  "type":      "subteam.heartbeat",
  "from":      "orchestrator",
  "to":        "tech-lead-alpha",
  "timestamp": "2026-05-07T14:00:30Z"
}
```

The harness sends this internally on its own timer. You only need to send it manually if you are operating the tech-lead from outside the normal harness flow.

## Session File Location and Format

Sub-agent state is persisted to `session.json` in the tech-lead's memory directory. The default memory path is `<binary-dir>/memory`, configurable via `memory.path` in `config.yaml`.

```
<memory>/session.json
```

The file contains a JSON array of session records:

```json
[
  {
    "name":       "impl-auth",
    "bot_type":   "implementer",
    "work_dir":   "/workspace/auth-service",
    "bus_id":     "bus-impl-auth",
    "status":     "working",
    "spawned_at": "2026-05-07T14:00:01Z"
  },
  {
    "name":       "impl-payments",
    "bot_type":   "implementer",
    "work_dir":   "/workspace/payments",
    "bus_id":     "bus-impl-payments",
    "status":     "idle",
    "spawned_at": "2026-05-07T14:01:15Z"
  }
]
```

**Status values:** `idle`, `working`, `terminating`, `terminated`.

The file is written atomically (write to `.tmp` then `os.Rename`). If the file is missing on load, an empty list is returned. If the file is corrupt, a warning is logged and an empty list is returned — the process does not crash.

When a sub-agent terminates (gracefully or via heartbeat timeout), its record is removed from the file.

## Limits and Warnings

The `SubTeamManager` has a soft spawn limit of **5 simultaneously active sub-agents**. When more than 5 sub-agents are spawned, a warning is logged:

```
level=WARN msg="subteam: soft spawn limit exceeded" limit=5 current=6
```

Spawning is not blocked. The warning is advisory — it surfaces unusual load before it becomes a resource concern. Consider whether the work can be structured with fewer parallel agents before exceeding this threshold.

## Example: Spawning Two Parallel Implementer Bots

This example shows the message sequence for a tech-lead spawning two implementer bots to work in parallel on separate modules, then terminating them when both are done.

**1. Spawn the first implementer:**
```json
{
  "type": "subteam.spawn",
  "payload": {
    "bot_type": "implementer",
    "name":     "impl-auth",
    "work_dir": "/workspace/auth-service"
  }
}
```

**2. Spawn the second implementer:**
```json
{
  "type": "subteam.spawn",
  "payload": {
    "bot_type": "implementer",
    "name":     "impl-payments",
    "work_dir": "/workspace/payments"
  }
}
```

**3. Send tasks to each sub-agent via their bus IDs (`bus-impl-auth`, `bus-impl-payments`). Both work concurrently.**

**4. When `impl-auth` completes:**
```json
{
  "type": "subteam.terminate",
  "payload": { "name": "impl-auth" }
}
```

**5. When `impl-payments` completes:**
```json
{
  "type": "subteam.terminate",
  "payload": { "name": "impl-payments" }
}
```

If the tech-lead shuts down without explicit termination, `TearDownAll` is called automatically — all remaining sub-agents are terminated concurrently with a 10-second total deadline.
