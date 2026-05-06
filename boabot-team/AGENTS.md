# AGENTS.md — boabot-team

The team definition directory. Declares which bots exist, their personalities, and their per-bot infrastructure.

## Module Purpose

`boabot-team` is not a Go module — it contains:
- `team.yaml` — the authoritative list of bots to deploy, with enabled/disabled flags.
- `bots/<type>/` — per-bot directory containing `SOUL.md`, `AGENTS.md`, `config.yaml`, and optionally `mcp.json`.
- `cdk/` — AWS CDK stack that reads `team.yaml` and provisions per-bot infrastructure.

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

## CDK Stack

The `cdk/` stack reads `team.yaml` and for each enabled bot provisions:
- Private S3 memory bucket (S3 Vectors + S3 Files, versioning enabled).
- S3 prefix for Agent Card storage within the private bucket.
- SQS inbound queue with dead-letter queue (14-day retention, 3 retries).
- IAM role with least-privilege policies: own S3 bucket (r/w), team S3 bucket (r), own SQS queue, SNS publish, Bedrock InvokeModel, Secrets Manager own prefix, DynamoDB shared budget table (own items).
- ECS task definition and service (referencing the shared ECR image from `boabot/cdk`).
- Secrets Manager entries for model provider credentials.

Shared infrastructure ARNs (ECS cluster, ECR repo, SNS topic, team S3 bucket, DynamoDB table) are imported from the `boabot/cdk` stack via cross-stack references.

## Pull Requests

After opening a PR with `gh pr create`, immediately enable automerge:

```bash
gh pr merge --auto --merge <PR-number>
```

## Rules

- Every bot must have all three required files: `SOUL.md`, `AGENTS.md`, `config.yaml`.
- `team.yaml` is the single source of truth for what is deployed. Do not deploy bots by hand.
- The CDK stack should be re-run after any change to `team.yaml`.
- New bots start with `enabled: false` — they are defined before they are deployed.
