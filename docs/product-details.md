# Product Details — BaoBot Dev Team

For the full authoritative specification see [`PRODUCT.md`](../PRODUCT.md) at the repository root. This document summarises the key product features at the system level.

## Agent Model

Every agent runs the same BaoBot binary, differentiated by:
- `SOUL.md` — role and personality system prompt
- `AGENTS.md` — public interface description for teammates and operators
- `config.yaml` — runtime configuration (model providers, queue ARNs, allowed tools, budget caps, etc.)
- `mcp.json` (optional, private) — role-specific MCP tool configuration with typed credential descriptors

All agents additionally load a shared `mcp.json` from the team S3 bucket (optional, absence is logged by the orchestrator).

## Memory

Each agent has a private S3 bucket. Memory is organised in two tiers:

- **Structured memory** — a local git-backed directory of files synced to S3 via ETag-based object comparison (not git remote). Agents read and write memory using the same file tools they use for all other work. Personal memory uses `ours` conflict resolution; shared team memory writes are serialised through the orchestrator via SQS.
- **Semantic memory** — indexed in S3 Vectors. Queried via the `memory_search` harness tool, which returns ranked file paths and excerpts for the agent to fetch in full via file tools.

## Built-in Harness Tools

Every worker thread has access to a fixed set of built-in tools enforced by the harness:

| Tool | Purpose |
|---|---|
| `read_file`, `list_dir`, `glob`, `grep` | Filesystem reads (workspace-scoped) |
| `write_file`, `edit_file` | Filesystem writes (workspace-scoped) |
| `memory_search(query)` | Semantic retrieval from S3 Vectors |
| `send_message`, `read_messages` | Bot-to-bot messaging |
| `todo_write`, `todo_read` | Persistent per-bot task list |
| `http_request` | HTTP to allowlisted hosts |
| `get_metrics` | Own operational metrics |

Tool schemas are injected dynamically via BM25 scoring (Tool Attention) — only top-k matched tools appear as full schemas; all others are name-and-summary stubs. Hard cap: 20 simultaneously injected full schemas.

## Agent Skills

Skills are modular capability packages (a `SKILL.md` plus optional scripts) stored in S3. Operators upload skills via `baobotctl skills push`; an Admin promotes them via `baobotctl skills approve`. Skills scripts run in restricted subprocesses with stripped environment and limited filesystem scope.

## Messaging

- **SQS** — each bot has a dedicated inbound queue for direct messages, work assignments, EventBridge events, and structured delegation messages.
- **SNS** — a shared topic fans out to all bot queues for team-wide broadcasts (orchestrator startup, shutdown signals, Agent Card distribution).
- **Structured delegation** — bot-to-bot task delegation uses an A2A-shaped SQS envelope carrying a task lifecycle: `submitted → working → input-required → completed → failed`. The envelope schema is A2A-compatible for a future transport upgrade.

## Agent Cards and Discovery

Each bot publishes a signed Agent Card to its S3 bucket. At registration, the orchestrator fetches the card and broadcasts it via SNS. All running bots cache cards locally. On startup, each bot requests a `team_snapshot` from the orchestrator to pre-populate its local card cache.

## Orchestrator

The orchestrator is a configuration-enabled mode of the standard bot. It provides:
- **Control plane** — team registry tracking active bots (type, SQS ARN, Agent Card, heartbeat, status).
- **Kanban board** — work item tracking with backlog/in-progress/blocked/done states.
- **REST API** — authenticated access to both, served via ALB at `/api/*`.
- **Web UI** — browser-accessible Kanban board at `/*`.
- **Shared memory writes** — serialises all writes to the team S3 bucket to eliminate concurrent write conflicts.
- **team_snapshot** — responds to startup requests with the current registry and all cached Agent Cards.

All database writes are mediated exclusively through the orchestrator. Orchestrator message handlers are idempotent; SQS visibility timeouts provide restart durability.

## Tool Safety

- **Startup binding** — each bot's `config.yaml` declares `allowed_tools`; the harness binds the tool set at startup. Tools outside the allowlist are invisible to the model.
- **Calibrated autonomy** — gate types (advisory, validating, blocking, escalating) are assigned per action based on reversibility. Approvals are never cached.
- **Prompt injection defence** — all tool outputs are sanitised before being returned to the model.
- **Budget caps** — token spend per day and tool calls per hour are enforced by the harness. Counters are flushed to DynamoDB every 30 seconds and seeded from DynamoDB on startup.
- **Cross-bot allowlists** — `receive_from` in each bot's config controls which bots can send action-triggering messages.

## Context Management

- **Progressive disclosure** — each worker thread starts with SOUL.md, current todo list, skill index, and task definition. Additional material is fetched on demand.
- **Checkpoint-and-restart** — when the context window approaches capacity, durable state is checkpointed to the git-backed memory store and a fresh worker thread is started from the checkpoint. Provider-agnostic.
- **Structured handoffs** — cross-thread and cross-bot delegations use a JSON handoff artifact (task state + progress note + git ref).

## User Access

- JWT-based authentication (username/password, forced change on first login).
- Two roles: **Admin** (full user management, skill approval) and **User** (own profile and work items).
- `baobotctl` CLI for terminal access; web browser for the Kanban board.

## Infrastructure

- **AWS ECS** — single cluster, one service per bot, shared container image.
- **AWS ALB** — routes `/api/*` to REST API, `/*` to web UI.
- **RDS MariaDB** — two instances: control plane DB and Kanban board DB.
- **DynamoDB** — shared budget counter table (`bot_id + window` key).
- **AWS CDK** — shared stack in `boabot/cdk/`, per-bot stack in `boabot-team/cdk/`.
- **GitHub Actions** — three pipelines: boabot (container + deploy), boabotctl (GitHub Releases), boabot-team (CDK).
