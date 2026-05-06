# AGENTS.md — boabot-team

The team definition directory. Declares which bots exist, their personalities, and their per-bot infrastructure.

## Module Purpose

`boabot-team` is not a Go module — it contains:
- `team.yaml` — the authoritative list of bots, with enabled/disabled flags.
- `bots/<type>/` — per-bot directory containing `SOUL.md`, `AGENTS.md`, `config.yaml`, and optionally `mcp.json`.

The `boabot` runtime reads `team.yaml` at startup and starts all enabled bots as in-process goroutines. No cloud infrastructure is required.

## Adding a New Bot

1. Create `bots/<type>/` directory.
2. Write `SOUL.md` — role, responsibilities, personality, boundaries.
3. Write `AGENTS.md` — public interface: what the bot does, what to send it, what it needs, what it won't do.
4. Write `config.yaml` — bot name, type, model providers, allowed tools, budget caps. Use an existing bot as a reference.
5. Optionally add `mcp.json` — role-specific MCP tool configuration.
6. Add an entry to `team.yaml` (set `enabled: false` until ready to deploy).
7. Update `docs/` and `README.md` to reflect the new team member.

## Bot Directory Structure

```
bots/<type>/
├── SOUL.md         # system prompt — role, personality, boundaries
├── AGENTS.md       # public interface — what to send, what it needs, what it produces
├── config.yaml     # runtime config — bot name, type, model providers, tools, budget, context
└── mcp.json        # optional — role-specific MCP tool config with typed credential field
```

## Pull Requests

After opening a PR with `gh pr create`, immediately enable automerge:

```bash
gh pr merge --auto --merge <PR-number>
```

## Rules

- Every bot must have all three required files: `SOUL.md`, `AGENTS.md`, `config.yaml`.
- `team.yaml` is the single source of truth. Never configure bots outside this file and `bots/<type>/`.
- New bots start with `enabled: false` — they are defined before they are deployed.
- Never commit secrets, real API keys, or credentials into config files. Use a `config.example.yaml` as the template.

## Adding a Bot

```bash
# 1. Create the bot directory
mkdir -p bots/<type>

# 2. Create required files
# SOUL.md, AGENTS.md, config.yaml (see existing bots for reference)

# 3. Add to team.yaml (enabled: false)

# 4. After review, set enabled: true
# The boabot runtime will start the new bot goroutine on next launch
```

## Docs to Update When Changing This Directory

- `docs/product-summary.md` — if the team roster changes.
- `docs/technical-details.md` — if the directory structure changes.
- `README.md` — update the team table when bots are added or removed.
- `user-docs/adding-bots.md` — if the process for adding bots changes.
