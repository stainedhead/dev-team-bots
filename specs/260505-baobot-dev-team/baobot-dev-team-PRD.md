# PRD: BaoBot Dev Team

**Created:** 2026-05-05
**Jira:** N/A
**Status:** Draft

## Problem Statement

An individual developer working alone lacks development capacity during offline hours — sleep, travel, and personal time. BaoBot addresses this by providing a hosted team of cooperative AI agents that continue working autonomously when the developer is unavailable, and that can be monitored and directed from any location (desktop or mobile). The project also serves as a first-principles exploration of the architectural design space for agent harness and orchestration layers — what it actually takes to build a production-grade, always-on, multi-agent system — and will produce a recommendation on the viability of this model for company-wide adoption.

## Goals

1. Deliver a stable, production-quality orchestration and agent runtime that a solo developer can operate to complete real development work asynchronously.
2. Validate the "technical architect as product owner" model — demonstrating how much development throughput a strong individual contributor can command by managing a small AI agent team, without traditional staffing investment.
3. Produce a written recommendation on whether and how this architecture could be adopted within a company to enable architects with product ownership to drive delivery through agent teams.
4. Support local password authentication as the v1 auth flow, with OAuth2 (GitHub and Okta preferred) as a pluggable secondary flow ahead of any production or company deployment.
5. Design provider and infrastructure layers to be cloud-portable, enabling future migration away from AWS without requiring a rewrite.
6. Plan for a native React Native client (macOS-first, Windows as a later corporate target) as a defined follow-on deliverable.

## Non-Goals

- Multi-tenant or enterprise SaaS deployment — this is a single-operator system.
- General-purpose agentic platform — purpose-built for dev-team workflows, not a generic agent framework.
- Full A2A HTTP protocol in v1 — SQS transport with A2A-compatible envelope shape is the current target; HTTP upgrade is future.
- Non-AWS infrastructure in v1 — AWS-native now, but the design must not foreclose future portability.

## Functional Requirements

**FR-001:** The system must allow users to create and manage a prioritised queue of work items via `baobotctl` and the web UI.

**FR-002:** Work items must support two types: *ad-hoc* (immediate, one-off) and *scheduled* (recurring, defined by a cron-style schedule).

**FR-003:** The team must operate under a configurable development workflow with user-defined step definitions and role assignments per step. The default workflow shipped out of the box is:

> backlog → review → document (PRD) → review (PRD) → spec → implement → code-design-review → remediate → confirmation → analysis → done

**FR-004:** Ad-hoc work items must be routable through the configured workflow steps, with each step assigned to a bot role.

**FR-005:** Scheduled tasks must carry a system prompt describing the work, a schedule, and an assigned bot or role. They run as background activity and yield back to a bot's primary workflow participation when a workflow item requires attention.

**FR-006:** The system must support repository monitoring as a built-in scheduled task — a configured bot periodically scans assigned repositories for open issues and PRs.

**FR-007:** A designated triage bot must evaluate incoming issues and PRs and route them to one of: direct resolution (assigned to a worker bot), the development queue (enters the standard workflow), or PO review (held for human assessment).

**FR-008:** Bots must be configurable with a `SOUL.md` (identity and behavioural boundaries), an assigned skill set, and scheduled task assignments. All currently defined bot roles must be deployable in the first cluster deployment.

**FR-009:** Workflow step definitions and role assignments must be modifiable by an Admin without requiring a redeployment.

**FR-010:** The Kanban board and monitoring surfaces must visualise per-bot activity and workload — showing what each bot is currently doing, its queue depth, and recent throughput.

**FR-011:** The system must highlight critical path bottlenecks: when a blocked or overloaded bot is holding up downstream work, the board must surface this visually and raise an SNS notification to the PO.

**FR-012:** The orchestrator must support automatic work rebalancing — when a bottleneck is detected, the orchestrator (or a peer bot via bot-to-bot messaging) may reassign queued items to an available bot with the appropriate role, without requiring human intervention. The rebalancing decision and outcome must be logged and visible to the operator.

**FR-013:** Work items may be created in a backlog state and remain unstarted; bots or users may add context, attachments, or review notes before work begins.

**FR-014:** The orchestrator must produce an estimated time-to-delivery and an ETA for start-of-work for each queued item. Estimation uses LLM-assessed task complexity converted to agent minutes via a calibration factor. Before sufficient history exists (default threshold: 10 completed tasks of the same type), a configurable seed multiplier is used: `human_man_days × 480 minutes × multiplier`, where the multiplier is a reduction factor reflecting agent speed advantage (observed range 60x–100x; default seed ~0.015). After the threshold is reached, the system replaces the seed with an observed ratio of `actual_agent_minutes ÷ human_estimate_minutes`. Rate-limited wait time (FR-024) is excluded from all timing calculations and reported separately.

**FR-015:** Work items and scheduled tasks must support a future start time, allowing work to be deliberately staged for overnight runs or lower-cost off-peak periods.

**FR-016:** Cost ceilings must be configurable at two scopes: per-bot (daily token and tool-call budget) and system-wide (daily and monthly aggregate). Both are enforced by the harness before any model invocation or tool call is dispatched.

**FR-017:** The orchestrator must perform a daily cost review using two independently configurable alert modes:
- **Daily spike alert:** Compute the pro-rated daily budget as `monthly_cap ÷ days_in_month`. If actual spend on any day exceeds this by more than a configurable threshold (default: 30%), send an SNS notification to the PO immediately.
- **Flat cap alert:** Send an SNS notification when cumulative monthly spend reaches a configurable percentage of the monthly cap (default: 80%).

Both modes may be active simultaneously. Each threshold is independently configurable per bot and at the system level.

**FR-018:** Notifications must be configurable at three levels: system-wide defaults, per-workflow, and per-task. Task and workflow settings override system defaults.

**FR-019:** Per-item notification triggers must be selectable by the PO: success, failure, and blockage after a configurable timeout. Each trigger is independently opt-in.

**FR-020:** All external content ingested by a bot — repository issue bodies, PR descriptions, inbound messages from unlisted senders, MCP tool outputs, and HTTP responses — must be wrapped in explicit untrusted-content delimiters in the prompt before being passed to the model. The model's system prompt must instruct it to treat delimited zones as data, not instructions.

**FR-021:** The harness must implement a content screening interface that pre-processes all external content before it is returned to the model. The v1 implementation must use rule-based detection (regex patterns targeting common prompt injection signatures). The interface must be designed to accept pluggable providers — Bedrock Guardrails or equivalent — as a drop-in replacement without changes to the screening call sites.

**FR-022:** The system must capture and expose the data needed to assess agent team viability: per-bot throughput (tasks completed per period), delivery accuracy (estimated vs. actual completion time), cost per task, and workflow step cycle times. This data must be queryable via `baobotctl` and visible on the monitoring surface. The orchestrator must produce a periodic summary report (at minimum weekly) delivered via SNS to the PO.

**FR-023:** The system must implement authentication as a pluggable provider interface with two concrete implementations: (1) local password auth with JWT sessions (v1, required), and (2) OAuth2 via an OIDC-compatible provider — GitHub and Okta are the preferred targets. The active provider is selected by configuration. Both providers must issue a JWT that is accepted by `baobotctl` and the web UI without modification to either client.

**FR-024:** When a model provider connection exhausts its token window, the harness must pause the affected worker and wait for the window to reset rather than failing the task. The orchestrator must be notified of the rate-limited state and must notify the PO via SNS. Rate-limited wait time must be excluded from delivery time and throughput calculations. The ETA must be updated in real time to reflect the estimated reset time, and rate-limited pauses must be reported separately in task completion summaries and the periodic viability report (FR-022).

**FR-025:** The system must present a single external identity to all users and external systems — BaoBot. All inbound messages, commands, and API requests are addressed to BaoBot (the orchestrator). The existence, roles, and individual identities of worker bots are internal implementation details not exposed to external callers. Users interact with one agent; the orchestrator routes work internally.

**FR-026:** All worker bots must share a common base IAM managed policy covering shared resources (Bedrock, SNS subscribe, shared S3 read, DynamoDB budget table read/write, Secrets Manager for shared secrets). Each worker bot's ECS task role is the base managed policy plus a single inline policy statement scoped to its own private S3 bucket ARN. The orchestrator has a separate IAM role with elevated permissions covering RDS, SNS publish, all S3 buckets (as sole writer to shared memory), DynamoDB, and SQS management. No worker bot role may access another bot's private S3 bucket.

## Non-Functional Requirements

- **Reliability:** Retries with exponential backoff on all external service calls, bulkhead isolation between worker threads, health and readiness endpoints per bot service, dead-letter queues for unprocessable SQS messages.
- **Performance:** Responsive under solo-operator load; task submission acknowledged within seconds; status updates near-real-time. No over-engineering for scale beyond the system's intended use.
- **Cost:** Per-bot daily caps and system-wide daily/monthly caps enforced as hard limits by the harness. The orchestrator performs a daily automated cost review and notifies the PO via SNS before limits are approached. Operational cost must remain predictable and reasonable for a solo operator.
- **Security:** HTTPS/TLS on all external traffic. Local password authentication is the v1 flow. OAuth2 (GitHub and Okta preferred) is integrated as a pluggable secondary flow ahead of any production or company deployment.
- **Observability:** OpenTelemetry structured logs, metrics, and distributed traces on all bot activity, model invocations, tool calls, and queue events. The OTel export endpoint is operator-configured; AWS CloudWatch (via ADOT collector) is the default. The backend is provider-agnostic — any OTLP-compatible receiver is supported.
- **Notifications:** Configurable at system, workflow, and task levels; triggers include success, failure, and blockage-after-timeout; delivered via SNS to configured recipients.
- **Architecture (cloud portability):** No AWS SDK import may appear in domain or application code. All AWS-specific implementations (S3, SQS, Bedrock, RDS, etc.) must satisfy domain interfaces from the infrastructure layer only. This boundary is the portability seam — a future cloud migration replaces infrastructure implementations without touching domain or application logic.
- **Architecture (single external identity):** Worker bots are not individually addressable from outside the system. The orchestrator is the sole external contact point. Internal work routing between the orchestrator and worker bots is an implementation concern invisible to callers. Per-bot SQS queues, if used, are internal infrastructure — not published addresses.

## Acceptance Criteria

- [ ] A solo operator can deploy the full stack (shared infrastructure + all defined bots) using CDK with no manual AWS console steps.
- [ ] A work item can be created via `baobotctl` or the web UI, enter the configured workflow, and be completed by a bot without operator intervention.
- [ ] A scheduled task (e.g. repo monitor) runs on its configured schedule, produces triage output, and routes items into the work queue correctly.
- [ ] A bot blocked by a full queue or unavailable dependency triggers a critical path alert on the Kanban board and an SNS notification to the PO.
- [ ] The orchestrator automatically reassigns a blocked work item to an available bot with the correct role, logs the decision, and the result is visible on the board.
- [ ] The orchestrator produces an ETA and time-to-delivery estimate for a newly queued item, and updates it as conditions change.
- [ ] Work items with a future start time are not picked up by bots before that time.
- [ ] Per-bot and system-wide cost caps are enforced — a bot that hits its daily cap stops invoking models and logs the breach.
- [ ] Given a configured monthly cap, if a bot's single-day spend exceeds `monthly_cap ÷ days_in_month` by more than the configured spike threshold (default: 30%), an SNS alert is sent that day. Given cumulative monthly spend crossing the configured flat threshold (default: 80% of monthly cap), a second independent SNS alert is sent. Both thresholds are independently configurable and can be active simultaneously.
- [ ] A user can log in via local password auth, receive a JWT, and use it for both `baobotctl` and the web UI without re-authenticating.
- [ ] OAuth2 (GitHub or Okta) can be configured as an alternative auth flow and used end-to-end in a test environment.
- [ ] External content (e.g. a GitHub issue body) is wrapped in untrusted-content delimiters before being passed to the model in all code paths.
- [ ] The harness screening layer detects and flags a known injection pattern in a tool output before it reaches the model.
- [ ] All bot activity, tool calls, and model invocations produce OTel traces visible in a connected observability backend.
- [ ] The written recommendation document addresses at minimum: (1) measured agent throughput and delivery accuracy from at least one completed project cycle, (2) actual infrastructure cost per task vs. estimated, (3) workflow friction points and their resolutions, and (4) a go/no-go recommendation with stated conditions for company adoption.

## Dependencies and Risks

| Item | Type | Notes |
|---|---|---|
| AWS (ECS, S3, SQS, SNS, RDS, DynamoDB, Bedrock, Secrets Manager, EventBridge) | Dependency | Core infrastructure; all services must be available in us-east-1 |
| S3 Vectors | Dependency | Semantic memory layer; confirm GA availability in us-east-1 before committing — adjust architecture if unavailable |
| GitHub (Actions, Releases, Repos) | Dependency | CI/CD pipelines and bot repo monitoring |
| Slack / Microsoft Teams | Dependency | Primary human-facing notification and communication channels |
| AWS Bedrock model availability | Dependency | Model IDs and regional quotas must be confirmed before deploy |
| GitHub OAuth2 / Okta | Dependency | Required for pluggable OAuth2 auth flow |
| React Native (future) | Dependency | Native client depends on this decision being validated for macOS and Windows targets |
| OTel observability backend | Dependency | CloudWatch (via ADOT) is the default; any OTLP-compatible backend is supported. Backend must be provisioned and endpoint configured before observability acceptance criteria can be verified. |
| Agent cost overrun before caps are tuned | Risk | Initial cap values will be estimates; early runs may be expensive before limits are calibrated |
| Prompt injection via repo content | Risk (mitigated) | Addressed by FR-020 (content segmentation) and FR-021 (harness screening layer); residual risk remains for novel injection patterns |
| Orchestrator single point of failure | Risk | One orchestrator per cluster; crash and restart window leaves team uncoordinated — mitigated by SQS visibility timeouts and idempotent handlers |
| LLM output quality for complex dev tasks | Risk | Agent code quality may be insufficient for non-trivial tasks without a human review gate |
| Company adoption recommendation scope creep | Risk | The research/recommendation deliverable could expand indefinitely — needs a defined endpoint scoped to this project's findings |

## Open Questions

- **S3 Vectors regional availability:** Confirm GA status in us-east-1 before architecture is finalised. If unavailable, an alternative vector store (e.g. OpenSearch Serverless) must be evaluated.
- **Bedrock Guardrails promotion criteria:** FR-021 defines the screening interface as pluggable but defers Bedrock Guardrails to a future provider. No trigger condition has been defined for when it becomes required.
- **ETA cold-start resolved:** Seed with a configurable reduction multiplier applied to human man-day estimates converted to minutes (`human_man_days × 480 × multiplier`). The multiplier reflects agent speed advantage (observed range: 60x–100x, implying seed multiplier ~0.01–0.02). Operator configures the seed; system replaces it with an observed ratio after 10 completed tasks of the same type. Historical factor: `actual_agent_minutes ÷ human_estimate_minutes`. Rate-limited wait time is excluded from all timing calculations (FR-024).
- **Observability backend resolved:** OTel export endpoint is operator-configured. CloudWatch via ADOT collector is the default. Any OTLP-compatible backend is supported — no backend is mandated by the system.
