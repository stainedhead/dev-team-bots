# Product Summary — boabot

`boabot` is the BaoBot agent runtime. It is a single Go binary that all team bots run as local processes. Role differentiation is applied at runtime via injected configuration — the binary is the same, the bot's identity is not.

## What It Does

- Runs as a local process; no cloud account or AWS infrastructure is required to self-host.
- Bots communicate via an in-process message router (`local/queue` package) rather than a cloud messaging service.
- Spawns worker goroutines that execute tasks agentically using a configured language model, built-in harness tools, MCP tools, and Agent Skills.
- Maintains a local filesystem memory directory (`local/fs` package); optional scheduled GitHub git backup keeps memory durable across restarts (configurable, default every 30 minutes).
- Performs semantic search via a local BM25 feature-hash embedder (`local/bm25`) and cosine similarity vector store (`local/vector`) — no external embedding API required.
- Tracks token spend and tool call counts in a local budget tracker (`local/budget`), persisted to a JSON file.
- Monitors heap usage via a configurable watchdog (`local/watchdog`) that logs a warning at a soft limit and shuts down gracefully if the hard limit is exceeded.
- Anthropic Claude is the primary model provider, configured via `ANTHROPIC_API_KEY`; AWS Bedrock is supported as an optional alternative model provider via `internal/infrastructure/aws/bedrock`.
- Configuration loaded from per-bot `config.yaml` and a shared `team.yaml`; credentials loaded from `~/.boabot/credentials` INI file and environment variables — no secrets are stored in config files.

## Orchestrator Mode

When `orchestrator.enabled: true` is set in config, the same binary additionally:
- Runs the control plane (team registry).
- Runs the Kanban board (work tracking).
- Serves the REST API and web UI via configured ports.
- Manages user authentication (JWT).
- Maintains a dynamic pool of tech-lead instances, one per In Progress kanban item, with automatic allocation and deallocation as items change state.

## Dynamic Subteam Spawning (Tech-Lead Bots)

Tech-lead bots can spawn and manage named sub-agent goroutines at runtime. Each spawned sub-agent runs in complete isolation with its own message bus and queue router. Spawning and termination are triggered via typed messages on the existing queue. A heartbeat watchdog on each spawned agent ensures stale goroutines are self-terminated automatically. Sub-agent state is persisted to a session file so the tech-lead survives process restarts without losing context.
