# Configuration Reference — boabot

The agent reads `config.yaml` from the same directory as the binary by default. Override with `--config <path>`.

Credentials (API keys, tokens) are **never** stored in `config.yaml`. They are read from `~/.boabot/credentials` (INI format) or environment variables at startup.

## Minimal Required Fields

```yaml
bot:
  name: <string>      # unique name for this bot instance
  type: <string>      # bot type — must match a directory in boabot-team/bots/

models:
  default: <provider-name>   # name of the default provider
  providers:
    - name: <provider-name>
      type: anthropic         # or: bedrock | openai
      model_id: <string>      # e.g. claude-sonnet-4-6

budget:
  token_spend_daily: 1000000   # 0 = disabled
  tool_calls_hourly: 500       # 0 = disabled

context:
  threshold_tokens: 150000
```

## Team File

```yaml
team:
  file_path: ./team.yaml    # path to team.yaml (required if using TeamManager)
  bots_dir: ./bots          # directory containing per-bot subdirectories
```

## Memory

```yaml
memory:
  path: ./memory             # local directory for bot memory files (default: <binary-dir>/memory)
  vector_index: cosine       # only "cosine" supported today
  embedder: bm25             # "bm25" (default, no API key needed) | provider name (e.g. "openai")
  heap_warn_mb: 512          # log warning at this heap usage (0 = disabled)
  heap_hard_mb: 1024         # shut down gracefully at this heap usage (0 = disabled)
```

## GitHub Backup (Optional)

```yaml
backup:
  enabled: false
  schedule: "*/30 * * * *"    # cron expression (default: every 30 minutes)
  restore_on_empty: true       # clone from remote if local memory directory is empty on startup
  github:
    repo: org/repo             # e.g. myorg/baobot-memory
    branch: main
    author_name: BaoBot
    author_email: baobot@example.com
```

The GitHub token is read from `BOABOT_BACKUP_TOKEN` (env var) or the `backup_token` key in `~/.boabot/credentials`. It is never read from `config.yaml`.

## Orchestrator Mode

```yaml
orchestrator:
  enabled: false    # set to true to activate the control plane, Kanban board, REST API, and web UI
  api_port: 8080
  web_port: 8081
```

## Tools

```yaml
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
    - get_metrics
  http_allowed_hosts:     # hosts the http_request tool may contact
    - api.github.com
  receive_from:           # bot names permitted to send action-triggering messages to this bot
    - orchestrator
```

## Budget Caps

```yaml
budget:
  token_spend_daily: 1000000   # maximum tokens per calendar day (UTC); 0 = disabled
  tool_calls_hourly: 500       # maximum tool dispatches per hour; 0 = disabled
```

Counters are persisted to `budget.json` in the bot's memory directory and restored on startup.

## Context Management

```yaml
context:
  threshold_tokens: 150000   # context size at which checkpoint-and-restart is triggered
```

## Credentials File

API keys are stored in `~/.boabot/credentials` (INI format, mode 0600):

```ini
[default]
anthropic_api_key = sk-ant-...
backup_token = ghp_...

[staging]
anthropic_api_key = sk-ant-...
```

Select a non-default profile with `BOABOT_PROFILE=staging`. Values in the credentials file are applied only if the corresponding environment variable is not already set — environment variables always take precedence.

## Provider Types

| Type | Env var / credential key | Notes |
|---|---|---|
| `anthropic` | `ANTHROPIC_API_KEY` / `anthropic_api_key` | Primary provider. Any non-empty key accepted at startup. |
| `bedrock` | AWS SDK credentials (standard chain) | Requires AWS account; region set in provider config. |
| `openai` | `OPENAI_API_KEY` / `openai_api_key` | OpenAI-compatible; `endpoint` overrides base URL (e.g. for Ollama). |
