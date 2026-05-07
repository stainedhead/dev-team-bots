# Product Details — boabot

## Agent Lifecycle

1. **Startup** — load `team.yaml`, start all enabled bots as in-process goroutines managed by `TeamManager`.
2. **Registration** — each bot goroutine registers with the in-process `BotRegistry`; the registry makes the bot discoverable to peers.
3. **Operation** — bot polls its in-process queue (`local/queue`); executes tasks via `RunAgentUseCase`; worker goroutines handle each task.
4. **Memory** — reads and writes the local filesystem via the `local/fs` FS adapter; semantic search served by the BM25 embedder + cosine similarity vector store (`local/vector`); optional GitHub git backup (`github/backup`) runs on a configurable cron schedule (default: every 30 minutes).
5. **Budget** — token spend and tool call counts are tracked in the local budget tracker (`local/budget`) and enforced before each tool dispatch; counters are persisted to `budget.json` and restored on startup.
6. **Shutdown** — `TeamManager` broadcasts a `ShutdownMessage` to all bot queues and waits for all goroutines to exit cleanly. The heap watchdog goroutine cancels the shared context if the hard memory limit is exceeded, triggering an orderly shutdown.

## Worker Thread

A worker goroutine is a self-contained agent harness. It receives a task and builds an initial context: SOUL.md, current todo list, skill index (name-and-summary stubs only), and the task definition. Additional material is loaded on demand via tool calls (progressive disclosure). A panic in a worker goroutine is recovered and logged — it does not affect the `TeamManager` goroutine or other workers.

### Context Management

When the context window approaches a configurable token threshold, the harness checkpoints all durable state — todo list, any memory writes from the current session, structured task state — to the local filesystem memory store, then starts a fresh worker goroutine reinitialised from the checkpoint. This is provider-agnostic and works identically across Anthropic, Bedrock, and OpenAI-compatible providers.

## Built-in Harness Tools

The harness provides a fixed set of built-in tools, bound at startup from the bot's `allowed_tools` config:

| Tool | Purpose | Safety scope |
|---|---|---|
| `read_file`, `list_dir`, `glob`, `grep` | Filesystem reads | Workspace-scoped |
| `write_file`, `edit_file` | Filesystem writes | Workspace-scoped |
| `memory_search(query)` | Local vector store semantic retrieval | Namespaced to bot |
| `send_message`, `read_messages` | Bot-to-bot messaging via in-process queue | Audited; `receive_from` allowlist enforced on recipient |
| `todo_write`, `todo_read` | Persistent per-bot task list | Scoped to calling bot |
| `http_request` | HTTP to allowed external hosts | Allowlisted hosts; fully logged |
| `get_metrics` | Own operational metrics | Read-only, scoped to calling bot |

All file tools are constrained to the bot's workspace and memory directory. The harness enforces this at dispatch time.

### Tool Attention

Before injecting tool schemas into the model's context, the harness scores all available tools (built-in + MCP) against the current task intent using BM25. Only the top-k matched tools are injected as full JSON schemas; all others appear as compact name-and-summary stubs. The hard cap is 20 simultaneously injected full schemas. The scorer is a swappable interface — BM25 can be replaced with neural embeddings by configuring `memory.embedder`.

### Tool Safety

- **Startup binding** — `allowed_tools` in `config.yaml` controls what the model can see. Tools outside the list are invisible.
- **Calibrated autonomy** — gate types (advisory, validating, blocking, escalating) are assigned per action based on reversibility. Approvals are never cached.
- **Prompt injection defence** — all tool outputs are sanitised before being returned to the model.
- **Budget caps** — token spend and tool call counts enforced by the harness before each dispatch; counters persisted locally in `budget.json`.
- **Cross-bot messaging allowlists** — `receive_from` in `config.yaml` controls which bots can send action-triggering messages.

## Agent Skills

Skills are modular capability packages (a `SKILL.md` + optional scripts) stored in the bot's memory directory. The harness maintains a skill index in the agent's context (names and one-line summaries). When a task matches a skill, the full `SKILL.md` is promoted into context and supporting scripts are made available as harness-executed tools.

Skills scripts run in restricted subprocesses: no inherited environment variables, filesystem access limited to a temporary working directory.

## Memory

Each bot reads and writes structured memory via file tools on a local git-backed directory:
- **Structured memory** — local filesystem directory managed by the `local/fs` adapter. Personal memory per bot; all bots run in the same process so shared memory coordination uses the in-process bus.
- **Semantic memory** — queried via `memory_search` against the local vector store (`local/vector`). The harness writes embeddings when an agent saves a memory worth indexing.
- **GitHub backup** — optional scheduled backup via `github/backup` adapter. Commits and pushes the memory directory to a configured GitHub repository on a cron schedule (default: `*/30 * * * *`). Restore clones or pulls from the remote on startup when the local directory is empty.

## Model Provider

Model providers are named and typed in config. The provider factory initialises each provider once and caches it. Workers call `factory.Get(name)` to obtain a provider and `provider.Invoke(prompt, options)` to call the model.

Supported provider types:
- `anthropic` — Anthropic API via `ANTHROPIC_API_KEY`; primary provider.
- `bedrock` — AWS Bedrock via `internal/infrastructure/aws/bedrock`; optional.
- `openai` — OpenAI-compatible endpoints (including Ollama); optional.

## Orchestrator Mode

Enabled by `orchestrator.enabled: true` in config. Adds:

- **Control plane** — maintains the team registry. Accepts registration, heartbeat, and deregistration messages. Stores Agent Card per bot. Enforces one-instance-per-agent-type.
- **Kanban board** — manages work items. States: backlog, in-progress, blocked, done. Notifies assigned bots on assignment. All mutations include client-supplied idempotency tokens.
- **REST API** — JWT-authenticated access to control plane and board at `/api/v1/`. All 26 endpoints match the `baobotctl` CLI contract (auth, board, team, skills, users, profile, DLQ). Admin-only routes return 403 for non-admin callers.
- **Web UI** — HTMX Kanban board at `/`; auto-refreshes board columns and team health every 30 seconds without a full page reload.
- **User management** — two roles: Admin and User. JWT issued on login, forced password change on first use.
- **Tech-lead pool** — dynamically allocates and deallocates tech-lead instances as kanban items move in and out of In Progress state. See [Tech-Lead Pool Management](#tech-lead-pool-management) below.

## Dynamic Subteam Spawning

Tech-lead bots can spawn named sub-agent goroutines on demand during active work sessions. This enables a single tech-lead to delegate parallel workstreams — for example, spawning two implementer bots to work on independent modules simultaneously.

### How Spawning Works

A `subteam.spawn` message is sent to the tech-lead's queue with the bot type, a unique name, and an optional working directory. The tech-lead's `RunAgentUseCase` receives the message and instructs the `SubTeamManager` to create the sub-agent. The new sub-agent goroutine gets its own isolated message bus and queue router — it shares no state with the parent tech-lead or any sibling sub-agents. On successful spawn, the tech-lead receives the `SpawnedAgent` record including the bus ID used to route messages to the new sub-agent.

A `subteam.terminate` message stops a named sub-agent. The tech-lead also calls `TearDownAll` on graceful shutdown, which terminates all live sub-agents before the process exits.

### Heartbeat Watchdog

Every spawned sub-agent has an internal heartbeat watchdog. The tech-lead calls `SendHeartbeat` on a 30-second timer; each call fans the signal to all live sub-agents. If a sub-agent goes 90 seconds without receiving a heartbeat (three missed intervals), it self-terminates. This prevents goroutine leaks when the tech-lead becomes busy, crashes, or loses its own context — the sub-agents clean up independently.

Panics inside a spawned bot goroutine are caught and logged; the goroutine terminates cleanly without affecting the tech-lead or any other sub-agents.

### Session Persistence

When a sub-agent is spawned, its record — name, bot type, working directory, bus ID, status, and spawn time — is written atomically to `session.json` in the tech-lead's memory directory. When the sub-agent terminates, its record is removed. On startup, the tech-lead can load existing session records to reconstruct its view of any sub-agents that survived a process restart.

### Soft Spawn Limit

When more than 5 sub-agents are spawned simultaneously, the `SubTeamManager` logs a warning. This is a soft advisory limit — spawning is not blocked. The warning is intended to surface unusual load before it becomes a resource concern.

## Tech-Lead Pool Management

The orchestrator maintains a pool of tech-lead instances keyed to In Progress kanban items. The goal is that every active work item always has a dedicated tech-lead standing by to coordinate it, with minimal cold-start latency.

### Allocation

When a board item transitions to `in-progress`, the orchestrator calls `TechLeadPool.Allocate()`. The pool first looks for an idle instance already in the pool; if one exists, it is promoted to `allocated` and associated with the item. If no idle instance is available, a new `tech-lead-N` instance is spawned and added to the pool. All allocation operations are serialised by a mutex to prevent double-allocation.

When the pool reaches 10 entries, a warning is logged. This does not block allocation.

### Deallocation and Warm Standby

When a board item leaves `in-progress` (completed, blocked, or moved back to backlog), `TechLeadPool.Deallocate()` is called. If the instance being deallocated is the last one in the pool, it is not stopped — it is kept as an idle warm standby. This guarantees that at least one tech-lead instance is always pre-warmed, eliminating cold-start latency when the next item enters `in-progress`. If there is more than one instance in the pool at deallocation time, the instance is stopped and its record removed.

### Pool State Persistence

The pool's current state — all instances with their names, status, allocated item IDs, bus IDs, and allocation timestamps — is written atomically to `pool.json` in the orchestrator's memory directory on every allocation or deallocation. On startup, `Reconcile()` loads this file, checks which instances are still running, and discards stale records for instances that did not survive the restart.

### REST API

The current pool state is exposed at `GET /api/v1/pool`. This endpoint does not require authentication and returns the live snapshot from `TechLeadPool.ListEntries()`. See [user-docs/pool-management.md](../user-docs/pool-management.md) for the response schema and an example.
