# Product Summary — BaoBot Dev Team

BaoBot is a system of cooperative, always-on AI agents that function as a software development team. Agents are specialised by role, carry their own evolving memory, and communicate with each other and with human operators to coordinate and complete development work autonomously.

## Modules

- **boabot** — the agent runtime. A single Go binary and container image that all agents run, differentiated by per-bot configuration and a customised system prompt (SOUL.md).
- **boabotctl** — the operator CLI. A kubectl-style tool for human operators to manage the team, the work board, users, and agent skills.
- **boabot-team** — the team definition. Bot personalities (SOUL.md, AGENTS.md), configurations, and CDK infrastructure definitions.

## Current Team

| Bot | Role |
|---|---|
| Orchestrator | Control plane, Kanban board, team registry, user access |
| Architect | Technical design and API contracts |
| Implementer | TDD-based code implementation |
| Reviewer | Code review and quality gate |
| Maintainer | Bug fixes, dependency updates, operational health |

## Key Capabilities

- Agents monitor Slack and Microsoft Teams and respond to commands.
- Agents communicate via SQS queues (direct), SNS broadcast (team-wide), and structured delegation messages (A2A-shaped envelopes over SQS with full task lifecycle tracking).
- Each agent maintains a local git-backed memory directory synced to its private S3 bucket; S3 Vectors provides semantic retrieval on top.
- Work is tracked on an internal Kanban board with a browser-accessible web UI.
- Agents use AWS Bedrock and OpenAI-compatible models, configured per bot.
- Agents are MCP clients, with shared and optional per-bot tool configuration.
- Every worker thread runs inside the agent harness, which provides built-in tools (filesystem, memory search, messaging, task tracking, HTTP, self-monitoring) alongside MCP tools.
- Tool Attention (BM25 scoring) keeps injected tool schemas under a 20-tool cap, preventing context bloat across large MCP deployments.
- Agent Skills are modular capability packages stored in S3, uploaded via `baobotctl`, and Admin-approved before agents can use them.

See [`product-details.md`](product-details.md) for the full feature specification.
