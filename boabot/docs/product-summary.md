# Product Summary — boabot

`boabot` is the BaoBot agent runtime. It is a single Go binary and container image that all team bots run. Role differentiation is applied at runtime via injected configuration — the binary is the same, the bot's identity is not.

## What It Does

- Runs as a long-lived ECS process monitoring incoming messages from SQS, Slack, and Microsoft Teams.
- Spawns worker threads that execute tasks agentically using a configured language model, built-in harness tools, MCP tools, and Agent Skills.
- Maintains a local git-backed memory directory synced to its private S3 bucket (ETag-based object sync); uses S3 Vectors for semantic retrieval.
- On startup, requests a `team_snapshot` from the orchestrator to populate the local Agent Card cache, then registers with the orchestrator.
- Invokes language models via the provider abstraction (Bedrock or OpenAI-compatible).
- Connects to MCP servers as defined by shared and optional private `mcp.json` configuration, resolving credentials from Secrets Manager at startup.
- Enforces Tool Attention (BM25 scoring) to keep injected tool schemas under the 20-tool cap.
- Checkpoints durable state to memory and restarts worker threads when the context window approaches capacity.
- Tracks token spend and tool call counts in memory, flushed to DynamoDB every 30 seconds.

## Orchestrator Mode

When `orchestrator.enabled: true` is set in config, the same binary additionally:
- Runs the control plane (team registry in RDS MariaDB).
- Runs the Kanban board (work tracking in RDS MariaDB).
- Serves the REST API and web UI via configured ports (fronted by ALB).
- Manages user authentication (JWT).
- Serialises all writes to the shared team memory bucket.
- Fetches Agent Cards from S3 at bot registration and distributes them via SNS broadcast.
- Responds to `team_snapshot` requests from newly started bots.
- Mediates all database writes on behalf of the team via idempotent, SQS-driven handlers.
