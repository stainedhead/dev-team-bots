# Architecture: BaoBot Dev Team

**Feature:** baobot-dev-team
**Date:** 2026-05-05
**Status:** Draft

---

## Architecture Overview

[TBD — high-level diagram description: ECS cluster, orchestrator service, worker bot services, shared infrastructure (RDS, DynamoDB, SQS, SNS, S3), OTel/CloudWatch, external callers]

---

## Component Architecture

### boabot (Agent Runtime)

```
boabot/
├── cmd/boabot/main.go          # wiring only
├── internal/
│   ├── domain/
│   │   ├── workitem/           # WorkItem entity, repository interface
│   │   ├── workflow/           # WorkflowDefinition, WorkflowStep, router interface
│   │   ├── scheduler/          # ScheduledTask entity, scheduler interface
│   │   ├── cost/               # Budget entity, CostEnforcer interface
│   │   ├── screening/          # ContentScreener interface
│   │   ├── auth/               # AuthProvider interface, JWT entity
│   │   ├── eta/                # ETAEstimator interface, ETACalibration entity
│   │   ├── rebalancing/        # RebalancingEngine interface
│   │   ├── metrics/            # MetricsStore interface, ViabilityMetrics entity
│   │   └── notification/       # NotificationSender interface
│   ├── application/
│   │   ├── workitem/           # CreateWorkItem, UpdateWorkItem, RouteWorkItem use cases
│   │   ├── workflow/           # AdvanceWorkflow, AssignBot, Rebalance use cases
│   │   ├── scheduler/          # RunScheduledTask, TriageIssue use cases
│   │   ├── cost/               # EnforceBudget, DailyCostReview use cases
│   │   ├── screening/          # ScreenContent use case
│   │   ├── auth/               # Login, ValidateToken, OAuthCallback use cases
│   │   ├── eta/                # EstimateETA, CalibrateETA use cases
│   │   └── metrics/            # RecordMetrics, GenerateViabilityReport use cases
│   └── infrastructure/
│       ├── aws/
│       │   ├── s3/             # S3 adapter (private bot buckets + shared memory)
│       │   ├── sqs/            # SQS adapter (per-bot queues, A2A envelope)
│       │   ├── sns/            # SNS adapter (notifications)
│       │   ├── bedrock/        # Bedrock adapter (model invocation)
│       │   ├── dynamodb/       # DynamoDB adapter (budget tracking)
│       │   └── secretsmanager/ # Secrets Manager adapter
│       ├── db/                 # PostgreSQL adapter (work items, workflow, metrics)
│       ├── otel/               # OTel provider, ADOT exporter setup
│       ├── auth/
│       │   ├── local/          # Local password auth + JWT issuance
│       │   └── oauth2/         # OAuth2/OIDC adapter (GitHub, Okta)
│       └── screening/          # Regex-based content screener
```

### boabotctl (Operator CLI)

```
boabotctl/
├── cmd/boabotctl/main.go
└── internal/
    ├── commands/               # work, workflow, status, cost, auth command groups
    └── client/                 # HTTP client to boabot API
```

### boabot-team (CDK Infrastructure)

```
boabot-team/
├── lib/
│   ├── shared-stack.ts         # RDS, DynamoDB, SNS, shared S3, shared IAM policy
│   └── bot-stack.ts            # ECS task def, IAM role (base policy + inline), SQS, private S3
└── bin/boabot-team.ts          # CDK app entry
```

---

## Layer Responsibilities

| Layer | Responsibility | Import Rule |
|-------|---------------|-------------|
| Domain | Entities, value objects, repository and service interfaces | No imports from application or infrastructure |
| Application | Use cases orchestrating domain logic | Imports domain only |
| Infrastructure | AWS SDK, DB, HTTP adapters satisfying domain interfaces | Imports domain and stdlib |
| cmd | Dependency wiring, startup | Imports all layers |

---

## Data Flow

[TBD — sequence of events for: work item submission, workflow step advancement, scheduled task execution, cost cap enforcement, content screening, rate-limit handling, bottleneck detection + rebalancing]

---

## Sequence Diagrams

[TBD — key flows: work item lifecycle, triage bot routing, ETA estimation, cost review, auth (local password + OAuth2)]

---

## Integration Points

| System | Direction | Protocol | Layer |
|--------|-----------|----------|-------|
| AWS Bedrock | Outbound | HTTPS (AWS SDK) | Infrastructure |
| AWS SQS | Bidirectional | HTTPS (AWS SDK) | Infrastructure |
| AWS SNS | Outbound | HTTPS (AWS SDK) | Infrastructure |
| AWS S3 | Bidirectional | HTTPS (AWS SDK) | Infrastructure |
| AWS DynamoDB | Bidirectional | HTTPS (AWS SDK) | Infrastructure |
| AWS RDS (PostgreSQL) | Bidirectional | TCP/TLS | Infrastructure |
| AWS Secrets Manager | Inbound | HTTPS (AWS SDK) | Infrastructure |
| GitHub (OAuth2) | Outbound | HTTPS | Infrastructure |
| Okta (OAuth2) | Outbound | HTTPS | Infrastructure |
| OTel/ADOT | Outbound | OTLP/HTTPS | Infrastructure |
| GitHub API (repo monitoring) | Outbound | HTTPS | Infrastructure |
| Slack / Teams | Outbound | HTTPS (webhook) | Infrastructure |

---

## Architectural Decisions

[TBD — populate during Phase 3; pre-seeded questions:]
- S3 Vectors vs. OpenSearch Serverless for semantic memory
- Web UI technology choice (Go-served HTML/HTMX vs. SPA)
- SQS per-bot queue vs. shared queue with filtering
- PostgreSQL vs. DynamoDB for work item state (durability vs. cost)
- OTel ADOT sidecar vs. standalone collector service
