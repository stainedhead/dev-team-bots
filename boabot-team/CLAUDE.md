# CLAUDE.md — boabot-team

Team definition and CDK infrastructure directory. See root `CLAUDE.md` for repo-wide rules.

## What This Directory Does

Defines the team: who the bots are, what their roles and personalities are, and what infrastructure they need. The CDK stack in `cdk/` reads `team.yaml` and reconciles the cluster.

## Critical Rules

- `team.yaml` is the single source of truth. Never provision bot infrastructure by hand.
- New bots must have `SOUL.md`, `AGENTS.md`, and `config.yaml` before being added to `team.yaml`.
- Set `enabled: false` for new bots until they are ready to deploy and have been reviewed.
- CDK changes follow TDD where possible: write a failing CDK assertion test before changing a stack construct.
- Never commit secrets, real config values, or AWS account IDs into CDK or config files.

## Adding a Bot

```bash
# 1. Create the bot directory
mkdir -p bots/<type>

# 2. Create required files
# SOUL.md, AGENTS.md, config.yaml (see existing bots for reference)

# 3. Add to team.yaml (enabled: false)

# 4. After review, set enabled: true and run CDK
cd cdk && cdk deploy
```

## CDK

```bash
cd cdk
npm install          # CDK dependencies
cdk synth            # synthesise — review before deploying
cdk diff             # show what will change
cdk deploy           # deploy per-bot infrastructure
```

CDK imports shared stack outputs (ECS cluster ARN, ECR repo URI, SNS topic ARN, etc.) — the shared stack in `boabot/cdk/` must be deployed first.

## CI/CD

The GitHub Actions workflow for this directory is `.github/workflows/boabot-team.yml` at the repository root. It triggers on changes to `boabot-team/**`. On a PR it runs CDK assertion tests and posts a CDK diff comment. On merge to main it runs `cdk deploy`.

## Docs to Update When Changing This Directory

- `docs/product-summary.md` — if the team roster changes.
- `docs/technical-details.md` — if the CDK stack structure changes.
- `README.md` — update the team table when bots are added or removed.
- `user-docs/adding-bots.md` — if the process for adding bots changes.
