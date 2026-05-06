# Technical Details — BaoBot Dev Team

This document describes the system-level architecture. For module-specific technical details see the `docs/technical-details.md` within each module directory.

## Technology Stack

| Concern | Technology |
|---|---|
| Language | Go 1.26 |
| Agent runtime | Local single-binary process |
| Messaging | In-process queues (`local/queue`) per bot; in-process broadcaster (`local/bus`) for team-wide |
| Structured delegation | In-process queue with A2A-shaped envelope |
| Memory — structured | Local filesystem (`local/fs`); optional GitHub git backup |
| Memory — semantic | Local BM25 embedder (`local/bm25`) + cosine similarity vector store (`local/vector`) |
| Budget counters | Local JSON file (`local/budget`), persisted per bot |
| Model inference | Anthropic API (primary), AWS Bedrock (optional), OpenAI-compatible endpoints (optional, incl. Ollama) |
| Tool integration | MCP (Model Context Protocol) |
| Tool gating | BM25 scoring (Tool Attention, 20-tool cap) |
| CI/CD | GitHub Actions |
| Credentials | `~/.boabot/credentials` INI file + environment variables |
| Authentication | JWT (username/password, HS256) |
| Observability | OpenTelemetry (traces, metrics, logs) |

## System Architecture

```
┌─────────────────────────────────────────┐
│             boabot process              │
│                                         │
│  TeamManager                            │
│    ├── Orchestrator goroutine           │
│    │     └── RunAgentUseCase            │
│    │           └── Worker goroutines    │
│    ├── Architect goroutine              │
│    │     └── RunAgentUseCase            │
│    │           └── Worker goroutines    │
│    ├── ... (other enabled bots)         │
│    └── Watchdog goroutine (optional)    │
│                                         │
│  local/queue  (per-bot in-process)      │
│  local/bus    (broadcast)               │
│  local/fs     (memory per bot)          │
│  local/vector (semantic search)         │
│  local/budget (per-bot counters)        │
└─────────────────────────────────────────┘
       │
  REST API / Web UI (orchestrator mode)
       │
  baobotctl / Browser
```

## Messaging Topology

```
Bot A → local/queue (Bot B) → Bot B goroutine   (direct message, delegation)
Bot A → local/bus → all bot queues              (broadcast: shutdown, Agent Card)
Bot B → local/queue (Bot A) → Bot A goroutine   (delegation status: working, completed, failed)
```

## Clean Architecture Layers

```
domain/         — interfaces, entities, value objects (no external imports)
application/    — use cases orchestrating domain logic
infrastructure/ — adapters: local queue/bus/fs/budget/vector, Anthropic, Bedrock, HTTP, auth
cmd/            — wiring: instantiate infrastructure, inject into application
```

## Bot Lifecycle

1. Start → `TeamManager` reads `team.yaml`, starts all enabled bots as goroutines
2. Each bot goroutine registers with `BotRegistry`
3. Each bot polls its in-process queue (`local/queue`), spawning worker goroutines for tasks
4. Memory reads/writes go to the local filesystem (`local/fs`); semantic search via `local/vector`
5. Budget enforced by `local/budget` before each tool dispatch; counters restored from `budget.json` on startup
6. Shutdown → `TeamManager` broadcasts `ShutdownMessage` to all bots; waits for goroutines to exit

## Worker Goroutine Context Lifecycle

```
Receive task
  └── Build initial context: SOUL.md + todo list + skill index (stubs) + task
  └── Execute (Tool Attention gates schema injection, BM25 scores tools)
  └── On context threshold → checkpoint to local/fs memory → restart worker from checkpoint
  └── On completion → write result, update todo list, flush memory
```

## Heap Watchdog

The optional watchdog goroutine runs inside `TeamManager.Run()`. It polls `runtime.ReadMemStats` at a configurable interval. If heap allocations exceed `memory.heap_warn_mb`, a warning is logged. If they exceed `memory.heap_hard_mb`, the watchdog cancels the shared context, triggering orderly shutdown of all bot goroutines.
