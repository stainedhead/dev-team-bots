# Data Dictionary: Tech-Lead Dynamic Subteam and Orchestrator Pool Management

**Feature:** tech-lead-dynamic-subteam
**Created:** 2026-05-07
**Status:** Draft — populate during Phase 2

---

## Purpose

Defines all domain entities, value objects, interfaces, enumerations, and API types introduced or modified by this feature.

---

## Entities

### `SpawnedAgent`

Represents a dynamically spawned sub-agent bot instance managed by a tech-lead.

| Field | Type | Description |
|-------|------|-------------|
| Name | string | Caller-chosen unique instance name within the session (e.g. `impl-feature-auth`) |
| BotType | string | Bot type name matching an entry in `boabot-team/bots/` |
| WorkDir | string | Working directory assigned to this bot (empty if not set) |
| BusID | string | ID of the private scoped message bus this bot is connected to |
| Status | AgentStatus | Current lifecycle status of the bot |
| SpawnedAt | time.Time | When the bot was spawned |

### `PoolEntry`

Represents a tech-lead instance in the orchestrator's pool.

| Field | Type | Description |
|-------|------|-------------|
| InstanceName | string | Distinct name of this tech-lead instance (e.g. `tech-lead-1`) |
| Status | PoolEntryStatus | Current status: idle or allocated |
| ItemID | string | ID of the kanban item this tech-lead is allocated to (empty if idle) |
| AllocatedAt | time.Time | When this tech-lead was last allocated (zero if idle) |
| BusID | string | ID of this tech-lead's private scoped message bus |

---

## Value Objects

[TBD — identify value objects during Phase 2, e.g. `InstanceName`, `BotType`, `BusID`]

---

## Interfaces

### `SubTeamManager` (domain)

```go
type SubTeamManager interface {
    Spawn(ctx context.Context, botType, name, workDir string) (*SpawnedAgent, error)
    Terminate(ctx context.Context, name string) error
    SendHeartbeat(ctx context.Context) error
    ListAgents(ctx context.Context) ([]*SpawnedAgent, error)
    Reconnect(ctx context.Context, sessionFile SessionFile) error
    TearDownAll(ctx context.Context) error
}
```

### `TechLeadPool` (domain)

```go
type TechLeadPool interface {
    Allocate(ctx context.Context, itemID string) (*PoolEntry, error)
    Deallocate(ctx context.Context, itemID string) error
    Reconcile(ctx context.Context) error
    ListEntries(ctx context.Context) ([]*PoolEntry, error)
    GetByItemID(ctx context.Context, itemID string) (*PoolEntry, error)
}
```

### `ScopedBus` (infrastructure)

[TBD — define scoped bus interface during Phase 2 codebase research]

---

## Enumerations

### `AgentStatus`

| Value | Description |
|-------|-------------|
| `idle` | Spawned and connected, awaiting a task |
| `working` | Actively executing a task |
| `terminating` | Finishing current work unit before shutdown |
| `terminated` | Cleanly stopped |

### `PoolEntryStatus`

| Value | Description |
|-------|-------------|
| `idle` | Tech-lead instance is warm and awaiting allocation |
| `allocated` | Tech-lead instance is assigned to a kanban item |
| `terminating` | Tech-lead is finishing in-flight work before shutdown |

---

## API Request / Response Types

### `GET /api/v1/pool` (new endpoint)

Response:

```json
{
  "pool": [
    {
      "instance_name": "tech-lead-1",
      "status": "allocated",
      "item_id": "ITEM-42",
      "allocated_at": "2026-05-07T10:00:00Z"
    },
    {
      "instance_name": "tech-lead-2",
      "status": "idle",
      "item_id": "",
      "allocated_at": null
    }
  ]
}
```

### Session File Schema

JSON file stored in the tech-lead's memory directory:

```json
{
  "session_id": "uuid",
  "agents": [
    {
      "name": "impl-feature-auth",
      "bot_type": "implementer",
      "work_dir": "/path/to/worktree",
      "bus_id": "scoped-bus-uuid",
      "status": "working",
      "spawned_at": "2026-05-07T10:00:00Z"
    }
  ]
}
```

### Pool State File Schema

JSON file stored in the orchestrator's memory directory:

```json
{
  "entries": [
    {
      "instance_name": "tech-lead-1",
      "status": "allocated",
      "item_id": "ITEM-42",
      "bus_id": "scoped-bus-uuid",
      "allocated_at": "2026-05-07T10:00:00Z"
    }
  ]
}
```
