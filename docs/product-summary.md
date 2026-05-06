# Product Summary — BaoBot Dev Team

BaoBot is a system of cooperative, always-on AI agents that function as a software development team. Agents are specialised by role, carry their own evolving memory, and communicate with each other and with human operators to coordinate and complete development work autonomously.

## Modules

- **boabot** — the agent runtime. A single Go binary that all agents run as local processes, differentiated by per-bot configuration and a customised system prompt (SOUL.md). No cloud account is required to self-host.
- **boabotctl** — the operator CLI. A kubectl-style tool for human operators to manage the team, the work board, users, and agent skills.
- **boabot-team** — the team definition. Bot personalities (SOUL.md, AGENTS.md) and configurations.

## Current Team

| Bot | Role |
|---|---|
| Orchestrator | Control plane, Kanban board, team registry, user access |
| Architect | Technical design and API contracts |
| Implementer | TDD-based code implementation |
| Reviewer | Code review and quality gate |
| Maintainer | Bug fixes, dependency updates, operational health |

## Key Capabilities

- Agents communicate via in-process message queues (direct) and in-process broadcaster (team-wide), with structured delegation messages (A2A-shaped envelopes with full task lifecycle tracking).
- Each agent maintains a local filesystem memory directory with optional scheduled GitHub git backup; semantic retrieval uses a local BM25 embedder and cosine similarity vector store.
- Work is tracked on an internal Kanban board with a browser-accessible web UI.
- Anthropic Claude is the primary model provider (via `ANTHROPIC_API_KEY`); AWS Bedrock and OpenAI-compatible endpoints are supported as optional alternatives.
- Agents are MCP clients, with shared and optional per-bot tool configuration.
- Every worker goroutine runs inside the agent harness, which provides built-in tools (filesystem, memory search, messaging, task tracking, HTTP, self-monitoring) alongside MCP tools.
- Tool Attention (BM25 scoring) keeps injected tool schemas under a 20-tool cap, preventing context bloat across large MCP deployments.
- Agent Skills are modular capability packages stored in the bot's memory directory, uploaded via `baobotctl`, and Admin-approved before agents can use them.
- Budget tracked locally per bot (token spend per day, tool calls per hour), persisted to `budget.json`.
- Heap watchdog monitors memory usage and shuts down gracefully if a hard limit is exceeded.

See [`product-details.md`](product-details.md) for the full feature specification.
