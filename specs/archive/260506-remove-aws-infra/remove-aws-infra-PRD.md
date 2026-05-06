# PRD: Remove AWS Infrastructure — Local Single-Binary Runtime

**Created:** 2026-05-05
**Jira:** N/A
**Status:** Draft

## Problem Statement

The current `boabot` runtime is tightly coupled to six AWS services (SQS, SNS, S3, S3 Vectors, DynamoDB, Secrets Manager) and assumes deployment on ECS/Fargate with an ECR image. This creates four concrete barriers:

- **High operational friction.** A developer cannot run the system without an active AWS account, correct IAM roles, provisioned queues, tables, and buckets. The CDK stack must be deployed before a single bot can start.
- **Cost at idle.** AWS charges for every SQS poll, DynamoDB read, and S3 request even when the team is doing nothing.
- **Deployment complexity.** Each bot is a separately containerised process coordinated via external message queues. Adding a new bot requires a CDK stack update, ECR push, and ECS task definition change.
- **No local-first story.** There is no way to run the full multi-bot team on a laptop for development or evaluation without mocking every AWS adapter.

The desired end state is a single self-contained binary that runs the full bot team locally, with AWS providers remaining available as optional compiled-in adapters for teams that want them.

## Goals

- Remove AWS as a hard runtime dependency so the full bot team can be started with a single binary and no cloud account.
- Replace every AWS infrastructure adapter with a local equivalent that satisfies the same domain interface, so no domain or application code changes are required.
- Add the Anthropic Claude SDK as a first-class LLM provider alongside the existing Bedrock and OpenAI adapters.

## Non-Goals

- Removing Bedrock as a supported LLM provider — it remains a compiled-in option for teams already on AWS.
- Changing any domain interface (`MessageQueue`, `Broadcaster`, `MemoryStore`, `VectorStore`, `Embedder`, `BudgetTracker`, `ModelProvider`, etc.).
- Changing the agent runtime loop (`run_agent.go`, `execute_task.go`, `context_manager.go`).
- Changing the orchestrator HTTP server or Kanban board.
- Changing `boabotctl`'s core commands — only new `memory` subcommands are added.
- Supporting per-bot GitHub backup repositories — all bots share one repo.
- Creating a plugin system for providers — all adapters remain compiled-in.
- Calling the GitHub API to create the backup repository — the user creates it; `boabotctl memory init` prints setup instructions only.

## Functional Requirements

**FR-001:** On startup, `boabot` reads `team.yaml`, instantiates each enabled bot's configuration, wires up local adapters, and starts each bot as an in-process goroutine. No separate container images, ECS tasks, or external process manager is required.

**FR-002:** `cmd/boabot/main.go` instantiates `application/team.TeamManager` and blocks until OS signal (SIGINT/SIGTERM). All multi-bot wiring moves into `TeamManager`.

**FR-003:** `application/team.TeamManager` reads `team.yaml`, starts the orchestrator bot as the primary goroutine, and spawns each additional enabled bot in its own goroutine. Each bot has its own config, `SOUL.md`, tool set, and memory namespace.

**FR-004:** Each bot goroutine is wrapped in a `recover()` handler. A panicking bot is restarted with exponential back-off. `TeamManager.Shutdown()` sends `ShutdownMessage` to all bots and waits for their goroutines to exit before the process terminates.

**FR-005:** `application/team.BotRegistry` maintains a live map of running bot instances used by the local event bus to route messages.

**FR-006:** `infrastructure/local/queue` implements `domain.MessageQueue` using a buffered `chan ReceivedMessage` per bot. `Send` routes by bot name (replacing queue URL). Receipt handles are UUIDs; `Delete` is a no-op.

**FR-007:** `infrastructure/local/bus` implements `domain.Broadcaster` using a fan-out registry of subscriber channels. It is thread-safe via `sync.RWMutex`.

**FR-008:** `infrastructure/local/fs` implements `domain.MemoryStore` using `os.ReadFile`/`os.WriteFile`/`os.Remove` under `<memory.path>/<bot-name>/`. Directories are created on first write.

**FR-009:** `infrastructure/local/vector` implements `domain.VectorStore`. Embeddings are stored as binary files at `<memory.path>/<bot-name>/vectors/<key>.vec`. `Search` loads all vectors into memory, computes cosine similarity, and returns top-k results.

**FR-010:** `infrastructure/local/budget` implements `domain.BudgetTracker` using `sync/atomic` counters. Counters are flushed to `<memory.path>/<bot-name>/budget.json` on a configurable interval (default 10s) and loaded from that file on startup.

**FR-011:** `infrastructure/local/watchdog` runs as a goroutine within `TeamManager`. It samples `runtime.MemStats.HeapInuse` on a configurable interval (default 30s). At `memory.heap_warn_mb` it logs a warning and emits an internal metric. At `memory.heap_hard_mb` it calls `TeamManager.Shutdown()`. Both thresholds default to 0 (disabled).

**FR-012:** `infrastructure/anthropic` implements `domain.ModelProvider` using `github.com/anthropics/anthropic-sdk-go`. It maps `InvokeRequest` → Anthropic Messages API → `InvokeResponse`, streams by default, and exposes `StopReason` and `TokenUsage`. The API key is read from `ANTHROPIC_API_KEY` env var or `anthropic_api_key` in `~/.boabot/credentials`.

**FR-013:** The `domain.Embedder` adapter in local mode calls the configured provider's embedding endpoint when one is available (OpenAI `/v1/embeddings`, Anthropic embeddings). When no embedding endpoint is available, a `BM25Embedder` is used as fallback. The vector store is provider-agnostic.

**FR-014:** `memory.embedder` in `config.yaml` defaults to `bm25`. When set to a provider name, `TeamManager` validates at startup that the provider supports embeddings and fails with a clear error if it does not.

**FR-015:** `infrastructure/github/backup` implements `domain.MemoryBackup` using `github.com/go-git/go-git/v5`. `Backup()` runs `git add -A`, commits with an RFC3339 timestamp message, and pushes. If nothing changed since the last commit, the push is skipped. On remote divergence, it pulls with rebase (local wins on conflict) before pushing. Authentication uses a PAT read from `BOABOT_BACKUP_TOKEN` env var or credentials file.

**FR-016:** `application/backup.ScheduledBackupUseCase` wraps `domain.MemoryBackup` and is driven by the existing scheduler. Default schedule is every 30 minutes (configurable via cron expression). Failure is non-fatal — logged and retried on the next tick.

**FR-017:** When `backup.enabled: true` and `backup.restore_on_empty: true`, `boabot` attempts `Restore()` on startup if `memory.path` is absent or empty. A failed restore under this configuration is a fatal startup error.

**FR-018:** `boabotctl memory backup` triggers an immediate backup. `boabotctl memory restore` clones or pulls from the configured remote into `memory.path`. `boabotctl memory status` shows last backup time and pending change count.

**FR-019:** The `config.yaml` schema gains a `memory` section (`path`, `vector_index`, `embedder`, `heap_warn_mb`, `heap_hard_mb`) and a `backup` section (`enabled`, `schedule`, `restore_on_empty`, `github.*`). The `aws` section is removed from per-bot config. The `models.providers` list gains `type: anthropic` as a valid provider type.

**FR-020:** `~/.boabot/credentials` uses INI format with named profiles (same convention as AWS CLI). The active profile is selected via `BOABOT_PROFILE` env var (default: `default`). Environment variables override file values.

**FR-021:** The following packages are deleted: `infrastructure/aws/sqs`, `infrastructure/aws/sns`, `infrastructure/aws/s3`, `infrastructure/aws/s3vectors`, `infrastructure/aws/dynamodb`, `infrastructure/aws/secretsmanager`, `infrastructure/aws/secrets`. `infrastructure/aws/bedrock` is retained.

**FR-022:** `boabot-team/cdk/` is deleted entirely.

## Non-Functional Requirements

- **Performance:** Vector search must return results in < 100ms for up to 100k stored vectors (brute-force cosine). Model invocation latency is provider-bound and not constrained here.
- **Reliability:** A panicking bot is restarted with exponential back-off; the process must not exit on a single bot crash. Backup failures are non-fatal — logged and retried on the next scheduled tick.
- **Memory / Resource:** Heap watchdog thresholds are operator-configurable (`heap_warn_mb`, `heap_hard_mb`); defaults are 0 (disabled). No process-wide memory limit is enforced by default.
- **Security:** `~/.boabot/credentials` must be mode 0600; `boabot` refuses to start if the file is world-readable. API keys and tokens must never be written to `config.yaml` and must never appear in log output.
- **Observability:** Each backup attempt emits a structured log line (success/failure, files changed, duration). The heap watchdog emits a log warning at the soft threshold. All log output goes to stdout (existing behaviour).
- **Test Coverage:** ≥ 90% per module, enforced in CI. Deleted AWS adapter packages must have their tests deleted — no dead test coverage.

## Acceptance Criteria

- [ ] `boabot` starts with a single `go run ./cmd/boabot` or compiled binary and runs all enabled bots in-process — no AWS credentials required.
- [ ] All six AWS infrastructure adapters (`sqs`, `sns`, `s3`, `s3vectors`, `dynamodb`, `secretsmanager`) are replaced by local equivalents and the old packages are deleted.
- [ ] `boabot-team/cdk/` is deleted.
- [ ] Memory files are read/written under `<memory.path>/<bot-name>/` with no S3 calls.
- [ ] Bots communicate via in-process channels; no SQS or SNS calls are made at runtime.
- [ ] `infrastructure/anthropic` provider passes an end-to-end invocation test against the real Anthropic API (skippable in CI via env-var gate).
- [ ] `boabotctl memory backup`, `restore`, and `status` work against a configured GitHub repository.
- [ ] A crashed bot is restarted with exponential back-off without bringing down the process.
- [ ] Budget counters survive a clean restart (loaded from `budget.json` on startup).
- [ ] `go fmt`, `go vet`, `golangci-lint` all pass; test coverage stays ≥ 90% across all modules.

## Dependencies and Risks

| Item | Type | Notes |
|------|------|-------|
| `github.com/anthropics/anthropic-sdk-go` | Dependency | New Go module dependency for Anthropic provider |
| `github.com/go-git/go-git/v5` | Dependency | Pure-Go git implementation for GitHub backup; no `git` binary required |
| In-process bots share heap — one bad bot can OOM the process | Risk | Mitigated by heap watchdog + configurable limits + per-bot `BudgetTracker` |
| BM25 fallback produces lower-quality memory retrieval than semantic search | Risk | Document the trade-off clearly; semantic search requires explicit embedder config |
| Budget counters lost on crash before flush | Risk | Short flush interval (default 10s); write-ahead log pattern available if needed |
| Anthropic SDK API surface may change faster than Bedrock | Risk | Adapter isolated behind `ModelProvider` interface |
| Large teams (10+ bots) may hit goroutine scheduler pressure on small hardware | Risk | Benchmark before GA; model invocations are the real bottleneck, not goroutines |
| GitHub PAT leaked via config or logs | Risk | Token read exclusively from env var or credentials file; never from `config.yaml`; never logged |
| `go-git` push fails silently during network outage | Risk | Retry with exponential back-off; emit metric; non-fatal |
| Backup repo grows unboundedly via git history | Risk | Document `git gc` + shallow-clone restore; squash older history if needed |

## Open Questions

- None. All architecture decisions were resolved in `remove-aws-infra-cr.md` §6 before PRD creation.
