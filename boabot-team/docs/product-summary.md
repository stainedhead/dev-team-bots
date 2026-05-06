# Product Summary — boabot-team

`boabot-team` is the team definition directory. It declares which bots exist, their personalities and roles, and the per-bot runtime configuration required to run each one.

## What It Contains

- `team.yaml` — the authoritative team manifest. Lists every bot by name and type, with an enabled flag controlling whether it is started by the runtime.
- `bots/<type>/` — per-bot personality and configuration: `SOUL.md` (system prompt), `AGENTS.md` (public interface), `config.yaml` (runtime config), and optional `mcp.json` (role-specific MCP tools).

## Current Team

| Bot | Role | Enabled |
|---|---|---|
| orchestrator | Control plane, Kanban board, REST API | Yes |
| architect | Technical design and API contracts | No |
| implementer | TDD-based code implementation | No |
| reviewer | Code review and quality gate | No |
| maintainer | Bug fixes, dependency updates, health | No |
