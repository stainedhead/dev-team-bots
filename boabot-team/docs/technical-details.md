# Technical Details — boabot-team

## Directory Structure

```
bots/
  <type>/
    SOUL.md         # system prompt — role, personality, boundaries
    AGENTS.md       # public interface description
    config.yaml     # runtime configuration
    mcp.json        # optional role-specific MCP tools
team.yaml           # authoritative team manifest
```

## team.yaml Parsing

The `boabot` runtime (`TeamManager`) reads `team.yaml` at startup. Each enabled bot entry causes a goroutine to be started for that bot. Disabled entries are parsed but skipped.

## Per-Bot Runtime Resources

For each `enabled: true` entry in `team.yaml`, `TeamManager` provides:

```
in-process Queue (local/queue)
  └── Dead-letter handling (configurable retries before dropping)

in-process BotRegistry entry
  └── Agent Card stored locally

local/fs memory directory
  └── <memory.path>/<bot-name>/

local/budget tracker
  └── budget.json persisted per bot

local/vector store
  └── cosine similarity index per bot

optional GitHub backup
  └── scheduled git push per config.backup settings
```

## No Cloud Infrastructure Required

All bot resources are in-process or on the local filesystem. No AWS account, ECS cluster, S3 buckets, SQS queues, or DynamoDB tables are needed to run the team.
