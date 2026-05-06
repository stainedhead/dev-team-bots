# Product Details — BaoBot Dev Team

For the full authoritative specification see [`PRODUCT.md`](../PRODUCT.md) at the repository root. This document summarises the key product features at the system level.

## Agent Model

Every agent runs the same BaoBot binary, differentiated by:
- `SOUL.md` — role and personality system prompt
- `AGENTS.md` — public interface description for teammates and operators
- `config.yaml` — runtime configuration (model providers, allowed tools, budget caps, memory settings, etc.)
- `mcp.json` (optional, private) — role-specific MCP tool configuration

## Memory

Each agent has a local memory directory. Memory is organised in two tiers:

- **Structured memory** — a local filesystem directory managed by the `local/fs` adapter. Agents read and write memory using the same file tools they use for all other work. Optional scheduled GitHub git backup keeps memory durable across restarts (configurable cron; default every 30 minutes).
- **Semantic memory** — indexed locally via the BM25 feature-hash embedder and cosine similarity vector store. Queried via the `memory_search` harness tool, which returns ranked file paths and excerpts for the agent to fetch in full via file tools.

## Built-in Harness Tools

Every worker goroutine has access to a fixed set of built-in tools enforced by the harness:

| Tool | Purpose |
|---|---|
| `read_file`, `list_dir`, `glob`, `grep` | Filesystem reads (workspace-scoped) |
| `write_file`, `edit_file` | Filesystem writes (workspace-scoped) |
| `memory_search(query)` | Local vector store semantic retrieval |
| `send_message`, `read_messages` | Bot-to-bot messaging |
| `todo_write`, `todo_read` | Persistent per-bot task list |
| `http_request` | HTTP to allowlisted hosts |
| `get_metrics` | Own operational metrics |

Tool schemas are injected dynamically via BM25 scoring (Tool Attention) — only top-k matched tools appear as full schemas; all others are name-and-summary stubs. Hard cap: 20 simultaneously injected full schemas.

## Agent Skills

Skills are modular capability packages (a `SKILL.md` plus optional scripts) stored in the bot's memory directory. Operators upload skills via `baobotctl skills push`; an Admin promotes them via `baobotctl skills approve`. Skills scripts run in restricted subprocesses with stripped environment and limited filesystem scope.

## Messaging

- **In-process queues** — each bot has a dedicated in-process queue (`local/queue`) for direct messages, work assignments, and structured delegation messages.
- **In-process broadcaster** — a shared bus (`local/bus`) fans out to all bot queues for team-wide broadcasts (shutdown signals, Agent Card distribution).
- **Structured delegation** — bot-to-bot task delegation uses an A2A-shaped message envelope carrying a task lifecycle: `submitted → working → input-required → completed → failed`. The envelope schema is A2A-compatible for a future transport upgrade.

## Agent Cards and Discovery

Each bot publishes an Agent Card on startup. At registration, the orchestrator fetches the card and broadcasts it via the in-process bus. All running bots cache cards locally. The local registry is populated at startup via `BotRegistry`.

## Orchestrator

The orchestrator is a configuration-enabled mode of the standard bot. It provides:
- **Control plane** — team registry tracking active bots (type, Agent Card, heartbeat, status).
- **Kanban board** — work item tracking with backlog/in-progress/blocked/done states.
- **REST API** — authenticated access to both, served via configured ports.
- **Web UI** — browser-accessible Kanban board.
- **team_snapshot** — responds to startup requests with the current registry and all cached Agent Cards.

## Tool Safety

- **Startup binding** — each bot's `config.yaml` declares `allowed_tools`; the harness binds the tool set at startup. Tools outside the allowlist are invisible to the model.
- **Calibrated autonomy** — gate types (advisory, validating, blocking, escalating) are assigned per action based on reversibility. Approvals are never cached.
- **Prompt injection defence** — all tool outputs are sanitised before being returned to the model.
- **Budget caps** — token spend per day and tool calls per hour are enforced by the harness. Counters are persisted locally in `budget.json` and restored on startup.
- **Cross-bot allowlists** — `receive_from` in each bot's config controls which bots can send action-triggering messages.

## Context Management

- **Progressive disclosure** — each worker goroutine starts with SOUL.md, current todo list, skill index, and task definition. Additional material is fetched on demand.
- **Checkpoint-and-restart** — when the context window approaches capacity, durable state is checkpointed to the local memory store and a fresh worker goroutine is started from the checkpoint. Provider-agnostic.
- **Structured handoffs** — cross-goroutine and cross-bot delegations use a JSON handoff artifact (task state + progress note + git ref).

## User Access

- JWT-based authentication (username/password, forced change on first login).
- Two roles: **Admin** (full user management, skill approval) and **User** (own profile and work items).
- `baobotctl` CLI for terminal access; web browser for the Kanban board.

## Infrastructure

- **Local process** — bots run as goroutines inside a single binary; no cloud account required.
- **GitHub backup** — optional; configured per bot in `config.yaml`; token read from `~/.boabot/credentials` or `BOABOT_BACKUP_TOKEN` env var.
- **GitHub Actions** — two pipelines: boabot (test + lint + build), boabotctl (GitHub Releases).
- **Model providers** — Anthropic API (primary), AWS Bedrock (optional), OpenAI-compatible endpoints (optional, including Ollama).
