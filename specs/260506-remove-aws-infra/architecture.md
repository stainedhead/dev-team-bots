# Architecture: Remove AWS Infrastructure — Local Single-Binary Runtime

**Feature:** remove-aws-infra
**Created:** 2026-05-06
**Status:** Draft

---

## Architecture Overview

[TBD — populate after research phase]

The high-level change is: boabot moves from a distributed, cloud-hosted model (multiple ECS tasks communicating via SQS/SNS, persisting to S3/DynamoDB) to a single in-process model (one binary, goroutines per bot, in-memory channels, filesystem persistence).

Domain interfaces are unchanged. Only the infrastructure adapters swap.

---

## Component Architecture

[TBD — diagram: TeamManager → BotRegistry → [Bot goroutines] → [local/queue, local/bus, local/fs, local/vector, local/budget]]

---

## Layer Responsibilities

| Layer | Responsibility |
|---|---|
| `domain/` | Interfaces only — unchanged |
| `application/team/` | Multi-bot lifecycle: start, health-check, restart, shutdown |
| `application/backup/` | Scheduled backup use case driven by existing scheduler |
| `infrastructure/local/` | In-process adapter implementations |
| `infrastructure/anthropic/` | Anthropic SDK model provider |
| `infrastructure/github/backup/` | go-git-backed MemoryBackup |
| `cmd/boabot/main.go` | Wiring only: instantiates TeamManager, blocks on signal |

---

## Data Flow

### Bot startup
[TBD]

### Message routing (bot → bot)
[TBD — local/bus fan-out via BotRegistry]

### Memory read/write
[TBD — local/fs: <memory.path>/<bot-name>/<key>]

### Vector search
[TBD — local/vector: load all .vec files, cosine similarity, return top-k]

### Backup
[TBD — ScheduledBackupUseCase → go-git Backup() → push to GitHub remote]

---

## Sequence Diagrams

[TBD]

---

## Integration Points

| System | How integrated | Auth |
|---|---|---|
| Anthropic API | anthropic-sdk-go | ANTHROPIC_API_KEY env var or credentials file |
| GitHub (backup) | go-git over HTTPS | BOABOT_BACKUP_TOKEN env var or credentials file |
| OpenAI (existing) | HTTP to configured endpoint | env var or credentials file |
| AWS Bedrock (existing, optional) | AWS SDK | AWS credential chain (unchanged) |

---

## Architectural Decisions

[TBD — record significant decisions made during implementation here; reference resolved decisions from remove-aws-infra-cr.md §6]
