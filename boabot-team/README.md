# boabot-team — Team Definition

Defines the BaoBot team: bot personalities and per-bot runtime configurations.

## Documentation

- [`docs/product-summary.md`](docs/product-summary.md) — team overview
- [`docs/product-details.md`](docs/product-details.md) — bot roles and configurations
- [`docs/technical-details.md`](docs/technical-details.md) — directory structure and runtime resources
- [`docs/architectural-decision-record.md`](docs/architectural-decision-record.md) — decisions specific to this directory

## User Documentation

- [`user-docs/adding-bots.md`](user-docs/adding-bots.md) — how to define a new bot

## Current Team

| Bot | Type | Enabled |
|---|---|---|
| orchestrator | orchestrator | Yes |
| architect | architect | No |
| implementer | implementer | No |
| reviewer | reviewer | No |
| maintainer | maintainer | No |

## Structure

```
bots/
  <type>/
    SOUL.md         # system prompt — role, personality, boundaries
    AGENTS.md       # public interface description
    config.yaml     # runtime configuration
    mcp.json        # optional role-specific MCP tools
team.yaml           # authoritative team manifest
```

Bots run as goroutines inside the `boabot` binary — no cloud infrastructure is required. The `boabot` runtime reads `team.yaml` at startup and starts all enabled bots as in-process goroutines.

## Adding a Bot

See [`user-docs/adding-bots.md`](user-docs/adding-bots.md) for the full process.
