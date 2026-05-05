# boabot — Agent Runtime

The core BaoBot agent binary. All bots in the team run this binary, differentiated at startup by injected configuration and SOUL.md.

## What It Does

- Polls SQS, monitors Slack and Teams, spawns worker threads for incoming tasks.
- Executes tasks agentically using a configured language model, built-in harness tools, MCP tools, and Agent Skills.
- Maintains a git-backed memory directory synced to private S3; uses S3 Vectors for semantic search.
- Enforces Tool Attention (BM25 scoring) to keep injected tool schemas under the 20-tool cap.
- Checkpoints worker state and restarts when context window approaches capacity.
- Tracks token spend and tool call counts in memory, flushed to DynamoDB every 30 seconds.
- When `orchestrator.enabled: true`: runs the control plane, Kanban board, REST API, and web UI.

## Documentation

- [`docs/product-summary.md`](docs/product-summary.md) — what this module does
- [`docs/product-details.md`](docs/product-details.md) — features and behaviour
- [`docs/technical-details.md`](docs/technical-details.md) — architecture and key packages
- [`docs/architectural-decision-record.md`](docs/architectural-decision-record.md) — decisions specific to this module

## User Documentation

- [`user-docs/configuration.md`](user-docs/configuration.md) — config file reference
- [`user-docs/orchestrator.md`](user-docs/orchestrator.md) — running in orchestrator mode

## Development

See [`AGENTS.md`](AGENTS.md) for package structure and [`CLAUDE.md`](CLAUDE.md) for Claude Code guidance.

### Build

```bash
go build -o bin/boabot ./cmd/boabot
```

### Test

```bash
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

### Lint

```bash
go fmt ./... && go vet ./... && golangci-lint run
```

## Configuration

The binary reads `config.yaml` from its own directory by default. See [`user-docs/configuration.md`](user-docs/configuration.md) for the full reference. Use `config.example.yaml` as a starting point — never commit a real config file.

## Infrastructure

Shared infrastructure (ECS cluster, ALB, RDS, SNS, DynamoDB, ECR) is defined in [`cdk/`](cdk/). Per-bot infrastructure is defined in [`../boabot-team/cdk/`](../boabot-team/cdk/).
