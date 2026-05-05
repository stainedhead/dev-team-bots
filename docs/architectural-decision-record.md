# Architectural Decision Record — BaoBot Dev Team

Each entry records a significant decision: what was decided, why, and what was rejected.

---

## ADR-001 — Go 1.26 as implementation language

**Decision:** All modules are implemented in Go 1.26.

**Rationale:** Go provides strong concurrency primitives suited to the long-running, multi-threaded agent model. The compiled binary simplifies containerisation. The standard library and ecosystem cover all required infrastructure integrations. Go 1.26 is the current stable release with Green Tea GC enabled by default, reducing memory overhead for concurrent agent pipelines.

**Rejected:** Python (runtime overhead, packaging complexity), Rust (steeper ramp for contributors), Node.js (weaker concurrency model for this pattern).

---

## ADR-002 — Shared binary, per-bot configuration

**Decision:** All bots run the same compiled binary and container image. Role differentiation is applied at runtime via injected configuration (SOUL.md, config.yaml, queue ARNs).

**Rationale:** Eliminates per-bot build pipelines. A single image simplifies patching and deployment. Configuration-driven differentiation keeps the delivery surface small.

**Rejected:** Per-bot container images (build complexity, maintenance overhead).

---

## ADR-003 — SQS per-bot queues + SNS broadcast for agent messaging; A2A-shaped envelope for structured delegation

**Decision:** Each bot has a dedicated SQS inbound queue for direct messages. A shared SNS topic fans out to all bot queues for broadcasts. Bot-to-bot task delegation uses a structured SQS message envelope whose schema is modelled on the A2A task format, carrying a task lifecycle (`submitted → working → input-required → completed → failed`). The A2A HTTP transport protocol is not adopted.

**Rationale:** SQS is durable, AWS-native, and integrates directly with EventBridge. The A2A task schema provides structured lifecycle tracking without the operational complexity of running HTTP listeners on every ECS task (ephemeral IPs, extra ports, audit gaps). The envelope schema is intentionally A2A-compatible so the transport can be upgraded later without changing the data model. SNS Agent Card distribution (see ADR-016) piggybacks on the existing broadcast channel.

**Rejected:** Direct HTTP/gRPC between bots (ECS tasks lack stable addresses, adds listener complexity); A2A HTTP transport (same reasons, plus SDK assumes HTTP which conflicts with SQS-native design).

---

## ADR-004 — Git-backed local memory + S3 object sync; S3 Vectors for semantic retrieval

**Decision:** Each bot's structured memory is a local git repository — agents interact with it via file tools. The harness syncs changed objects to S3 individually using ETag comparison (not git remote protocol). S3 object versioning provides remote revision history. S3 Vectors provides semantic retrieval via the `memory_search` harness tool.

**Rationale:** The MemFS pattern (Letta V1) gives agents a uniform interface — memory feels like files, no separate memory API. Git provides local history, diffs, and conflict detection. S3 object sync is simpler and more reliable than git-over-S3 transports (git-remote-s3, tarball repack). S3 Vectors handles semantic search without a separate vector database.

**Rejected:** git-remote-s3 (fragile, poorly maintained); tarball repack on S3 (full repack on every sync, last-write-wins race); AWS CodeCommit (in maintenance mode); Bedrock Knowledge Bases (pipeline complexity); OpenSearch Serverless (separate service).

---

## ADR-005 — RDS MariaDB for control plane and Kanban board

**Decision:** Two separate RDS MariaDB instances: one for the orchestrator control plane registry, one for the Kanban board.

**Rationale:** Relational storage fits the structured, queryable nature of team registry and work item data. Separate instances isolate the two concerns. MariaDB is well-supported in the AWS ecosystem.

**Rejected:** DynamoDB (less suited to relational queries on board items), single shared instance (failure isolation, schema independence).

---

## ADR-006 — Orchestrator as sole writer to shared mutable state

**Decision:** All writes to the control plane DB, Kanban board DB, and shared team memory bucket are performed exclusively by the orchestrator. Other bots interact via SQS messages to the orchestrator.

**Rationale:** Single writer eliminates concurrency and access control complexity. All mutations are auditable through one actor. Database credentials and shared memory write permissions are scoped to the orchestrator IAM role only. Shared memory writes via SQS (fire-and-forget) keep the cost low and are consistent with the existing message-passing model.

**Rejected:** S3 conditional writes (optimistic concurrency — correct but adds per-write retry complexity); convention-only file partitioning (not enforced, fragile under bugs or prompt injection).

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

## ADR-009 — MCP client with shared and optional private configuration; typed credential field

**Decision:** All bots are MCP clients. Tool configuration is loaded from two optional sources: a shared `mcp.json` on the team S3 bucket and an optional private `mcp.json` on each bot's S3 bucket. Each server entry may include a typed `credential` field (`static_secret` for Secrets Manager lookup, `oauth2` reserved for future implementation).

**Rationale:** MCP provides a standardised protocol for tool integration. The two-file pattern allows team-wide tools to be defined once while enabling role-specific tool access. The typed credential field supports the 2026 MCP OAuth 2.1 spec while keeping the initial implementation simple (static API keys cover the majority of real servers). The union type design means adding OAuth 2.1 support is a new credential provider with no schema changes.

**Rejected:** Hardcoded credential injection (inflexible, couples auth to config format); full OAuth 2.1 from day one (complexity without a real server requiring it yet).

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

**Decision:** Three GitHub Actions workflow files live at `.github/workflows/` in the repository root, each with `paths:` filters scoped to its module directory.

- `boabot.yml` — test → lint → build → containerise → CDK deploy (shared stack)
- `boabotctl.yml` — test → lint → build; release binaries on tag `boabotctl/v*`
- `boabot-team.yml` — CDK test → CDK diff on PR → CDK deploy on main

**Rationale:** GitHub Actions only processes workflow files at the repository root `.github/workflows/`. Path filters provide the logical per-module separation. CDK diff posted as a PR comment gives reviewers infrastructure change visibility before merge.

**Rejected:** Workflow files in subdirectories (not supported by GitHub Actions).

---

## ADR-013 — Tool Attention with BM25 scoring

**Decision:** The harness implements Tool Attention — a middleware layer that scores available tool descriptions against the current task intent and injects only top-k matching tools as full JSON schemas. Scoring uses BM25 (pure-Go, zero-dependency term-frequency ranking). Hard cap: 20 simultaneously injected full schemas.

**Rationale:** Naïve eager injection of all tool schemas in multi-MCP deployments consumes 10,000–60,000 tokens per turn. BM25 provides sufficient match quality for a closed, well-named tool set with high vocabulary overlap between tool descriptions and task prompts. The scorer is a single interface; it can be swapped for neural embeddings (e.g. Bedrock Titan Embeddings) if quality degrades as tool count grows.

**Rejected:** In-process neural embedding model (200–500 MB memory overhead, cgo complexity); Bedrock embeddings from day one (per-turn API latency and cost without evidence of quality gap requiring it).

---

## ADR-014 — Agent Skills: runtime upload with Admin approval gate

**Decision:** Agent Skills (SKILL.md + optional scripts) are uploaded at runtime via `baobotctl skills push` to a `skills/staging/` prefix in S3. An Admin must promote skills via `baobotctl skills approve` before they are discoverable by agents. Supporting scripts run in restricted subprocesses (stripped environment, temporary working directory, security-group-constrained network).

**Rationale:** Runtime upload allows fast iteration without a full CDK deploy cycle. The mandatory Admin approval gate prevents unapproved scripts from becoming executable. Restricted subprocess execution limits the blast radius of buggy or malicious skill scripts without requiring full OS-level sandboxing (the ECS task security group already limits network egress).

**Rejected:** Skills as code in `boabot-team/` repo (slower iteration, full deploy cycle required); unrestricted subprocess execution (bot credentials and filesystem accessible to skill scripts).

---

## ADR-015 — Budget caps in DynamoDB with periodic flush

**Decision:** Per-bot token spend (daily) and tool call counts (hourly) are enforced by the harness. Counters are maintained in memory and flushed to a shared DynamoDB table every 30 seconds and on graceful shutdown. On startup the harness seeds from DynamoDB.

**Rationale:** In-memory counters keep enforcement off the hot path. A 30-second flush interval is an acceptable error margin for daily token budgets and hourly tool call caps — worst-case exposure is one flush interval of uninhibited calls after a crash. DynamoDB is the right store for time-windowed per-bot counters; it requires no orchestrator involvement and scales naturally.

**Rejected:** Per-call DynamoDB writes (adds latency to every tool dispatch); routing through orchestrator (puts orchestrator in the hot path for every agent action).

---

## ADR-016 — Agent Card discovery via SNS broadcast and local cache

**Decision:** Each bot publishes a signed Agent Card to a well-known S3 path. At registration the orchestrator fetches the card and includes it in the registration acknowledgement broadcast to the SNS topic. All running bots receive and cache cards locally. On startup each bot requests a `team_snapshot` from the orchestrator to pre-populate its cache.

**Rationale:** The registration broadcast already exists. Piggybacking Agent Cards on it adds no new infrastructure and keeps delegation lookup latency at zero (served from local cache). The `team_snapshot` request on startup closes the cold-start gap before the first incremental broadcast arrives.

**Rejected:** Orchestrator REST API for card lookup (adds latency and orchestrator dependency per delegation); S3 direct read per delegation (per-delegation API call, no caching).

---

## ADR-017 — Context management: checkpoint-and-restart

**Decision:** When a worker thread's context window approaches a configurable token threshold, the harness checkpoints all durable state (todo list, memory writes, structured task state) to the git-backed memory store, then starts a fresh worker thread reinitialised from the checkpoint.

**Rationale:** Provider-agnostic — works identically across Bedrock, OpenAI-compatible, and Anthropic API providers. Preserves precision: the agent reinjects from exact checkpointed state, not a lossy summary. The git-backed memory store already exists for this purpose.

**Rejected:** Server-side compaction (Anthropic API only, not available on Bedrock or OpenAI-compatible providers); sliding window (permanent information loss); summarise-then-compress (summary quality varies, detail loss).

---

## ADR-018 — Orchestrator restart durability: idempotent handlers + SQS visibility timeout

**Decision:** All orchestrator message handlers are idempotent. SQS visibility timeouts provide re-delivery if the orchestrator crashes before acknowledging a message. Kanban mutations include a client-supplied UUID idempotency token. No outbox pattern.

**Rationale:** At-least-once delivery with idempotent handlers is the right trade-off: SQS already provides re-delivery; idempotency is a correctness property worth having regardless. The outbox pattern (exactly-once) adds a DB write and delete to every handler for a precision improvement that is not needed — duplicate processing of idempotent handlers is harmless.

**Rejected:** Outbox pattern (complexity, per-handler DB overhead, exactly-once semantics not required).

---

## ADR-019 — MCP server credential typed union in mcp.json

**Decision:** Each MCP server entry in `mcp.json` may include a `credential` field typed as `static_secret` (Secrets Manager ARN lookup) or `oauth2` (reserved). The field is a union type; adding OAuth 2.1 support is a new credential provider with no schema changes.

**Rationale:** Most real MCP servers use static API keys or are unauthenticated. The typed union provides a clear upgrade path to OAuth 2.1 (required by the 2026 MCP spec for compliant servers) without over-engineering the initial implementation.

**Rejected:** OAuth 2.1 from day one (complexity without a concrete server requiring it); hardcoded credential injection (inflexible).
