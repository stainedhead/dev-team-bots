# Configuration Reference — boabot

The agent reads `config.yaml` from the same directory as the binary by default. Override with `--config <path>`.

Use `config.example.yaml` (in this directory) as a starting point. Never commit a real config file.

## Required Fields

```yaml
bot:
  name: <string>      # unique name for this bot instance
  type: <string>      # bot type — must match a directory in boabot-team/bots/

aws:
  region: <string>               # e.g. us-east-1
  sqs_queue_url: <string>        # injected by CDK at deploy time
  sns_topic_arn: <string>        # injected by CDK at deploy time
  private_bucket: <string>       # injected by CDK at deploy time
  team_bucket: <string>          # injected by CDK at deploy time
  dynamodb_budget_table: <string> # injected by CDK at deploy time

models:
  default: <provider-name>   # name of the default provider
  providers:
    - name: <string>
      type: bedrock           # or: openai
      model_id: <string>
      region: <string>        # bedrock only
      endpoint: <url>         # openai only
```

## Orchestrator Mode

```yaml
orchestrator:
  enabled: true     # false by default
  api_port: 8080    # REST API port
  web_port: 8081    # web UI port
```

## Tools

```yaml
tools:
  allowed_tools:          # built-in tools this bot is permitted to use
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
    - get_metrics
  http_allowed_hosts:     # hosts the http_request tool may contact
    - api.github.com
  receive_from:           # bot names permitted to send action-triggering messages to this bot
    - orchestrator
```

## Budget Caps

```yaml
budget:
  token_spend_daily: 1000000   # maximum tokens per calendar day (UTC)
  tool_calls_hourly: 500       # maximum tool dispatches per hour
```

Budget counters are maintained in memory and flushed to DynamoDB every 30 seconds. On startup they are seeded from DynamoDB so caps survive process restarts.

## Context Management

```yaml
context:
  threshold_tokens: 150000   # context size at which checkpoint-and-restart is triggered
```

## Secrets

API keys, database credentials, and MCP server credentials are not in the config file. They are loaded from AWS Secrets Manager at startup using the bot's IAM role. The config file is safe to inspect — it contains no secrets.

MCP server credentials are referenced in `mcp.json` using a typed `credential` field:

```json
{
  "servers": [
    {
      "name": "github",
      "url": "...",
      "credential": {
        "type": "static_secret",
        "secret_arn": "arn:aws:secretsmanager:us-east-1:123456789:secret:boabot/github-token"
      }
    }
  ]
}
```

Supported credential types: `static_secret` (Secrets Manager ARN lookup). `oauth2` is reserved for future implementation.
