# Product Details — boabot

## Agent Lifecycle

1. **Startup** — load config from file next to binary, load SOUL.md and mcp.json from S3.
2. **Registration** — post registration message to orchestrator SQS queue.
3. **Operation** — poll SQS queue; monitor Slack and Teams; spawn worker threads for tasks.
4. **Heartbeat** — periodic liveness message to orchestrator.
5. **Re-registration** — on receiving an orchestrator startup broadcast, re-register immediately.
6. **Shutdown** — on SIGTERM: drain workers, publish shutdown broadcast to SNS, deregister, exit.

## Worker Thread

A worker thread is a self-contained agent harness. It receives a task, reasons over it using the configured model, calls MCP tools as needed, and reports a result. A panic in a worker is recovered and logged — it does not affect the main thread or other workers.

## Memory

Each bot reads and writes to its private S3 bucket:
- **S3 Vectors** — for semantic retrieval (RAG). Written as embeddings; queried by similarity.
- **S3 Files** — for structured direct access. Written and read by key.

The shared team S3 bucket is accessed the same way.

## Model Provider

Model providers are named and typed in config. The provider factory initialises each provider once and caches it. Workers call `factory.Get(name)` to obtain a provider and `provider.Invoke(prompt, options)` to call the model. Bedrock and OpenAI-compatible endpoints are the two supported types.

## MCP

At startup the bot loads `mcp.json` from the team S3 bucket (optional, absence logged by orchestrator), then loads `mcp.json` from its private S3 bucket (optional, absence silently ignored). The two configs are merged — private extends shared. The MCP client is initialised from the merged config and made available to worker threads.

## Orchestrator Mode

Enabled by `orchestrator.enabled: true` in config. Adds:

- **Control plane** — maintains the team registry in RDS MariaDB. Accepts registration, heartbeat, and deregistration messages. Enforces one-instance-per-agent-type.
- **Kanban board** — manages work items in RDS MariaDB. States: backlog, in-progress, blocked, done. Notifies assigned bots on assignment.
- **REST API** — JWT-authenticated access to control plane and board at `/api/*`.
- **Web UI** — HTML Kanban board at `/*`.
- **User management** — two roles: Admin and User. JWT issued on login, forced password change on first use.

Conflict detection: on startup, broadcasts presence to SNS. If another orchestrator responds, logs an error and exits.
