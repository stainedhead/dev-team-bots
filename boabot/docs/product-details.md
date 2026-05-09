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
- **Kanban board** — manages work items. States: `backlog`, `queued`, `in-progress`, `blocked`, `done`, `errored`. Notifies assigned bots on assignment. All mutations include client-supplied idempotency tokens.
- **Queue scheduling** — items in the `queued` state wait for a scheduling condition before the `QueueRunner` dispatches them. Four modes:
  - `asap` (default) — dispatch as soon as a concurrency slot is free. FIFO by queue time.
  - `run_at` — dispatch at or after a specified UTC time.
  - `run_after` — dispatch after a predecessor item reaches `done` (or `done`/`errored` when `require_success: false`).
  - `run_when` — dispatch when **both** a scheduled time has passed **and** a predecessor item has finished. Either condition can be omitted, behaving like `run_at` or `run_after` respectively.
  `MaxConcurrent` (default 3) caps how many items can be `in-progress` at once. The `QueueRunner` polls every 5 seconds; any in-progress item whose task has succeeded or failed is automatically transitioned to `done`/`errored`.
- **REST API** — JWT-authenticated access to control plane and board at `/api/v1/`. All 26 endpoints match the `baobotctl` CLI contract (auth, board, team, skills, users, profile, DLQ). Admin-only routes return 403 for non-admin callers.
- **Web UI** — single-page Kanban board at `/`. Features:
  - Six colour-coded columns (backlog, queued, in-progress, blocked, done, errored).
  - Cards show work item title, assignee, and the leaf folder of the working directory.
  - Bot roster shows a per-type skill summary, status, and an info popup with capability bullet-points.
  - Queue Config dialog lets operators set ASAP, Run At, or Run When scheduling with an inline predecessor picker (filtered to in-progress/queued items only).
  - Task list includes a "Dir" column and supports filtering by bot and free-text search.
  - Working-directory text fields trigger a file-path autocomplete popup on keystroke.
  - Board and task list scroll horizontally on narrow screens without breaking the layout.
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

## Plugin Registry

The plugin registry system allows admins to browse, install, update, and remove versioned capability packages from one or more hosted registries without restarting the process.

### Plugin Manifest

Every plugin ships a `plugin.yaml` at the root of its `.tar.gz` archive. The manifest describes the plugin and declares what it exposes:

```yaml
name: my-plugin
version: 1.0.0
description: "Does something useful"
author: your-name
homepage: https://github.com/org/my-plugin   # optional
license: MIT                                   # optional
tags: [utility, data]                          # optional
min_runtime: 1.0.0                             # advisory only; not enforced
entrypoint: run.sh                             # executable inside the archive
checksums:
  sha256: <hex>
provides:
  tools:
    - name: do_thing
      description: "Performs the thing"
      inputSchema:
        type: object
        required: [input]
        properties:
          input: {type: string}
permissions:
  network: [api.example.com]                   # allowed outbound hosts
  env_vars: [MY_API_KEY]                       # allowed env var names
  filesystem: false                            # whether filesystem access is permitted
```

`checksums.sha256` is the SHA-256 hex digest of the `.tar.gz` archive. The installer verifies this before writing any files to disk.

### Registry Protocol

Registries are static HTTPS catalogs. Any HTTPS host (GitHub raw content, S3, etc.) can serve as a registry by exposing:

- `<registry-url>/index.json` — the registry index listing all available plugins.
- Per-plugin manifest URLs and download URLs referenced in `index.json`.

HTTP URLs are rejected at configuration time. No authenticated or private registries are supported.

The registry index (`index.json`) has the following shape:

```json
{
  "registry": "stainedhead/shared-plugins",
  "generated_at": "2026-05-07T00:00:00Z",
  "plugins": [
    {
      "name": "my-plugin",
      "description": "Does something useful",
      "author": "your-name",
      "latest_version": "1.0.0",
      "tags": ["utility"],
      "versions": ["1.0.0", "0.9.0"],
      "manifest_url": "https://.../my-plugin/1.0.0/plugin.yaml",
      "download_url": "https://.../my-plugin/1.0.0/my-plugin.tar.gz"
    }
  ]
}
```

Registry indexes are cached in memory with a 5-minute TTL. A "reload" action bypasses the cache.

### Trust Model

Each configured registry carries a `trusted` flag.

| Trust level | Behaviour after successful checksum verification |
|---|---|
| Trusted | Plugin status set to `active` immediately. Tools appear in `ListTools` on the next call. |
| Untrusted | Plugin status set to `staged`. An admin must call `POST /api/v1/plugins/{id}/approve` before tools are visible to bots. |

Checksum verification runs for all plugins regardless of trust level. A checksum mismatch aborts the install and leaves no files on disk.

### Plugin Lifecycle States

| Status | Meaning |
|---|---|
| `downloading` | Archive is being fetched (transient) |
| `staged` | Installed from an untrusted registry; awaiting admin approval |
| `active` | Installed and available; tools appear in `ListTools` |
| `disabled` | Files retained on disk; tools hidden from bots |
| `update_available` | A newer version exists in the registry |
| `rejected` | Admin rejected a staged plugin; files removed |
| `checksum_fail` | SHA-256 mismatch; install aborted |

### Security Constraints

- Archives are extracted to a temporary directory first; the temp dir is atomically renamed to the final location on success. Any failure deletes the temp dir, leaving `install_dir` clean.
- Zip-slip protection: any archive member whose extracted path escapes `install_dir` aborts the install.
- Maximum wire size: 20 MB compressed. Maximum extracted size: 50 MB. Both limits are enforced before or during extraction.
- Plugin entrypoints run in existing subprocess isolation: network is limited to `permissions.network` hosts; env vars are limited to `permissions.env_vars` declarations.

### Tool Name Collision

If two active plugins declare a tool with the same name, the second plugin's tool is silently skipped (not the first) and a warning is logged. The first plugin's tool continues to work. This is evaluated dynamically on each `ListTools` call.

### Admin UI

The admin UI includes a "Plugins & Skills" tab. From this tab an admin can:
- Browse the catalog for any configured registry.
- Install a plugin in one click (trusted registries activate immediately).
- Approve or reject staged plugins from untrusted registries.
- Enable, disable, reload, or remove installed plugins.
- View full plugin detail: manifest metadata, tool list with schemas, permissions, and SHA-256 checksum.

### Observability

Every plugin lifecycle event — install, approve, reject, enable, disable, update, reload, remove — emits a structured `slog` line with fields: `plugin_name`, `version`, `registry`, `actor`, `status`, `timestamp`.

## Claude Code Plugin Support

Bots can consume Claude Code plugins (those distributed as a `plugin.json` manifest with `commands/<name>.md` Markdown files) via the `read_skill` built-in MCP tool. This allows the bot ecosystem to share plugins with the broader Claude Code tooling ecosystem without any changes to the plugin format.

### read_skill Tool

When a plugin's entrypoint is a `plugin.json` file (detected by `filepath.Base(entrypoint) == "plugin.json"`), calling the plugin tool does not attempt to exec the JSON file. Instead, the MCP client reads `<install_dir>/<plugin_name>/commands/<tool_name>.md` and returns the Markdown content as the tool result. The bot then follows the instructions in that Markdown autonomously using its own built-in tools (`read_file`, `write_file`, `http_request`, etc.).

The `read_skill` tool appears in `ListTools` whenever a plugin store is configured. It is not an opt-in per-tool — any active plugin with a `plugin.json` entrypoint is automatically routed through `read_skill` rather than subprocess exec.

### Claude Code Plugin Lifecycle

Claude Code plugins follow the same install / approve / enable / disable lifecycle as standard plugins. The only difference is in how tool calls are dispatched once the plugin is active. From the admin UI perspective, they are indistinguishable from regular plugins.

## CLI Agent Tools

Bots with the appropriate binaries installed can invoke other AI coding agents as MCP tools. This enables hybrid workflows where a boabot agent hands off a concrete coding task to a specialised CLI tool (Claude Code, Codex, opencode) and collects the result.

### Available Tools

| Tool | Binary | Description |
|---|---|---|
| `run_claude_code` | `claude` | Runs the Claude Code CLI in `--output-format=stream-json` mode and returns the accumulated text output. |
| `run_codex` | `codex` | Runs the OpenAI Codex CLI in quiet, full-auto approval mode and returns plain-text output. |
| `run_openai_codex` | `codex` | Alias for `run_codex`; targets the same OpenAI Codex binary. |
| `run_opencode` | `opencode` | Runs the opencode CLI and returns plain-text output. |

Each tool accepts three inputs: `instruction` (required — the task prompt), `work_dir` (required — working directory for the subprocess), and `model` (optional — overrides the CLI's default model selection).

Tools only appear in `ListTools` if their corresponding binary can be resolved on `PATH` or at the configured `binary_path`. If a binary is not installed, the tool is silently absent — no warning is logged on each `ListTools` call.

### Configuration

CLI tools are configured under `orchestrator.cli_tools` in `config.yaml`:

```yaml
orchestrator:
  cli_tools:
    claude_code:
      enabled: true
      binary_path: ""          # optional absolute path; defaults to PATH lookup of "claude"
    codex:
      enabled: false
      binary_path: ""
    openai_codex:
      enabled: false
      binary_path: ""
    opencode:
      enabled: false
      binary_path: ""
```

A tool disabled via `enabled: false` is always absent from `ListTools`, regardless of whether the binary is installed.

### Execution Model

Each CLI tool call spawns a subprocess via `CLIAgentRunner.Run`. The subprocess receives `SIGTERM` on context cancellation; if it does not exit within 5 seconds, it is force-killed. The default timeout is 30 minutes.

Claude Code output is parsed as `stream-json` events. Only `content_block_delta` text deltas are extracted; internal scaffolding events (tool calls, system prompts) are filtered out. The returned string is the accumulated assistant text, trimmed of trailing newlines.

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
