# boabot-team — Team Definition

Defines the BaoBot team: bot personalities, configurations, and per-bot AWS infrastructure.

## Documentation

- [`docs/product-summary.md`](docs/product-summary.md) — team overview
- [`docs/product-details.md`](docs/product-details.md) — bot roles and configurations
- [`docs/technical-details.md`](docs/technical-details.md) — CDK stack and infrastructure
- [`docs/architectural-decision-record.md`](docs/architectural-decision-record.md) — decisions specific to this directory

## User Documentation

- [`user-docs/adding-bots.md`](user-docs/adding-bots.md) — how to define and deploy a new bot

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
team.yaml           # authoritative deployment manifest
cdk/                # per-bot AWS infrastructure (CDK)
```

Each bot's CDK resources include a private S3 memory bucket (versioned, with S3 Vectors), an SQS inbound queue, an IAM role, and an ECS task definition and service. The shared stack (`boabot/cdk/`) must be deployed first — it provides the ECS cluster, ALB, SNS topic, team S3 bucket, DynamoDB budget table, and ECR repository.

## Deploying the Team

```bash
cd cdk
cdk diff    # review changes
cdk deploy  # provision or update per-bot infrastructure
```

## Adding a Bot

See [`user-docs/adding-bots.md`](user-docs/adding-bots.md) for the full process.
