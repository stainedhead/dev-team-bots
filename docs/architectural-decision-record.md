# Architectural Decision Record — BaoBot Dev Team

Each entry records a significant decision: what was decided, why, and what was rejected.

---

## ADR-001 — Go 1.26 as implementation language

**Decision:** All modules are implemented in Go 1.26.

**Rationale:** Go provides strong concurrency primitives suited to the long-running, multi-threaded agent model. The compiled binary simplifies containerisation. The standard library and ecosystem cover all required infrastructure integrations. Go 1.26 is the current stable release with Green Tea GC enabled by default, reducing memory overhead for concurrent agent pipelines.

**Rejected:** Python (runtime overhead, packaging complexity), Rust (steeper ramp for contributors), Node.js (weaker concurrency model for this pattern).

---

## ADR-002 — Shared binary, per-bot configuration

**Decision:** All bots run the same compiled binary. Role differentiation is applied at runtime via injected configuration (SOUL.md, config.yaml, per-bot model and tool settings).

**Rationale:** Eliminates per-bot build pipelines. A single image simplifies patching and deployment. Configuration-driven differentiation keeps the delivery surface small.

**Rejected:** Per-bot container images (build complexity, maintenance overhead).

---

## ADR-003 — In-process queues + broadcaster for agent messaging; A2A-shaped envelope for structured delegation

**Decision:** Each bot has a dedicated in-process queue (`local/queue`) for direct messages. A shared in-process broadcaster (`local/bus`) fans out to all bot queues for team-wide broadcasts. Bot-to-bot task delegation uses a structured message envelope whose schema is modelled on the A2A task format, carrying a task lifecycle (`submitted → working → input-required → completed → failed`). The A2A HTTP transport protocol is not adopted.

**Rationale:** In-process queues require no infrastructure and have zero network latency, making them the right default for a local single-binary runtime. The A2A task schema provides structured lifecycle tracking without the operational complexity of running HTTP listeners. The envelope schema is intentionally A2A-compatible so the transport can be upgraded to SQS or HTTP later without changing the data model.

**Rejected:** Direct HTTP/gRPC between goroutines (unnecessary overhead for in-process communication); A2A HTTP transport (same reasons, plus SDK assumes HTTP which conflicts with the local in-process design); SQS/SNS (requires AWS account and infrastructure provisioning).

---

## ADR-004 — Local filesystem memory + GitHub git backup; BM25 + cosine similarity vector store for semantic retrieval

**Decision:** Each bot's structured memory is a local filesystem directory — agents interact with it via file tools. An optional scheduled GitHub git backup (`github/backup`) commits and pushes the memory directory to a configured GitHub repository on a cron schedule (default: every 30 minutes), providing durability across restarts. Semantic retrieval uses a local BM25 feature-hash embedder (`local/bm25`) and cosine similarity vector store (`local/vector`), with no external embedding API required.

**Rationale:** The MemFS pattern (Letta V1) gives agents a uniform interface — memory feels like files, no separate memory API. Local filesystem is the simplest possible backing store; no cloud account is required. GitHub git backup provides durable history and enables restore from remote without requiring always-on cloud storage. The local BM25 + vector store provides fast semantic search (40ms at 100k × 512-dim) with zero external dependencies — no API key, no network call.

**Rejected:** S3 object sync (requires AWS account); S3 Vectors (requires AWS account and S3 Vectors preview access); git-remote-s3 (fragile, poorly maintained); neural embedder as default (200–500 MB memory overhead, API key required, unavailable offline).

---

## ADR-005 — RDS MariaDB for control plane and Kanban board

**Decision:** Two separate RDS MariaDB instances: one for the orchestrator control plane registry, one for the Kanban board.

**Rationale:** Relational storage fits the structured, queryable nature of team registry and work item data. Separate instances isolate the two concerns. MariaDB is well-supported in the AWS ecosystem.

**Rejected:** DynamoDB (less suited to relational queries on board items), single shared instance (failure isolation, schema independence).

---

## ADR-006 — Orchestrator as sole writer to shared mutable state

**Decision:** All writes to the control plane DB and Kanban board DB are performed exclusively by the orchestrator. Other bots interact via in-process messages to the orchestrator. Per-bot memory is each bot's own responsibility — written directly to the bot's local filesystem.

**Rationale:** Single writer eliminates concurrency and access control complexity for shared state. All mutations to shared structures are auditable through one actor. Per-bot memory isolation removes the need for shared write coordination at the filesystem level.

**Rejected:** Shared memory mutex per bot (fragile, error-prone); convention-only file partitioning for shared state (not enforced, fragile under bugs or prompt injection).

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

**Decision:** All bots are MCP clients. Tool configuration is loaded from two optional local sources: a shared `mcp.json` in the team bots directory and an optional private `mcp.json` in the bot's own directory. Each server entry may include a typed `credential` field (`static_secret` for `~/.boabot/credentials` lookup, `oauth2` reserved for future implementation).

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

## ADR-012 — Two CI/CD workflows with path filters, all at repo root

**Decision:** Two GitHub Actions workflow files live at `.github/workflows/` in the repository root, each with `paths:` filters scoped to its module directory.

- `boabot.yml` — test → lint → build
- `boabotctl.yml` — test → lint → build; release binaries on tag `boabotctl/v*`

**Rationale:** GitHub Actions only processes workflow files at the repository root `.github/workflows/`. Path filters provide the logical per-module separation. There is no CDK infrastructure to deploy — the runtime is a local binary.

**Rejected:** Workflow files in subdirectories (not supported by GitHub Actions).

---

## ADR-013 — Tool Attention with BM25 scoring

**Decision:** The harness implements Tool Attention — a middleware layer that scores available tool descriptions against the current task intent and injects only top-k matching tools as full JSON schemas. Scoring uses BM25 (pure-Go, zero-dependency term-frequency ranking). Hard cap: 20 simultaneously injected full schemas.

**Rationale:** Naïve eager injection of all tool schemas in multi-MCP deployments consumes 10,000–60,000 tokens per turn. BM25 provides sufficient match quality for a closed, well-named tool set with high vocabulary overlap between tool descriptions and task prompts. The scorer is a single interface; it can be swapped for neural embeddings (e.g. Bedrock Titan Embeddings) if quality degrades as tool count grows.

**Rejected:** In-process neural embedding model (200–500 MB memory overhead, cgo complexity); Bedrock embeddings from day one (per-turn API latency and cost without evidence of quality gap requiring it).

---

## ADR-014 — Agent Skills: runtime upload with Admin approval gate

**Decision:** Agent Skills (SKILL.md + optional scripts) are uploaded at runtime via `baobotctl skills push` to a `skills/staging/` prefix in the bot's memory directory. An Admin must promote skills via `baobotctl skills approve` before they are discoverable by agents. Supporting scripts run in restricted subprocesses (stripped environment, temporary working directory).

**Rationale:** Runtime upload allows fast iteration without any deploy cycle. The mandatory Admin approval gate prevents unapproved scripts from becoming executable. Restricted subprocess execution limits the blast radius of buggy or malicious skill scripts.

**Rejected:** Skills as code in `boabot-team/` repo (slower iteration, requires a full redeploy cycle); unrestricted subprocess execution (bot credentials and filesystem accessible to skill scripts).

---

## ADR-015 — Budget caps in local JSON file with in-memory counters

**Decision:** Per-bot token spend (daily) and tool call counts (hourly) are enforced by the harness. Counters are maintained in memory and persisted to a local `budget.json` file on graceful shutdown. On startup the harness seeds from `budget.json` if present.

**Rationale:** In-memory counters keep enforcement off the hot path. Persisting to a local file on shutdown is sufficient for the local single-binary runtime — worst-case exposure after a crash is one session's worth of uninhibited calls within the cap windows. No external service or network call required.

**Rejected:** Per-call file writes (adds latency to every tool dispatch); DynamoDB (requires AWS account, adds network RTT to every flush); routing through orchestrator (puts orchestrator in the hot path for every agent action).

---

## ADR-016 — Agent Card discovery via in-process broadcast and local BotRegistry cache

**Decision:** Each bot registers with the in-process `BotRegistry` on startup. The orchestrator fetches the card and broadcasts it via the in-process bus (`local/bus`). All running bots receive and cache cards locally. The `BotRegistry` provides instant local lookup for delegation.

**Rationale:** In-process registration eliminates network latency. The `BotRegistry` serves delegation lookups at zero cost — no round-trip required. Broadcasting cards via the in-process bus reuses the existing broadcast channel at no infrastructure cost.

**Rejected:** HTTP endpoint per bot for card lookup (adds latency and listener complexity); external discovery service (unnecessary for a single-process runtime).

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
