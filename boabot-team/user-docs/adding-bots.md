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

aws:
  region: us-east-1
  sqs_queue_url: ""           # injected by CDK at deploy time
  sns_topic_arn: ""           # injected by CDK at deploy time
  private_bucket: ""          # injected by CDK at deploy time
  team_bucket: ""             # injected by CDK at deploy time
  dynamodb_budget_table: ""   # injected by CDK at deploy time

tools:
  allowed_tools:
    - read_file
    - list_dir
    - glob
    - grep
    - write_file
    - edit_file
    - memory_search
    - send_message
    - read_messages
    - todo_write
    - todo_read
    - http_request
  http_allowed_hosts:
    - api.github.com
  receive_from:
    - orchestrator

budget:
  token_spend_daily: 1000000
  tool_calls_hourly: 500

context:
  threshold_tokens: 150000
```

Leave the AWS fields empty — the CDK stack injects the real values via environment variables at deploy time.

## Step 5 — Optionally add mcp.json

If this bot needs role-specific MCP tools beyond the shared team config, add `mcp.json` to the bot directory. Missing is fine — the bot will use only the shared config.

```json
{
  "servers": [
    {
      "name": "example-tool",
      "url": "...",
      "credential": {
        "type": "static_secret",
        "secret_arn": "arn:aws:secretsmanager:us-east-1:123456789:secret:boabot/<name>-tool-key"
      }
    }
  ]
}
```

## Step 6 — Add to team.yaml

```yaml
team:
  # ... existing entries ...
  - name: <name>
    type: <type>
    enabled: false    # start disabled until reviewed
```

## Step 7 — Review

Have the `SOUL.md` and `AGENTS.md` reviewed. Confirm `config.yaml` has the correct model provider, tool allowlist, and budget caps. Set `enabled: true` when ready.

## Step 8 — Deploy

```bash
cd boabot-team/cdk
cdk diff     # review what will be created
cdk deploy   # provision the bot's infrastructure
```

The bot's ECS service will start automatically. On first run it will:
1. Publish its Agent Card to its private S3 bucket.
2. Request a `team_snapshot` from the orchestrator.
3. Register with the orchestrator.

## Step 9 — Update documentation

- Update `boabot-team/README.md` team table.
- Update `boabot-team/docs/product-summary.md` team roster.
- Update root `docs/product-summary.md` if the system-level team roster changes.
