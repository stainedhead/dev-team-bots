# Tasks: Remove AWS Infrastructure — Local Single-Binary Runtime

**Feature:** remove-aws-infra
**Created:** 2026-05-06
**Status:** Planning

---

## Progress Summary

**0 / 0 tasks complete**

---

## M1 — Local Infrastructure Adapters

| ID | Task | Deps | Est (h) | Status |
|---|---|---|---|---|
| M1.1 | Implement `infrastructure/local/queue` — buffered channel MessageQueue | — | 3 | Not Started |
| M1.2 | Implement `infrastructure/local/bus` — fan-out Broadcaster | — | 2 | Not Started |
| M1.3 | Implement `infrastructure/local/fs` — filesystem MemoryStore | — | 2 | Not Started |
| M1.4 | Implement `infrastructure/local/budget` — atomic BudgetTracker + JSON flush | — | 3 | Not Started |

**Acceptance criteria (M1):** All four adapters pass interface compliance tests. `go test -race` clean. Coverage ≥ 90% on each package.

---

## M2 — Anthropic Provider

| ID | Task | Deps | Est (h) | Status |
|---|---|---|---|---|
| M2.1 | Add `github.com/anthropics/anthropic-sdk-go` dependency | — | 0.5 | Not Started |
| M2.2 | Implement `infrastructure/anthropic` ModelProvider | M2.1 | 4 | Not Started |
| M2.3 | Integration test (ANTHROPIC_API_KEY gated) | M2.2 | 2 | Not Started |

**Acceptance criteria (M2):** Provider passes unit tests with mock transport. Integration test invokes real API when key is present. StopReason and TokenUsage populated in response.

---

## M3 — Vector Store + Embedder

| ID | Task | Deps | Est (h) | Status |
|---|---|---|---|---|
| M3.1 | Implement `infrastructure/local/vector` — cosine similarity VectorStore | — | 5 | Not Started |
| M3.2 | Implement BM25Embedder | — | 4 | Not Started |
| M3.3 | Implement provider-backed Embedder adapter (OpenAI / Anthropic) | M2.2 | 3 | Not Started |
| M3.4 | Benchmark: cosine search < 100ms at 100k vectors | M3.1 | 2 | Not Started |

**Acceptance criteria (M3):** Benchmark passes at 100k vectors. BM25 and provider embedder both produce valid `[]float32` consumed by vector store.

---

## M4 — TeamManager + main.go Rewire

| ID | Task | Deps | Est (h) | Status |
|---|---|---|---|---|
| M4.1 | Implement `application/team.BotRegistry` | M1.2 | 2 | Not Started |
| M4.2 | Implement `application/team.TeamManager` (start, restart, shutdown) | M4.1, M1.1, M1.3 | 8 | Not Started |
| M4.3 | Implement `local/watchdog` heap monitor | M4.2 | 3 | Not Started |
| M4.4 | Rewire `cmd/boabot/main.go` to TeamManager | M4.2 | 2 | Not Started |
| M4.5 | Smoke test: binary starts with minimal team.yaml, no AWS env vars | M4.4 | 2 | Not Started |

**Acceptance criteria (M4):** Binary starts and runs bots in-process. Crashed bot restarts with back-off. Graceful shutdown on SIGINT.

---

## M5 — GitHub Backup

| ID | Task | Deps | Est (h) | Status |
|---|---|---|---|---|
| M5.1 | Add `github.com/go-git/go-git/v5` dependency | — | 0.5 | Not Started |
| M5.2 | Implement `infrastructure/github/backup` MemoryBackup | M5.1 | 6 | Not Started |
| M5.3 | Implement `application/backup.ScheduledBackupUseCase` | M5.2 | 3 | Not Started |
| M5.4 | Add `boabotctl memory` subcommands (backup, restore, status) | M5.2 | 3 | Not Started |
| M5.5 | Integration test (BOABOT_BACKUP_TOKEN gated) | M5.2 | 2 | Not Started |

**Acceptance criteria (M5):** Backup/restore/status commands work against real GitHub repo when token is present. Unit tests pass with mock git transport.

---

## M6 — Config Schema + Credentials + Watchdog

| ID | Task | Deps | Est (h) | Status |
|---|---|---|---|---|
| M6.1 | Add `MemoryConfig` and `BackupConfig` to domain config structs | — | 2 | Not Started |
| M6.2 | Implement INI credentials file parser (`~/.boabot/credentials`) | — | 3 | Not Started |
| M6.3 | Add mode-0600 startup check for credentials file | M6.2 | 1 | Not Started |
| M6.4 | Validate embedder provider at startup (FR-014) | M6.1 | 2 | Not Started |
| M6.5 | Wire watchdog into TeamManager config | M4.3, M6.1 | 1 | Not Started |

**Acceptance criteria (M6):** Config with `aws` section rejected at parse time. Credentials file world-readable causes fatal startup error. BOABOT_PROFILE selects correct profile.

---

## M7 — Remove AWS + CDK + Docs

| ID | Task | Deps | Est (h) | Status |
|---|---|---|---|---|
| M7.1 | Delete `infrastructure/aws/sqs`, `sns`, `s3`, `s3vectors`, `dynamodb`, `secretsmanager`, `secrets` | M4.4 | 2 | Not Started |
| M7.2 | Delete `boabot-team/cdk/` | — | 0.5 | Not Started |
| M7.3 | Update `docs/product-summary.md`, `product-details.md`, `technical-details.md`, `ADR` | M7.1 | 3 | Not Started |
| M7.4 | Update `user-docs/` and `README.md` | M7.3 | 2 | Not Started |
| M7.5 | Final quality pass: fmt, vet, lint, test, coverage | M7.1 | 2 | Not Started |

**Acceptance criteria (M7):** All AWS packages gone. CDK gone. All acceptance criteria in spec.md pass. Coverage ≥ 90%.
