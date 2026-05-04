# Adding a Bot to the Team

This guide walks through defining a new bot and deploying it to the cluster.

## Step 1 — Create the bot directory

```bash
mkdir -p boabot-team/bots/<type>
```

Replace `<type>` with a lowercase name for the bot's role (e.g. `qa`, `devops`).

## Step 2 — Write SOUL.md

Define the bot's role, responsibilities, personality, and boundaries. Use an existing bot's `SOUL.md` as a reference.

```markdown
# <Role> — SOUL.md

You are the <role> of the BaoBot development team...

## Responsibilities
## Personality
## Boundaries
```

## Step 3 — Write AGENTS.md

Define the bot's public interface — what it does, what to send it, what it needs to work effectively, and what it will not do.

## Step 4 — Write config.yaml

```yaml
bot:
  name: <name>
  type: <type>

models:
  default: bedrock-claude
  providers:
    - name: bedrock-claude
      type: bedrock
      model_id: anthropic.claude-sonnet-4-6
      region: us-east-1
```

## Step 5 — Optionally add mcp.json

If this bot needs role-specific MCP tools beyond the shared team config, add `mcp.json` to the bot directory. Missing is fine — the bot will use only the shared config.

## Step 6 — Add to team.yaml

```yaml
team:
  # ... existing entries ...
  - name: <name>
    type: <type>
    enabled: false    # start disabled until reviewed
```

## Step 7 — Review

Have the SOUL.md and AGENTS.md reviewed. Confirm config.yaml has the correct model provider. Set `enabled: true` when ready.

## Step 8 — Deploy

```bash
cd boabot-team/cdk
cdk diff     # review what will be created
cdk deploy   # provision the bot's infrastructure
```

The bot's ECS service will start automatically. It will register with the orchestrator on first run.

## Step 9 — Update documentation

- Update `boabot-team/README.md` team table.
- Update `boabot-team/docs/product-summary.md` team roster.
- Update root `docs/product-summary.md` if the system-level team roster changes.
