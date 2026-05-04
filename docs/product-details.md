# Product Details — BaoBot Dev Team

For the full authoritative specification see [`PRODUCT.md`](../PRODUCT.md) at the repository root. This document summarises the key product features at the system level.

## Agent Model

Every agent runs the same BaoBot binary, differentiated by:
- `SOUL.md` — role and personality system prompt
- `AGENTS.md` — public interface description for teammates and operators
- `config.yaml` — runtime configuration (model providers, queue ARNs, etc.)
- `mcp.json` (optional, private) — role-specific MCP tool configuration

All agents additionally load a shared `mcp.json` from the team S3 bucket (optional, absence is logged by the orchestrator).

## Memory

Each agent has a private S3 bucket with two access modes:
- **S3 Vectors** — semantic search and RAG retrieval.
- **S3 Files** — structured direct-access storage.

A shared team S3 bucket follows the same structure and is accessible by all agents.

## Messaging

- **SQS** — each bot has a dedicated inbound queue for direct messages, work assignments, and EventBridge events.
- **SNS** — a shared topic fans out to all bot queues for team-wide broadcasts (orchestrator startup, shutdown signals).

## Orchestrator

The orchestrator is a configuration-enabled mode of the standard bot. It provides:
- **Control plane** — team registry tracking active bots (type, SQS ARN, heartbeat, status).
- **Kanban board** — work item tracking with backlog/in-progress/blocked/done states.
- **REST API** — authenticated access to both, served via ALB at `/api/*`.
- **Web UI** — browser-accessible Kanban board at `/*`.

All database writes are mediated exclusively through the orchestrator.

## User Access

- JWT-based authentication (username/password, forced change on first login).
- Two roles: **Admin** (full user management) and **User** (own profile and work items).
- `baobotctl` CLI for terminal access; web browser for the Kanban board.

## Infrastructure

- **AWS ECS** — single cluster, one service per bot, shared container image.
- **AWS ALB** — routes `/api/*` to REST API, `/*` to web UI.
- **RDS MariaDB** — two instances: control plane DB and Kanban board DB.
- **AWS CDK** — shared stack in `boabot/cdk/`, per-bot stack in `boabot-team/cdk/`.
- **GitHub Actions** — two pipelines: boabot (container) and boabotctl (GitHub Releases).
