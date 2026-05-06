# Implementation Plan: BaoBot Dev Team

**Feature:** baobot-dev-team
**Date:** 2026-05-05
**Status:** Planning

---

## Development Approach

- **TDD throughout:** Red → Green → Refactor for every task. No production code without a failing test.
- **Clean Architecture:** Domain layer first, then application use cases, then infrastructure adapters.
- **Parallel workstreams:** Use git worktrees and agent swarms for independent milestones.
- **Commit cadence:** Commit and push at the end of each stable milestone.
- **Coverage target:** ≥ 90% on domain and application layers at all times.

---

## Phase Breakdown

### M1 — Domain + Core (Priority: Critical)

Establish all domain entities, value objects, repository interfaces, and service interfaces. Write unit tests against all interfaces using mock implementations. This is the foundation — all other milestones depend on it.

**Workstream A:** WorkItem, WorkflowDefinition, Bot entities + repository interfaces
**Workstream B:** Cost, Budget, ETACalibration entities + enforcer/estimator interfaces
**Workstream C:** ContentScreener, AuthProvider, NotificationSender, MetricsStore interfaces

### M2 — Infrastructure Adapters (Priority: High)

Implement AWS adapters satisfying domain interfaces. Use localstack for integration testing. Verify no AWS SDK imports leak into domain or application code.

**Workstream A:** S3, SQS, SNS adapters
**Workstream B:** Bedrock, DynamoDB, Secrets Manager adapters
**Workstream C:** PostgreSQL adapter (work items, workflow state, metrics)
**Workstream D:** Local password auth + JWT; regex content screener; OTel provider

### M3 — Workflow Engine (Priority: High)

Implement configurable workflow routing, work item lifecycle state machine, triage bot logic, and scheduler.

**Workstream A:** Workflow router use cases + step advancement
**Workstream B:** Scheduler (cron runner, ScheduledTask execution, repo monitor)
**Workstream C:** Triage bot routing logic

### M4 — Cost + Safety (Priority: High)

Implement cost enforcement, daily cost review, content screening, and rate-limit handling.

**Workstream A:** CostEnforcer use cases (per-bot + system-wide caps, spike alert, flat cap alert)
**Workstream B:** ContentScreener use cases (external content wrapping + regex screening)
**Workstream C:** Rate-limit handling in harness (pause, notify, ETA update)

### M5 — Observability + Monitoring (Priority: Medium)

OTel instrumentation, Kanban board, ETA estimator, viability reporting.

**Workstream A:** OTel instrumentation across all layers
**Workstream B:** Kanban board web UI (Go-served, HTMX or minimal JS)
**Workstream C:** ETA estimator + calibration; bottleneck detection + rebalancing engine

### M6 — CDK + Deploy (Priority: Medium)

Full CDK stack, IAM policies, CI/CD GitHub Actions workflows, end-to-end acceptance tests.

**Workstream A:** Shared CDK stack (RDS, DynamoDB, SNS, S3, shared IAM policy)
**Workstream B:** Per-bot CDK stack (ECS task def, IAM role, SQS, private S3)
**Workstream C:** GitHub Actions workflows (boabot.yml, boabotctl.yml, boabot-team.yml)

### M7 — Auth + CLI (Priority: Medium)

OAuth2 pluggable provider, `baobotctl` full command set.

**Workstream A:** OAuth2/OIDC adapter (GitHub, Okta)
**Workstream B:** `baobotctl` CLI commands (work, workflow, status, cost, auth)

### M8 — Recommendation Report (Priority: Low)

Written viability recommendation document.

---

## Critical Path

M1 (Domain) → M2 (Infrastructure) → M3 (Workflow) → M4 (Cost+Safety) → M5 (Observability) → M6 (CDK) → M7 (Auth+CLI) → M8 (Report)

M5 and M6 can partially overlap. M7 can start after M2-D (auth infrastructure) completes.

---

## Testing Strategy

- **Unit tests:** All domain entities and use cases tested with mock implementations. No external service calls.
- **Integration tests:** Infrastructure adapters tested against localstack (SQS, S3, SNS, DynamoDB, Secrets Manager) and a test PostgreSQL instance.
- **End-to-end tests:** Full workflow execution against deployed stack (or localstack where feasible).
- **Coverage gate:** `go tool cover -func=coverage.out` must show ≥ 90% on `internal/domain/` and `internal/application/` packages.

---

## Rollout Strategy

1. Deploy shared CDK stack to AWS account.
2. Deploy orchestrator ECS service.
3. Deploy worker bot services one at a time, starting with the triage bot.
4. Run first scheduled task (repo monitor) and verify triage output.
5. Submit first ad-hoc work item via `baobotctl` and verify end-to-end workflow.
6. Enable cost caps and verify enforcement.
7. Connect OTel backend and verify trace visibility.

---

## Success Metrics

- All acceptance criteria in spec.md pass.
- ≥ 90% test coverage on domain and application layers.
- `go fmt`, `go vet`, `golangci-lint` pass with zero warnings.
- No AWS SDK import in domain or application packages.
- Full CDK deploy with no manual console steps.
