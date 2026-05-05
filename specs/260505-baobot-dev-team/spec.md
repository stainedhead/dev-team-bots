# Feature Spec: BaoBot Dev Team

**Date:** 2026-05-05
**Source PRD:** [baobot-dev-team-PRD.md](baobot-dev-team-PRD.md)
**Status:** Draft

---

## Executive Summary

BaoBot is a hosted team of cooperative AI agents that extends a solo developer's effective working hours by continuing development work autonomously during offline periods. The system provides a production-grade orchestration runtime (`boabot`), an operator CLI (`boabotctl`), and a configurable team of AI bots (`boabot-team`) deployed on AWS ECS. All interactions flow through a single external identity ("BaoBot"), with internal routing, work management, scheduling, cost enforcement, and observability handled transparently. The project also serves as a first-principles validation of whether a strong individual contributor can multiply their delivery capacity by directing a small AI agent team.

---

## Problem Statement

An individual developer working alone lacks development capacity during offline hours — sleep, travel, and personal time. BaoBot addresses this by providing a hosted team of cooperative AI agents that continue working autonomously when the developer is unavailable, and that can be monitored and directed from any location (desktop or mobile). The project also serves as a first-principles exploration of the architectural design space for agent harness and orchestration layers, and will produce a recommendation on the viability of this model for company-wide adoption.

---

## Goals

1. Deliver a stable, production-quality orchestration and agent runtime that a solo developer can operate to complete real development work asynchronously.
2. Validate the "technical architect as product owner" model — demonstrating development throughput a strong IC can command managing a small AI agent team.
3. Produce a written recommendation on whether and how this architecture could be adopted within a company.
4. Support local password authentication as v1, with OAuth2 (GitHub and Okta) as a pluggable secondary flow.
5. Design provider and infrastructure layers to be cloud-portable, enabling future migration away from AWS without a rewrite.
6. Plan for a native React Native client (macOS-first) as a defined follow-on deliverable.

## Non-Goals

- Multi-tenant or enterprise SaaS deployment — single-operator system only.
- General-purpose agentic platform — purpose-built for dev-team workflows.
- Full A2A HTTP protocol in v1 — SQS transport with A2A-compatible envelope is the target; HTTP upgrade is future.
- Non-AWS infrastructure in v1 — AWS-native now, but design must not foreclose portability.

---

## User Requirements

### Functional Requirements

**FR-001:** The system must allow users to create and manage a prioritised queue of work items via `baobotctl` and the web UI.

**FR-002:** Work items must support two types: *ad-hoc* (immediate, one-off) and *scheduled* (recurring, defined by a cron-style schedule).

**FR-003:** The team must operate under a configurable development workflow with user-defined step definitions and role assignments per step. The default workflow shipped out of the box is:
> backlog → review → document (PRD) → review (PRD) → spec → implement → code-design-review → remediate → confirmation → analysis → done

**FR-004:** Ad-hoc work items must be routable through the configured workflow steps, with each step assigned to a bot role.

**FR-005:** Scheduled tasks must carry a system prompt describing the work, a schedule, and an assigned bot or role. They run as background activity and yield back to a bot's primary workflow participation when a workflow item requires attention.

**FR-006:** The system must support repository monitoring as a built-in scheduled task — a configured bot periodically scans assigned repositories for open issues and PRs.

**FR-007:** A designated triage bot must evaluate incoming issues and PRs and route them to one of: direct resolution, the development queue, or PO review.

**FR-008:** Bots must be configurable with a `SOUL.md`, an assigned skill set, and scheduled task assignments. All currently defined bot roles must be deployable in the first cluster deployment.

**FR-009:** Workflow step definitions and role assignments must be modifiable by an Admin without requiring a redeployment.

**FR-010:** The Kanban board and monitoring surfaces must visualise per-bot activity and workload — showing what each bot is currently doing, its queue depth, and recent throughput.

**FR-011:** The system must highlight critical path bottlenecks: when a blocked or overloaded bot is holding up downstream work, the board must surface this visually and raise an SNS notification to the PO.

**FR-012:** The orchestrator must support automatic work rebalancing — when a bottleneck is detected, the orchestrator (or a peer bot via bot-to-bot messaging) may reassign queued items to an available bot with the appropriate role, without human intervention. The rebalancing decision and outcome must be logged and visible to the operator.

**FR-013:** Work items may be created in a backlog state and remain unstarted; bots or users may add context, attachments, or review notes before work begins.

**FR-014:** The orchestrator must produce an estimated time-to-delivery and an ETA for start-of-work for each queued item. Estimation uses LLM-assessed task complexity converted to agent minutes via a calibration factor. Before sufficient history exists (default threshold: 10 completed tasks of the same type), a configurable seed multiplier is used: `human_man_days × 480 minutes × multiplier`, where the multiplier is a reduction factor reflecting agent speed advantage (observed range 60x–100x; default seed ~0.015). After the threshold is reached, the system replaces the seed with an observed ratio of `actual_agent_minutes ÷ human_estimate_minutes`. Rate-limited wait time is excluded from all timing calculations and reported separately.

**FR-015:** Work items and scheduled tasks must support a future start time, allowing work to be staged for overnight runs or lower-cost off-peak periods.

**FR-016:** Cost ceilings must be configurable at two scopes: per-bot (daily token and tool-call budget) and system-wide (daily and monthly aggregate). Both are enforced by the harness before any model invocation or tool call is dispatched.

**FR-017:** The orchestrator must perform a daily cost review using two independently configurable alert modes:
- **Daily spike alert:** If actual spend on any day exceeds the pro-rated daily budget (`monthly_cap ÷ days_in_month`) by more than a configurable threshold (default: 30%), send an SNS notification immediately.
- **Flat cap alert:** Send an SNS notification when cumulative monthly spend reaches a configurable percentage of the monthly cap (default: 80%).

Both modes may be active simultaneously. Each threshold is independently configurable per bot and at the system level.

**FR-018:** Notifications must be configurable at three levels: system-wide defaults, per-workflow, and per-task. Task and workflow settings override system defaults.

**FR-019:** Per-item notification triggers must be selectable by the PO: success, failure, and blockage after a configurable timeout. Each trigger is independently opt-in.

**FR-020:** All external content ingested by a bot — repository issue bodies, PR descriptions, inbound messages from unlisted senders, MCP tool outputs, and HTTP responses — must be wrapped in explicit untrusted-content delimiters in the prompt before being passed to the model. The model's system prompt must instruct it to treat delimited zones as data, not instructions.

**FR-021:** The harness must implement a content screening interface that pre-processes all external content before it is returned to the model. The v1 implementation must use rule-based detection (regex patterns targeting common prompt injection signatures). The interface must be designed to accept pluggable providers — Bedrock Guardrails or equivalent — as a drop-in replacement without changes to the screening call sites.

**FR-022:** The system must capture and expose the data needed to assess agent team viability: per-bot throughput, delivery accuracy (estimated vs. actual completion time), cost per task, and workflow step cycle times. This data must be queryable via `baobotctl` and visible on the monitoring surface. The orchestrator must produce a periodic summary report (at minimum weekly) delivered via SNS to the PO.

**FR-023:** The system must implement authentication as a pluggable provider interface with two concrete implementations: (1) local password auth with JWT sessions (v1, required), and (2) OAuth2 via an OIDC-compatible provider — GitHub and Okta are the preferred targets. The active provider is selected by configuration. Both providers must issue a JWT accepted by `baobotctl` and the web UI without modification to either client.

**FR-024:** When a model provider connection exhausts its token window, the harness must pause the affected worker and wait for the window to reset rather than failing the task. The orchestrator must be notified of the rate-limited state and must notify the PO via SNS. Rate-limited wait time must be excluded from delivery time and throughput calculations. The ETA must be updated in real time to reflect the estimated reset time, and rate-limited pauses must be reported separately in task completion summaries and the periodic viability report.

**FR-025:** The system must present a single external identity to all users and external systems — BaoBot. All inbound messages, commands, and API requests are addressed to BaoBot (the orchestrator). The existence, roles, and individual identities of worker bots are internal implementation details not exposed to external callers.

**FR-026:** All worker bots must share a common base IAM managed policy covering shared resources (Bedrock, SNS subscribe, shared S3 read, DynamoDB budget table read/write, Secrets Manager for shared secrets). Each worker bot's ECS task role is the base managed policy plus a single inline policy statement scoped to its own private S3 bucket ARN. The orchestrator has a separate IAM role with elevated permissions covering RDS, SNS publish, all S3 buckets (as sole writer to shared memory), DynamoDB, and SQS management. No worker bot role may access another bot's private S3 bucket.

---

## Non-Functional Requirements

| Category | Requirement |
|----------|-------------|
| **Reliability** | Retries with exponential backoff on all external service calls; bulkhead isolation between worker threads; health and readiness endpoints per bot service; dead-letter queues for unprocessable SQS messages |
| **Performance** | Responsive under solo-operator load; task submission acknowledged within seconds; status updates near-real-time |
| **Cost** | Per-bot daily caps and system-wide daily/monthly caps enforced as hard limits by the harness |
| **Security** | HTTPS/TLS on all external traffic; local password auth v1; OAuth2 pluggable secondary flow |
| **Observability** | OpenTelemetry structured logs, metrics, and distributed traces on all bot activity, model invocations, tool calls, and queue events. OTel export endpoint is operator-configured; AWS CloudWatch (via ADOT collector) is the default. Any OTLP-compatible backend is supported |
| **Notifications** | Configurable at system, workflow, and task levels; triggers include success, failure, and blockage-after-timeout; delivered via SNS |
| **Cloud Portability** | No AWS SDK import in domain or application code. All AWS-specific implementations satisfy domain interfaces from the infrastructure layer only |
| **Single External Identity** | Worker bots are not individually addressable externally. The orchestrator is the sole external contact point |

---

## System Architecture

### Affected Layers

- **Domain layer (`internal/domain/`):** New interfaces for WorkItem, Workflow, Bot, Scheduler, CostEnforcer, ContentScreener, AuthProvider, NotificationSender, MetricsStore, ETAEstimator.
- **Application layer (`internal/application/`):** Use cases for work item management, workflow routing, scheduling, cost enforcement, triage, rebalancing, ETA computation, viability reporting.
- **Infrastructure layer (`internal/infrastructure/`):** Implementations for S3, SQS, SNS, Bedrock (via AWS SDK), RDS (PostgreSQL), DynamoDB, Secrets Manager, OTel/CloudWatch.
- **CLI (`boabotctl/`):** Commands for work item CRUD, workflow management, status queries, cost report, auth login/logout.
- **CDK (`boabot-team/`):** Per-bot ECS task definitions, IAM roles, SQS queues, S3 buckets; shared stack for RDS, DynamoDB, SNS, shared S3.

### New / Modified Components

| Component | Type | Module |
|-----------|------|--------|
| WorkItem domain entity + repository interface | New | `boabot` |
| Workflow engine (step routing, role assignment) | New | `boabot` |
| Scheduler (cron-based task runner) | New | `boabot` |
| CostEnforcer (per-bot + system-wide caps) | New | `boabot` |
| ContentScreener (regex + pluggable interface) | New | `boabot` |
| AuthProvider (local password + JWT; OAuth2 stub) | New | `boabot` |
| ETAEstimator (seed multiplier + calibrated ratio) | New | `boabot` |
| RebalancingEngine (bottleneck detection + reassignment) | New | `boabot` |
| MetricsStore + ViabilityReporter | New | `boabot` |
| OTel instrumentation (all layers) | New | `boabot` |
| Kanban board web UI | New | `boabot` |
| `baobotctl` CLI commands | New/Extend | `boabotctl` |
| CDK: shared stack (RDS, DynamoDB, SNS, S3) | New/Extend | `boabot-team` |
| CDK: per-bot stack (ECS, IAM, SQS, S3) | New/Extend | `boabot-team` |

---

## Scope of Changes

### Files to Create

- `boabot/internal/domain/workitem/` — entity, repository interface, value objects
- `boabot/internal/domain/workflow/` — step, router, role assignment interfaces
- `boabot/internal/domain/scheduler/` — scheduler interface, task definition
- `boabot/internal/domain/cost/` — enforcer interface, budget entity
- `boabot/internal/domain/screening/` — content screener interface
- `boabot/internal/domain/auth/` — auth provider interface, JWT entity
- `boabot/internal/domain/eta/` — estimator interface, calibration entity
- `boabot/internal/domain/rebalancing/` — rebalancing interface
- `boabot/internal/domain/metrics/` — metrics store interface, viability report
- `boabot/internal/application/` — use cases for each domain area
- `boabot/internal/infrastructure/aws/` — S3, SQS, SNS, Bedrock, DynamoDB, Secrets Manager adapters
- `boabot/internal/infrastructure/db/` — RDS/PostgreSQL adapter
- `boabot/internal/infrastructure/otel/` — OTel provider setup
- `boabot/internal/infrastructure/auth/` — local password auth, OAuth2 adapter
- `boabot/internal/infrastructure/screening/` — regex screener implementation
- `boabotctl/cmd/` — CLI commands
- `boabot-team/lib/` — CDK stacks (shared + per-bot)

### Key Dependencies

- `aws-sdk-go-v2` (infrastructure layer only)
- `go-jwt/jwt` (auth)
- `robfig/cron` (scheduler)
- `go.opentelemetry.io/otel` (observability)
- `golang.org/x/crypto` (password hashing)
- PostgreSQL driver (`lib/pq` or `pgx`)
- `gorilla/mux` or `chi` (HTTP routing for web UI and API)

---

## Breaking Changes

- None anticipated for v1 (greenfield implementation on existing skeleton).
- Existing `boabot-team/team.yaml` schema will be extended; backward-compatible additions only.
- `boabot/config.yaml` schema will be extended with new sections (auth, cost, otel, workflow).

---

## Success Criteria and Acceptance Criteria

### Quality Gates

- All modules: `go fmt`, `go vet`, `golangci-lint` pass with zero warnings.
- Test coverage ≥ 90% on domain and application layers.
- All integration tests pass against localstack / test doubles.
- No AWS SDK import in domain or application packages (enforced by lint rule or CI check).

### Acceptance Criteria

- [ ] A solo operator can deploy the full stack using CDK with no manual AWS console steps.
- [ ] A work item can be created via `baobotctl` or the web UI, enter the configured workflow, and be completed by a bot without operator intervention.
- [ ] A scheduled task (e.g. repo monitor) runs on its configured schedule, produces triage output, and routes items into the work queue correctly.
- [ ] A bot blocked by a full queue or unavailable dependency triggers a critical path alert on the Kanban board and an SNS notification to the PO.
- [ ] The orchestrator automatically reassigns a blocked work item to an available bot with the correct role, logs the decision, and the result is visible on the board.
- [ ] The orchestrator produces an ETA and time-to-delivery estimate for a newly queued item, and updates it as conditions change.
- [ ] Work items with a future start time are not picked up by bots before that time.
- [ ] Per-bot and system-wide cost caps are enforced — a bot that hits its daily cap stops invoking models and logs the breach.
- [ ] Daily spike alert fires when single-day spend exceeds `monthly_cap ÷ days_in_month` by more than the configured threshold (default 30%).
- [ ] Flat cap alert fires when cumulative monthly spend crosses the configured threshold (default 80% of monthly cap).
- [ ] Both alert modes are independently configurable and can be active simultaneously.
- [ ] A user can log in via local password auth, receive a JWT, and use it for both `baobotctl` and the web UI without re-authenticating.
- [ ] OAuth2 (GitHub or Okta) can be configured as an alternative auth flow and used end-to-end in a test environment.
- [ ] External content (e.g. a GitHub issue body) is wrapped in untrusted-content delimiters before being passed to the model in all code paths.
- [ ] The harness screening layer detects and flags a known injection pattern in a tool output before it reaches the model.
- [ ] All bot activity, tool calls, and model invocations produce OTel traces visible in a connected observability backend.
- [ ] The written recommendation document addresses: measured agent throughput and delivery accuracy, actual infrastructure cost per task, workflow friction points and resolutions, and a go/no-go recommendation with stated conditions for company adoption.

---

## Risks and Mitigation

| Risk | Mitigation |
|------|------------|
| S3 Vectors unavailable in us-east-1 | Confirm GA status during research phase; fallback to OpenSearch Serverless |
| Agent cost overrun before caps are tuned | Default conservative caps; daily cost review alerts; seed multiplier calibration |
| Prompt injection via repo content | FR-020 (content segmentation) + FR-021 (regex screener); pluggable interface for future Bedrock Guardrails |
| Orchestrator single point of failure | SQS visibility timeouts; idempotent handlers; health/readiness endpoints |
| LLM output quality for complex dev tasks | Human review gate in workflow; confirmation step before merge |
| Company adoption recommendation scope creep | Scope defined: findings from one completed project cycle only |
| Bedrock model availability / regional quotas | Confirm model IDs and quotas before first deploy |

---

## Timeline and Milestones

| Milestone | Description |
|-----------|-------------|
| M1 — Domain + Core | Domain entities, interfaces, use cases; TDD baseline; 90%+ coverage |
| M2 — Infrastructure Adapters | AWS adapters (S3, SQS, SNS, Bedrock, RDS, DynamoDB); auth (local password + JWT) |
| M3 — Workflow Engine | Configurable workflow routing, work item lifecycle, scheduler, triage bot |
| M4 — Cost + Safety | Cost enforcement, content screener, rate-limit handling |
| M5 — Observability + Monitoring | OTel instrumentation, Kanban board, ETA estimator, viability reporting |
| M6 — CDK + Deploy | Full CDK stack, IAM policies, CI/CD workflows, end-to-end acceptance tests |
| M7 — Auth + CLI | OAuth2 pluggable provider, `baobotctl` full command set |
| M8 — Recommendation Report | Written viability recommendation document |

---

## References

- [baobot-dev-team-PRD.md](baobot-dev-team-PRD.md) — source PRD (co-located in this spec directory)
- `PRODUCT.md` — full product specification at repo root
- `AGENTS.md` — coding standards and dev-flow workflow
- `boabot-team/team.yaml` — bot roster and configuration
