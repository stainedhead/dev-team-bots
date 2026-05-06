# Feature Spec: Remove AWS Infrastructure — Local Single-Binary Runtime

**Spec dir:** specs/260506-remove-aws-infra/
**Source PRD:** specs/260506-remove-aws-infra/remove-aws-infra-PRD.md
**Created:** 2026-05-06
**Status:** Draft

---

## Executive Summary

Replace all six AWS runtime dependencies (SQS, SNS, S3, S3 Vectors, DynamoDB, Secrets Manager) with local in-process equivalents, allowing the full BaoBot team to run from a single binary with no cloud account required. Simultaneously add the Anthropic Claude SDK as a first-class LLM provider. AWS Bedrock is retained as a compiled-in optional provider. A GitHub-backed memory backup adapter provides the durability previously provided by S3.

---

## Problem Statement

The current `boabot` runtime is tightly coupled to six AWS services and assumes ECS/Fargate deployment with ECR images. This creates:

- **High operational friction** — developers cannot run the system without AWS credentials, IAM roles, provisioned queues, tables, and buckets.
- **Cost at idle** — AWS charges for every SQS poll, DynamoDB read, and S3 request even when no bots are doing work.
- **Deployment complexity** — each new bot requires a CDK stack update, ECR push, and ECS task definition change.
- **No local-first story** — no way to run the full multi-bot team on a laptop without mocking every AWS adapter.

---

## Goals

1. Remove AWS as a hard runtime dependency so the full bot team can be started with a single binary and no cloud account.
2. Replace every AWS infrastructure adapter with a local equivalent that satisfies the same domain interface — no domain or application code changes required.
3. Add the Anthropic Claude SDK as a first-class LLM provider alongside Bedrock and OpenAI.

## Non-Goals

- Removing Bedrock as a supported LLM provider — it remains compiled-in.
- Changing any domain interface (`MessageQueue`, `Broadcaster`, `MemoryStore`, `VectorStore`, `Embedder`, `BudgetTracker`, `ModelProvider`, etc.).
- Changing the agent runtime loop (`run_agent.go`, `execute_task.go`, `context_manager.go`).
- Changing the orchestrator HTTP server or Kanban board.
- Changing `boabotctl`'s core commands — only new `memory` subcommands are added.
- Supporting per-bot GitHub backup repositories.
- Creating a plugin system for providers — all adapters remain compiled-in.
- Calling the GitHub API to create the backup repository.

---

## Functional Requirements

**FR-001:** On startup, `boabot` reads `team.yaml`, instantiates each enabled bot's configuration, wires up local adapters, and starts each bot as an in-process goroutine. No container images, ECS tasks, or external process manager required.

**FR-002:** `cmd/boabot/main.go` instantiates `application/team.TeamManager` and blocks until OS signal (SIGINT/SIGTERM). All multi-bot wiring moves into `TeamManager`.

**FR-003:** `application/team.TeamManager` reads `team.yaml`, starts the orchestrator bot as the primary goroutine, and spawns each additional enabled bot in its own goroutine. Each bot has its own config, `SOUL.md`, tool set, and memory namespace.

**FR-004:** Each bot goroutine is wrapped in a `recover()` handler. A panicking bot is restarted with exponential back-off. `TeamManager.Shutdown()` sends `ShutdownMessage` to all bots and waits for goroutines to exit before the process terminates.

**FR-005:** `application/team.BotRegistry` maintains a live map of running bot instances used by the local event bus to route messages.

**FR-006:** `infrastructure/local/queue` implements `domain.MessageQueue` using a buffered `chan ReceivedMessage` per bot. `Send` routes by bot name. Receipt handles are UUIDs; `Delete` is a no-op.

**FR-007:** `infrastructure/local/bus` implements `domain.Broadcaster` using a fan-out registry of subscriber channels. Thread-safe via `sync.RWMutex`.

**FR-008:** `infrastructure/local/fs` implements `domain.MemoryStore` using `os.ReadFile`/`os.WriteFile`/`os.Remove` under `<memory.path>/<bot-name>/`. Directories are created on first write.

**FR-009:** `infrastructure/local/vector` implements `domain.VectorStore`. Embeddings stored as binary files at `<memory.path>/<bot-name>/vectors/<key>.vec`. `Search` loads all vectors, computes cosine similarity, returns top-k results.

**FR-010:** `infrastructure/local/budget` implements `domain.BudgetTracker` using `sync/atomic` counters. Flushed to `<memory.path>/<bot-name>/budget.json` on configurable interval (default 10s); loaded on startup.

**FR-011:** `infrastructure/local/watchdog` runs as a goroutine within `TeamManager`. Samples `runtime.MemStats.HeapInuse` on configurable interval (default 30s). Logs warning at `memory.heap_warn_mb`; calls `TeamManager.Shutdown()` at `memory.heap_hard_mb`. Both default to 0 (disabled).

**FR-012:** `infrastructure/anthropic` implements `domain.ModelProvider` using `github.com/anthropics/anthropic-sdk-go`. Maps `InvokeRequest` → Anthropic Messages API → `InvokeResponse`. Streams by default. API key from `ANTHROPIC_API_KEY` env var or `~/.boabot/credentials`.

**FR-013:** The `domain.Embedder` adapter calls the configured provider's embedding endpoint when available (OpenAI `/v1/embeddings`, Anthropic embeddings). Falls back to `BM25Embedder` when no embedding endpoint is available.

**FR-014:** `memory.embedder` defaults to `bm25`. When set to a provider name, `TeamManager` validates at startup that the provider supports embeddings; fails with a clear error if not.

**FR-015:** `infrastructure/github/backup` implements `domain.MemoryBackup` using `github.com/go-git/go-git/v5`. `Backup()` runs `git add -A`, commits with RFC3339 timestamp, pushes. Skips push if nothing changed. Pulls with rebase on remote divergence (local wins). Auth via PAT from `BOABOT_BACKUP_TOKEN` env var or credentials file.

**FR-016:** `application/backup.ScheduledBackupUseCase` wraps `domain.MemoryBackup` and is driven by the existing scheduler. Default schedule: every 30 minutes. Failure is non-fatal — logged and retried on next tick.

**FR-017:** When `backup.enabled: true` and `backup.restore_on_empty: true`, `boabot` attempts `Restore()` on startup if `memory.path` is absent or empty. Failed restore under this config is a fatal startup error.

**FR-018:** `boabotctl memory backup` triggers an immediate backup. `boabotctl memory restore` clones/pulls from the configured remote. `boabotctl memory status` shows last backup time and pending change count.

**FR-019:** `config.yaml` gains `memory` section (`path`, `vector_index`, `embedder`, `heap_warn_mb`, `heap_hard_mb`) and `backup` section (`enabled`, `schedule`, `restore_on_empty`, `github.*`). The `aws` section is removed. `models.providers` gains `type: anthropic`.

**FR-020:** `~/.boabot/credentials` uses INI format with named profiles. Active profile via `BOABOT_PROFILE` env var (default: `default`). Env vars override file values.

**FR-021:** Deleted packages: `infrastructure/aws/sqs`, `infrastructure/aws/sns`, `infrastructure/aws/s3`, `infrastructure/aws/s3vectors`, `infrastructure/aws/dynamodb`, `infrastructure/aws/secretsmanager`, `infrastructure/aws/secrets`. `infrastructure/aws/bedrock` retained.

**FR-022:** `boabot-team/cdk/` is deleted entirely.

---

## Non-Functional Requirements

| Category | Requirement |
|---|---|
| Performance | Vector search < 100ms for up to 100k vectors (brute-force cosine) |
| Reliability | Panicking bot restarted with exponential back-off; process must not exit on single bot crash |
| Reliability | Backup failures non-fatal — logged and retried on next tick |
| Memory/Resource | Heap watchdog thresholds configurable; defaults 0 (disabled) |
| Security | `~/.boabot/credentials` must be mode 0600; boabot refuses to start if world-readable |
| Security | API keys/tokens never written to `config.yaml`; never logged |
| Observability | Each backup emits structured log line (success/failure, files changed, duration) |
| Observability | Heap watchdog emits log warning at soft threshold |
| Test Coverage | ≥ 90% per module; deleted AWS packages must have tests deleted too |

---

## System Architecture

### Affected Layers

| Layer | Change |
|---|---|
| `cmd/boabot/` | Wiring replaced: single-bot → `TeamManager` |
| `application/team/` | **New** — `TeamManager`, `BotRegistry` |
| `application/backup/` | **New** — `ScheduledBackupUseCase` |
| `infrastructure/local/` | **New** — `queue`, `bus`, `fs`, `vector`, `budget`, `watchdog` |
| `infrastructure/anthropic/` | **New** — Anthropic SDK model provider |
| `infrastructure/github/backup/` | **New** — `go-git`-backed `MemoryBackup` |
| `infrastructure/aws/` | **Delete** sqs, sns, s3, s3vectors, dynamodb, secretsmanager, secrets; **retain** bedrock |
| `boabot-team/cdk/` | **Delete** entirely |
| `boabotctl/` | **Add** `memory` subcommands (backup, restore, status) |
| `config.yaml` schema | **Modify** — add `memory`, `backup`; remove `aws`; add `anthropic` provider type |
| `domain/` | **Add** `MemoryBackup` interface; no other changes |

### New Components

- `application/team.TeamManager` — multi-bot lifecycle orchestration
- `application/team.BotRegistry` — running bot instance registry
- `application/backup.ScheduledBackupUseCase` — scheduled backup use case
- `infrastructure/local/queue.LocalQueue` — buffered channel message queue
- `infrastructure/local/bus.LocalBus` — fan-out in-process broadcaster
- `infrastructure/local/fs.LocalMemoryStore` — filesystem memory store
- `infrastructure/local/vector.LocalVectorStore` — cosine similarity vector store
- `infrastructure/local/budget.LocalBudgetTracker` — atomic counter budget tracker
- `infrastructure/local/watchdog.HeapWatchdog` — process-wide heap monitor
- `infrastructure/anthropic.AnthropicProvider` — Anthropic SDK model provider
- `infrastructure/github/backup.GitHubBackup` — go-git-backed memory backup

---

## Scope of Changes

### New Files (boabot module)

```
internal/domain/memory_backup.go           # MemoryBackup interface + BackupStatus
internal/application/team/team_manager.go
internal/application/team/bot_registry.go
internal/application/backup/scheduled_backup.go
internal/infrastructure/local/queue/queue.go
internal/infrastructure/local/bus/bus.go
internal/infrastructure/local/fs/fs.go
internal/infrastructure/local/vector/vector.go
internal/infrastructure/local/budget/budget.go
internal/infrastructure/local/watchdog/watchdog.go
internal/infrastructure/anthropic/provider.go
internal/infrastructure/github/backup/backup.go
```

### Modified Files (boabot module)

```
cmd/boabot/main.go                         # Rewire to TeamManager
internal/domain/config.go                  # Add MemoryConfig, BackupConfig, AnthropicProviderType
```

### Deleted Files (boabot module)

```
internal/infrastructure/aws/sqs/
internal/infrastructure/aws/sns/
internal/infrastructure/aws/s3/
internal/infrastructure/aws/s3vectors/
internal/infrastructure/aws/dynamodb/
internal/infrastructure/aws/secretsmanager/
internal/infrastructure/aws/secrets/
```

### New Files (boabotctl module)

```
cmd/boabotctl/memory.go                    # memory subcommands
```

### Deleted Files (boabot-team module)

```
cdk/                                       # entire CDK stack
```

### New Dependencies

| Module | Package | Purpose |
|---|---|---|
| boabot | `github.com/anthropics/anthropic-sdk-go` | Anthropic provider |
| boabot | `github.com/go-git/go-git/v5` | GitHub backup |

---

## Breaking Changes

### Config Schema

The `aws` section is removed from `config.yaml`. Any existing config file with an `aws:` block will fail validation at startup. Migration path: remove the `aws` section; add a `memory` section and at least one non-Bedrock provider.

### Environment Variables

`AWS_*` environment variables are no longer required (and no longer read) for core operation. `ANTHROPIC_API_KEY` and `BOABOT_BACKUP_TOKEN` are new.

### Credentials File

`~/.boabot/credentials` is new. Existing deployments using env vars are unaffected.

---

## Success Criteria and Acceptance Criteria

### Quality Gates

- `go fmt ./...` — no diff
- `go vet ./...` — no errors
- `golangci-lint run` — no errors
- `go test -race -coverprofile=coverage.out ./...` — all pass
- `go tool cover -func=coverage.out` — ≥ 90% per module

### Acceptance Criteria

- [ ] `boabot` starts with a single binary and runs all enabled bots in-process — no AWS credentials required.
- [ ] All six AWS infrastructure adapters replaced by local equivalents and old packages deleted.
- [ ] `boabot-team/cdk/` deleted.
- [ ] Memory files read/written under `<memory.path>/<bot-name>/` with no S3 calls.
- [ ] Bots communicate via in-process channels; no SQS or SNS calls at runtime.
- [ ] `infrastructure/anthropic` passes end-to-end invocation test (env-var gated in CI).
- [ ] `boabotctl memory backup/restore/status` work against a configured GitHub repo.
- [ ] Crashed bot is restarted with exponential back-off without bringing down the process.
- [ ] Budget counters survive a clean restart (loaded from `budget.json` on startup).
- [ ] `go fmt`, `go vet`, `golangci-lint` pass; test coverage ≥ 90% across all modules.
- [ ] Cosine similarity search over 100k stored vectors completes in < 100ms (verified by benchmark test).
- [ ] `local/watchdog`: at `heap_warn_mb` threshold a warning log is emitted; at `heap_hard_mb` `TeamManager.Shutdown()` is called and the process exits cleanly.
- [ ] When `memory.embedder` is set to a provider name that has no embedding endpoint, `boabot` exits at startup with a human-readable error (not a panic).
- [ ] When `backup.restore_on_empty: true` and `Restore()` fails, `boabot` exits at startup with a non-zero code and a clear error message.
- [ ] A `config.yaml` containing an `aws:` block is rejected at parse time with a clear error message.
- [ ] `boabot` starts successfully when `~/.boabot/credentials` does not exist, provided required credentials are supplied via env vars.
- [ ] `boabot` refuses to start if `~/.boabot/credentials` exists and is world-readable (mode not 0600).

---

## Risks and Mitigation

| Risk | Likelihood | Mitigation |
|---|---|---|
| In-process bots share heap — one bad bot can OOM the process | Medium | Heap watchdog + configurable limits + per-bot BudgetTracker |
| BM25 fallback produces lower-quality memory retrieval | Medium | Document trade-off; require explicit embedder config for semantic search |
| Budget counters lost on crash before flush | Low | 10s default flush interval; WAL pattern available if needed |
| Anthropic SDK API surface changes | Low | Adapter isolated behind ModelProvider interface |
| Large teams hit goroutine scheduler pressure | Low | Benchmark before GA; model invocations are the real bottleneck |
| GitHub PAT leaked via config or logs | Medium | Token from env var or credentials file only; never logged |
| go-git push fails silently during network outage | Low | Retry with exponential back-off; emit metric; non-fatal |
| Backup repo grows unboundedly | Low | Document git gc + shallow-clone restore |

---

## Timeline and Milestones

| Milestone | Scope |
|---|---|
| M1 | Local infrastructure adapters: `queue`, `bus`, `fs`, `budget` — pure adapter swap, no behaviour change |
| M2 | Anthropic provider adapter (`infrastructure/anthropic`) |
| M3 | In-process vector store + embedder (`local/vector`, BM25/semantic) |
| M4 | `application/team` — TeamManager, BotRegistry, lifecycle; `cmd/boabot/main.go` rewire |
| M5 | GitHub backup — `infrastructure/github/backup`, `application/backup`, `boabotctl memory` subcommands |
| M6 | Config schema migration + credentials file + `local/watchdog` |
| M7 | Remove AWS packages + CDK; docs update; final quality pass |

---

## Edge Cases

| Component | Edge Case | Required Behaviour |
|---|---|---|
| `local/queue` | Buffered channel full (slow consumer) | `Send` returns an error immediately — never blocks caller goroutine |
| `local/bus` | Subscriber goroutine panics during fan-out | Broadcaster recovers the panic, logs it, and continues delivering to remaining subscribers |
| `application/team` | Bot `config.yaml` or `SOUL.md` missing/malformed at startup | `TeamManager` logs the error and skips that bot; remaining bots start normally. Zero enabled bots is a fatal startup error. |
| `infrastructure/local/fs` | `memory.path` directory cannot be created (permission denied) | Fatal startup error with clear message — not deferred to first write |
| `infrastructure/github/backup` | `Backup()` called when memory path is empty | Skip commit and push; log "nothing to back up"; return nil |
| `infrastructure/github/backup` | Remote has diverged at push time | Pull with rebase (local always wins on conflict); retry push once; fail with error if still diverged |
| `~/.boabot/credentials` | File does not exist | Boot normally if required values present in env vars |
| `~/.boabot/credentials` | File exists but is world-readable | Fatal startup error: refuse to start |
| `local/vector` | `.vec` file is corrupt or truncated on read | Skip the corrupted entry, log a warning, continue search over valid entries |
| `local/budget` | Flush to `budget.json` fails (disk full) | Log error; continue in-memory; retry on next flush interval |

---

## Open Questions

| # | Question | Blocks | Target |
|---|---|---|---|
| OQ-1 | `.vec` binary encoding: raw float32 little-endian or length-prefixed with dimension count? | M3 (local/vector) | Resolve in research phase |
| OQ-2 | BM25 implementation: use `github.com/blugelabs/bluge` / `github.com/blevesearch/bleve`, or hand-roll a minimal sparse-vector BM25? | M3 (BM25Embedder) | Resolve in research phase |
| OQ-3 | INI credentials parser: use `github.com/go-ini/ini` or write a minimal parser? Preference is minimal if the library adds significant transitive dependencies. | M6 (credentials) | Resolve in research phase |

---

## References

- Source PRD: `specs/260506-remove-aws-infra/remove-aws-infra-PRD.md`
- Change Request: `remove-aws-infra-cr.md`
- Product spec: `PRODUCT.md`
- Coding standards: `AGENTS.md`
