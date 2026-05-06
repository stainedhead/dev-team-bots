# Product Details — boabot

## Agent Lifecycle

1. **Startup** — load config, SOUL.md, and mcp.json (shared + optional private) from S3; resolve MCP credentials from Secrets Manager; seed budget counters from DynamoDB.
2. **Team snapshot** — request `team_snapshot` from orchestrator SQS queue; receive reply with current registry and all Agent Cards; populate local card cache.
3. **Registration** — publish own Agent Card to S3; post registration message to orchestrator SQS queue.
4. **Operation** — poll SQS queue; monitor Slack and Teams; spawn worker threads for tasks.
5. **Heartbeat** — periodic liveness message to orchestrator.
6. **Re-registration** — on receiving an orchestrator startup broadcast, re-register immediately.
7. **Shutdown** — on SIGTERM: checkpoint active worker state to memory, publish shutdown broadcast to SNS, flush budget counters to DynamoDB, drain workers, exit.

## Worker Thread

A worker thread is a self-contained agent harness. It receives a task and builds an initial context: SOUL.md, current todo list, skill index (name-and-summary stubs only), and the task definition. Additional material is loaded on demand via tool calls (progressive disclosure). A panic in a worker is recovered and logged — it does not affect the main thread or other workers.

### Context management

When the context window approaches a configurable token threshold, the harness checkpoints all durable state — todo list, any memory writes from the current session, structured task state — to the git-backed memory store, then starts a fresh worker thread reinitialised from the checkpoint. This is provider-agnostic and works identically across Bedrock, OpenAI-compatible, and Anthropic API providers.

## Built-in Harness Tools

The harness provides a fixed set of built-in tools, bound at startup from the bot's `allowed_tools` config:

| Tool | Purpose | Safety scope |
|---|---|---|
| `read_file`, `list_dir`, `glob`, `grep` | Filesystem reads | Workspace-scoped |
| `write_file`, `edit_file` | Filesystem writes | Workspace-scoped |
| `memory_search(query)` | S3 Vectors semantic retrieval | Namespaced to bot + team |
| `send_message`, `read_messages` | Bot-to-bot messaging | Audited; `receive_from` allowlist enforced on recipient |
| `todo_write`, `todo_read` | Persistent per-bot task list | Scoped to calling bot |
| `http_request` | HTTP to allowed external hosts | Allowlisted hosts; fully logged |
| `get_metrics` | Own operational metrics | Read-only, scoped to calling bot |

All file tools are constrained to the bot's workspace and memory directory. The harness enforces this at dispatch time.

### Tool Attention

Before injecting tool schemas into the model's context, the harness scores all available tools (built-in + MCP) against the current task intent using BM25. Only the top-k matched tools are injected as full JSON schemas; all others appear as compact name-and-summary stubs. The hard cap is 20 simultaneously injected full schemas. The scorer is a swappable interface — BM25 can be replaced with neural embeddings if quality degrades.

### Tool safety

- **Startup binding** — `allowed_tools` in `config.yaml` controls what the model can see. Tools outside the list are invisible.
- **Calibrated autonomy** — gate types (advisory, validating, blocking, escalating) are assigned per action based on reversibility. Approvals are never cached.
- **Prompt injection defence** — all tool outputs are sanitised before being returned to the model.
- **Budget caps** — token spend and tool call counts enforced by the harness; counters flushed to DynamoDB every 30 seconds, seeded from DynamoDB on startup.
- **Cross-bot messaging allowlists** — `receive_from` in `config.yaml` controls which bots can send action-triggering messages.

## Agent Skills

Skills are modular capability packages (a `SKILL.md` + optional scripts) stored in the bot's private S3 bucket or the shared team bucket. The harness maintains a skill index in the agent's context (names and one-line summaries). When a task matches a skill, the full `SKILL.md` is promoted into context and supporting scripts are made available as harness-executed tools.

Skills scripts run in restricted subprocesses: no inherited environment variables, filesystem access limited to a temporary working directory, network access constrained by the ECS task's security group.

## Memory

Each bot reads and writes structured memory via file tools on a local git-backed directory:
- **Structured memory** — local git repo synced to S3 via ETag comparison. Personal memory uses `ours` conflict resolution. Shared team memory writes are sent to the orchestrator via SQS and applied sequentially.
- **Semantic memory** — queried via `memory_search` against the bot's S3 Vectors index. The harness writes embeddings to S3 Vectors when an agent saves a memory worth indexing.

## Model Provider

Model providers are named and typed in config. The provider factory initialises each provider once and caches it. Workers call `factory.Get(name)` to obtain a provider and `provider.Invoke(prompt, options)` to call the model. Bedrock and OpenAI-compatible endpoints are the two supported types.

## MCP

At startup the bot loads `mcp.json` from the team S3 bucket (optional, absence logged by orchestrator), then loads `mcp.json` from its private S3 bucket (optional, absence silently ignored). The two configs are merged — private extends shared. Each server entry may include a typed `credential` field: `static_secret` resolves a Secrets Manager ARN using the bot's IAM role; `oauth2` is reserved. The MCP client is initialised from the merged config and made available to worker threads.

## Structured Delegation

When a bot delegates a task to a peer, it sends an A2A-shaped SQS envelope to the target bot's queue. The envelope carries a task lifecycle: `submitted → working → input-required → completed → failed`. The receiving bot posts status updates back to the delegating bot's queue. The delegating bot serves delegation lookups from its local Agent Card cache (no round-trip required).

## Orchestrator Mode

Enabled by `orchestrator.enabled: true` in config. Adds:

- **Control plane** — maintains the team registry in RDS MariaDB. Accepts registration, heartbeat, and deregistration messages. Stores Agent Card per bot. Enforces one-instance-per-agent-type.
- **team_snapshot** — responds to startup requests with the full current registry and all cached Agent Cards.
- **Agent Card distribution** — fetches each registering bot's Agent Card from S3 and broadcasts it via SNS.
- **Shared memory writes** — applies `memory_write` messages from bots to the team S3 bucket sequentially.
- **Kanban board** — manages work items in RDS MariaDB. States: backlog, in-progress, blocked, done. Notifies assigned bots on assignment. All mutations include client-supplied idempotency tokens.
- **Restart durability** — all message handlers are idempotent. SQS visibility timeouts re-deliver messages if the orchestrator crashes before acknowledging.
- **REST API** — JWT-authenticated access to control plane and board at `/api/v1/`. All 26 endpoints match the `baobotctl` CLI contract (auth, board, team, skills, users, profile, DLQ). Admin-only routes return 403 for non-admin callers.
- **Web UI** — HTMX Kanban board at `/`; auto-refreshes board columns and team health every 30 seconds without a full page reload.
- **User management** — two roles: Admin and User. JWT issued on login, forced password change on first use.

Conflict detection: on startup, broadcasts presence to SNS. If another orchestrator responds, logs an error and exits.
