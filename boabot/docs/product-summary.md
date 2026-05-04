# Product Summary — boabot

`boabot` is the BaoBot agent runtime. It is a single Go binary and container image that all team bots run. Role differentiation is applied at runtime via injected configuration — the binary is the same, the bot's identity is not.

## What It Does

- Runs as a long-lived ECS process monitoring incoming messages from SQS, Slack, and Microsoft Teams.
- Spawns worker threads to execute tasks agentically using a configured language model.
- Accesses private and shared S3 memory (S3 Vectors for semantic search, S3 Files for structured storage).
- Registers with the orchestrator on startup and participates in team lifecycle broadcasts.
- Invokes language models via the provider abstraction (Bedrock or OpenAI-compatible).
- Connects to MCP servers as defined by shared and optional private `mcp.json` configuration.

## Orchestrator Mode

When `orchestrator.enabled: true` is set in config, the same binary additionally:
- Runs the control plane (team registry in RDS MariaDB).
- Runs the Kanban board (work tracking in RDS MariaDB).
- Serves the REST API and web UI via configured ports (fronted by ALB).
- Manages user authentication (JWT).
- Mediates all database writes on behalf of the team.
