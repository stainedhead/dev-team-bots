# CLAUDE.md — boabot-team

Team definition directory. See root `CLAUDE.md` for repo-wide rules.

## What This Directory Does

Defines the team: who the bots are, what their roles and personalities are, and how they are configured. The `boabot` runtime reads `team.yaml` at startup and starts all enabled bots as in-process goroutines. No cloud infrastructure is required.

## Critical Rules

- `team.yaml` is the single source of truth. Never configure bots outside this file and `bots/<type>/`.
- New bots must have `SOUL.md`, `AGENTS.md`, and `config.yaml` before being added to `team.yaml`.
- Set `enabled: false` for new bots until they are ready and have been reviewed.
- Never commit secrets, real config values, or credentials into config files.

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
