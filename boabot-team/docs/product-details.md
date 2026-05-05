# Product Details — boabot-team

## team.yaml

The single source of truth for what is deployed to the cluster. Fields per entry:

| Field | Required | Description |
|---|---|---|
| `name` | Yes | Unique bot name within the team |
| `type` | Yes | Bot type — maps to `bots/<type>/` directory |
| `enabled` | Yes | `true` to deploy, `false` to define without deploying |
| `orchestrator` | No | `true` on the orchestrator bot only — enables control plane mode |

## Bot Directory Contents

| File | Required | Purpose |
|---|---|---|
| `SOUL.md` | Yes | System prompt — role, responsibilities, personality, boundaries |
| `AGENTS.md` | Yes | Public interface — what to send, what it needs, what it produces |
| `config.yaml` | Yes | Runtime config — bot name, type, model providers, tools, budget |
| `mcp.json` | No | Role-specific MCP tool configuration (extends shared team config) |

## Agent Card

Each bot publishes an Agent Card to its private S3 bucket on startup. The card describes the bot's capabilities, accepted message types, and delegation interface. The orchestrator fetches the card at registration time and distributes it via SNS broadcast to all running bots, which cache it locally. Bots also request a `team_snapshot` on startup to populate the full card cache before accepting delegated tasks.

## CDK Per-Bot Provisioning

For each `enabled: true` entry in `team.yaml`, the CDK stack provisions:

- Private S3 memory bucket (S3 Vectors + S3 Files access enabled; versioning enabled for durability).
- S3 prefix for Agent Card storage within the private bucket.
- SQS inbound queue with dead-letter queue (14-day retention, 3 retries before DLQ).
- DynamoDB item in the shared budget table (keyed by bot ID + window) — provisioned by the bot at startup, but the table itself is in the shared stack.
- IAM role with least-privilege policies:
  - S3 read/write own private bucket
  - S3 read team shared bucket
  - SQS send/receive/delete own queue
  - SNS publish to broadcast topic
  - Bedrock InvokeModel
  - Secrets Manager read own path prefix
  - DynamoDB read/write shared budget table (own items only)
- ECS task definition referencing the shared ECR image, with environment variables injecting config path, queue URL, bucket names, SNS topic ARN, and DynamoDB table name.
- ECS service (desired count: 1).
- Secrets Manager entries for model provider credentials.

## Shared Infrastructure Dependency

The per-bot CDK stack imports the following from the shared stack (`boabot/cdk/`):
- ECS cluster ARN
- ECR repository URI
- SNS broadcast topic ARN
- Team S3 bucket name
- DynamoDB budget table name
- VPC and subnet IDs
- ALB ARN (orchestrator only)
