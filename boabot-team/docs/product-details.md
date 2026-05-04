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
| `config.yaml` | Yes | Runtime config — bot name, type, model providers |
| `mcp.json` | No | Role-specific MCP tool configuration (extends shared team config) |

## CDK Per-Bot Provisioning

For each `enabled: true` entry in `team.yaml`, the CDK stack provisions:

- Private S3 memory bucket (S3 Vectors + S3 Files access enabled).
- SQS inbound queue with dead-letter queue (14-day retention, 3 retries before DLQ).
- IAM role with least-privilege policies: read/write own S3 bucket, read team S3 bucket, SQS send/receive own queue, SNS publish to broadcast topic, Bedrock InvokeModel, Secrets Manager read own secrets.
- ECS task definition referencing the shared ECR image, with environment variables injecting config path, queue ARL, bucket names, and SNS topic ARN.
- ECS service (desired count: 1).
- Secrets Manager entries for model provider credentials.

## Shared Infrastructure Dependency

The per-bot CDK stack imports the following from the shared stack (`boabot/cdk/`):
- ECS cluster ARN
- ECR repository URI
- SNS broadcast topic ARN
- Team S3 bucket name
- VPC and subnet IDs
- ALB ARN (orchestrator only)
