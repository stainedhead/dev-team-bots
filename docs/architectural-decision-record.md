# Architectural Decision Record — BaoBot Dev Team

Each entry records a significant decision: what was decided, why, and what was rejected.

---

## ADR-001 — Go 1.26 as implementation language

**Decision:** All modules are implemented in Go 1.26.

**Rationale:** Go provides strong concurrency primitives suited to the long-running, multi-threaded agent model. The compiled binary simplifies containerisation. The standard library and ecosystem cover all required infrastructure integrations. Go 1.26 is the current stable release.

**Rejected:** Python (runtime overhead, packaging complexity), Rust (steeper ramp for contributors), Node.js (weaker concurrency model for this pattern).

---

## ADR-002 — Shared binary, per-bot configuration

**Decision:** All bots run the same compiled binary and container image. Role differentiation is applied at runtime via injected configuration (SOUL.md, config.yaml, queue ARNs).

**Rationale:** Eliminates per-bot build pipelines. A single image simplifies patching and deployment. Configuration-driven differentiation keeps the delivery surface small.

**Rejected:** Per-bot container images (build complexity, maintenance overhead).

---

## ADR-003 — SQS per-bot queues + SNS broadcast for agent messaging

**Decision:** Each bot has a dedicated SQS inbound queue for direct messages. A shared SNS topic fans out to all bot queues for broadcasts.

**Rationale:** SQS is durable, AWS-native, and integrates directly with EventBridge. Queue ARNs injected at deploy time by CDK solve the bootstrap/discovery problem. Dead-letter queues provide resilience for failed message processing. SNS fan-out covers team-wide broadcasts without requiring bots to know each other's addresses.

**Rejected:** Direct HTTP/gRPC between bots (ECS tasks lack stable addresses), Google A2A spec (additional dependency, async model already covered by SQS).

---

## ADR-004 — S3 Vectors + S3 Files for agent memory

**Decision:** Each bot's memory is stored in its private S3 bucket using two native S3 mechanisms: S3 Vectors for semantic search/RAG, S3 Files for structured direct-access storage. The shared team bucket follows the same pattern.

**Rationale:** Keeps memory entirely within S3. Avoids the overhead of Bedrock Knowledge Bases or a separate vector database. S3 Vectors is a native AWS feature requiring no additional services.

**Rejected:** Bedrock Knowledge Bases (additional pipeline complexity), OpenSearch Serverless (separate service), pgvector on RDS (requires PostgreSQL, conflicts with MariaDB decision).

---

## ADR-005 — RDS MariaDB for control plane and Kanban board

**Decision:** Two separate RDS MariaDB instances: one for the orchestrator control plane registry, one for the Kanban board.

**Rationale:** Relational storage fits the structured, queryable nature of team registry and work item data. Separate instances isolate the two concerns. MariaDB is well-supported in the AWS ecosystem.

**Rejected:** DynamoDB (less suited to relational queries on board items), single shared instance (failure isolation, schema independence).

---

## ADR-006 — Orchestrator as sole database writer

**Decision:** All writes to the control plane and Kanban board databases are performed exclusively by the orchestrator. Other bots interact via SQS messages to the orchestrator.

**Rationale:** Single writer eliminates concurrency and access control complexity. All mutations are auditable through one actor. Database credentials are scoped to the orchestrator IAM role only; VPC security groups enforce the network boundary.

---

## ADR-007 — ALB replaces API Gateway

**Decision:** An Application Load Balancer fronts the orchestrator, routing `/api/*` to the REST API port and `/*` to the web UI port. API Gateway is not used.

**Rationale:** The orchestrator REST API is an internal team management tool, not a high-traffic public API. ALB provides TLS termination, health checks, and stable DNS without API Gateway complexity or cost. The orchestrator handles its own authentication.

**Rejected:** API Gateway (unnecessary for this access pattern, adds cost and configuration overhead).

---

## ADR-008 — JWT authentication with username/password

**Decision:** User authentication uses username and password. A signed JWT is issued on login, stored locally by baobotctl (or as a cookie by the web UI), and used for all subsequent requests. First login forces a password change.

**Rationale:** Single credential type works across both baobotctl and the web UI. Standard security practice. Stateless auth simplifies the orchestrator. Configurable expiry.

**Rejected:** API-token-only (no web UI login UX), per-surface auth (implementation overhead, two credential types to manage).

---

## ADR-009 — MCP client with shared and optional private configuration

**Decision:** All bots are MCP clients. Tool configuration is loaded from two optional sources: a shared `mcp.json` on the team S3 bucket (all bots, absence logged by orchestrator) and an optional private `mcp.json` on each bot's S3 bucket (extends shared, missing = silently ignored).

**Rationale:** MCP provides a standardised protocol for tool integration. The two-file pattern allows team-wide tools to be defined once while enabling role-specific tool access without affecting other bots.

---

## ADR-010 — Clean Architecture with interface-driven infrastructure

**Decision:** All modules enforce Clean Architecture. Domain interfaces define contracts; infrastructure adapters implement them. No infrastructure imports in domain or application layers.

**Rationale:** Enables unit testing at adapter seams without real external services. Keeps domain logic portable and infrastructure replaceable. Enforces a consistent contributor mental model across all three modules.

---

## ADR-011 — TDD as mandatory development practice

**Decision:** All production code must be written test-first (red-green-refactor). Coverage target is 90%+ for all modules. Enforced in CI.

**Rationale:** TDD produces better-designed, more testable code. The coverage gate prevents regression. Non-negotiable because the codebase is operated by AI agents as well as humans — high confidence in correctness is essential.

---

## ADR-012 — Three CI/CD workflows with path filters, all at repo root

**Decision:** Three GitHub Actions workflow files live at `.github/workflows/` in the repository root, each with `paths:` filters scoped to its module directory. There is no workflow file in any subdirectory.

- `boabot.yml` — test → lint → build → containerise → CDK deploy (shared stack)
- `boabotctl.yml` — test → lint → build; release binaries on tag `boabotctl/v*`
- `boabot-team.yml` — CDK test → CDK diff on PR → CDK deploy on main

**Rationale:** GitHub Actions only processes workflow files at the repository root `.github/workflows/`. Path filters provide the logical per-module separation. CDK diff posted as a PR comment gives reviewers infrastructure change visibility before merge.

**Rejected:** Workflow files in subdirectories (not supported by GitHub Actions).
