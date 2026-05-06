# Product Details — boabot-team

## team.yaml

The single source of truth for which bots are started by the runtime. Fields per entry:

| Field | Required | Description |
|---|---|---|
| `name` | Yes | Unique bot name within the team |
| `type` | Yes | Bot type — maps to `bots/<type>/` directory |
| `enabled` | Yes | `true` to start, `false` to define without starting |
| `orchestrator` | No | `true` on the orchestrator bot only — enables control plane mode |

## Bot Directory Contents

| File | Required | Purpose |
|---|---|---|
| `SOUL.md` | Yes | System prompt — role, responsibilities, personality, boundaries |
| `AGENTS.md` | Yes | Public interface — what to send, what it needs, what it produces |
| `config.yaml` | Yes | Runtime config — bot name, type, model providers, tools, budget, memory |
| `mcp.json` | No | Role-specific MCP tool configuration (extends shared team config) |

## Agent Card

Each bot publishes an Agent Card on startup. The card describes the bot's capabilities, accepted message types, and delegation interface. The orchestrator fetches the card at registration time and broadcasts it via the in-process bus to all running bots, which cache it locally in the `BotRegistry`.
