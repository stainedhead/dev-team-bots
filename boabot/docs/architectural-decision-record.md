# Architectural Decision Record — boabot

Module-specific decisions. For system-level decisions see root [`docs/architectural-decision-record.md`](../../docs/architectural-decision-record.md).

---

## ADR-B001 — Worker goroutines recover from panics

**Decision:** Each worker goroutine wraps its execution in a `recover()`. A panicking worker logs the error and exits cleanly without propagating to the main thread.

**Rationale:** Worker tasks are agentic and unpredictable. A single bad task must not crash the agent. The main thread and other workers continue unaffected.

---

## ADR-B002 — Config loaded from filesystem, credentials from INI file and environment variables

**Decision:** Non-secret configuration is loaded from `config.yaml` next to the binary. Credentials (API keys, backup tokens) are loaded at startup from `~/.boabot/credentials` (INI format, profile selected by `BOABOT_PROFILE` env var) and from environment variables — never from `config.yaml`. World-readable credential files are rejected with an error.

**Rationale:** Keeps secrets out of config files and git. The credentials file follows the same pattern as AWS CLI and other developer tools, making it familiar and easy to manage on a local machine without requiring any cloud infrastructure. Environment variables remain a valid override for CI/CD and container environments.

---

## ADR-B003 — Orchestrator mode is additive, not a separate binary

**Decision:** Orchestrator features (control plane, Kanban board, REST API, web UI, shared memory write serialisation) are activated by a config flag in the standard bot binary — not a separate binary or container image.

**Rationale:** Maintains a single delivery artefact. The orchestrator is operationally a bot with extra responsibilities, not a fundamentally different system. The config flag gates all orchestrator code paths cleanly.

---

## ADR-B004 — MCP config merged from shared and private sources

**Decision:** MCP configuration is loaded from two optional S3 locations and merged at startup. Private config extends (not replaces) shared config. Missing files are not errors.

**Rationale:** Allows team-wide tools to be defined once while enabling role-specific tools without coordination overhead. Missing files are not errors — the system operates on whatever is present.

---

## ADR-B005 — Tool Attention as harness middleware, not model instruction

**Decision:** Tool schema injection is controlled by the harness via BM25 scoring, not by instructing the model to ignore certain tools. The model only sees tools that the harness has chosen to inject.

**Rationale:** Model-side filtering is unreliable and still consumes context tokens. Harness-side gating is enforced regardless of the model's behaviour. This is also a security boundary — a prompt-injected instruction cannot make the model invoke a tool that is not injected.

---

## ADR-B006 — Budget caps enforced before tool dispatch, not after

**Decision:** The harness checks budget caps before dispatching any tool call or model invocation. Requests that would exceed the cap are rejected before execution.

**Rationale:** Post-execution enforcement is meaningless — the tokens and tool calls have already been consumed. Pre-execution enforcement is the only effective gate. The DynamoDB flush (30s interval) means the counter may be slightly stale after a crash, which is acceptable given the cap windows.

---

## ADR-B007 — Skill scripts run as restricted subprocesses, not plugins

**Decision:** Agent Skill scripts are executed via `exec` with a stripped environment (no inherited env vars), filesystem access limited to a temporary working directory, and network access constrained by the ECS task's security group. No plugin API or SDK.

**Rationale:** Skills are operator-approved scripts, not trusted code. Restricting the subprocess environment limits the blast radius of a buggy or malicious skill without requiring OS-level sandboxing infrastructure (gVisor, Firecracker). The ECS security group already limits network egress — the subprocess inherits this boundary implicitly.

**Rejected:** Full OS-level sandboxing (unnecessary given the Admin approval gate and existing network controls); plugin API/SDK (over-engineered, skills are simple scripts).

---

## ADR-B008 — Local in-process adapters replace AWS services

**Decision:** The agent runtime uses local in-process adapters for all messaging and storage: `local/queue` (per-bot in-process queues) instead of SQS, `local/bus` (in-process broadcaster) instead of SNS, `local/fs` (local filesystem) instead of S3, and `local/budget` (local JSON file) instead of DynamoDB. AWS infrastructure is not required to run boabot.

**Rationale:** Zero-infrastructure developer experience — anyone can run the full team on a laptop without an AWS account. Local adapters are faster (no network RTT), simpler to debug (no cloud console), and eliminate operational cost for small self-hosted deployments. The domain interface layer (`domain.MessageQueue`, `domain.Broadcaster`, `domain.MemoryStore`, `domain.BudgetTracker`) is unchanged, so cloud-backed adapters can be introduced in future without touching application or domain code.

**Rejected:** Keeping SQS/SNS/DynamoDB/S3 as the only option (requires AWS account and infrastructure provisioning just to run; local development experience is poor); LocalStack (adds Docker dependency and partial AWS API emulation — the full domain interface approach is cleaner).

---

## ADR-B009 — BM25 feature-hashing as default embedder

**Decision:** The default semantic embedder is a BM25-style feature hasher using FNV-1a hashing into a fixed 512-dimensional float32 vector, L2-normalised. No external API or network call is required. Combined with a flat cosine similarity vector store (`local/vector`), search over 100k × 512-dim vectors completes in ~40ms on commodity hardware.

**Rationale:** No API key or network call needed — the embedder is self-contained in the process. FNV-1a hashing is deterministic and fast. The O(n) flat search is sufficient for memory stores up to ~100k documents before latency becomes a concern. The `domain.Embedder` interface is swappable: operators can replace BM25 with an OpenAI or other neural embedder by setting `memory.embedder` in config, with no application-layer changes required.

**Rejected:** Neural embedding model in-process (200–500 MB memory overhead, cgo complexity, GPU dependency); OpenAI embeddings as the default (requires API key, adds per-write latency and cost, unavailable offline); HNSW approximate nearest neighbour (complexity without evidence of need at current scale).
