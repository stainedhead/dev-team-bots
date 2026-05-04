# CLAUDE.md — boabot

Agent runtime module. See root `CLAUDE.md` for repo-wide rules. This file adds boabot-specific guidance.

## What This Module Does

Long-running agent process. Main thread monitors SQS + Slack + Teams. Worker threads execute tasks using the model provider, MCP tools, and skills. Orchestrator mode adds control plane, Kanban board, REST API, and web UI.

## Critical Rules

- **TDD always.** No production code without a failing test.
- **No AWS SDK in domain or application packages.** Only in `internal/infrastructure/`.
- **Orchestrator mode is additive.** All orchestrator features are behind the config flag — removing the flag must leave a normal bot running cleanly.
- **Worker thread panics must not kill the main thread.** Use recover() in worker goroutines.

## Key Commands

```bash
# Build
go build -o bin/boabot ./cmd/boabot

# Test with coverage
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# Lint
go fmt ./... && go vet ./... && golangci-lint run
```

## Adding a New Infrastructure Adapter

1. Define the interface in `internal/domain/` if it doesn't exist.
2. Write a failing test for the use case that needs it (mock the interface).
3. Implement the adapter in `internal/infrastructure/<service>/`.
4. Write integration tests for the adapter separately, tagged `//go:build integration`.
5. Wire it in `cmd/boabot/main.go`.

## Adding Orchestrator Features

All orchestrator-specific code lives in packages clearly named or tagged for orchestrator mode. The config flag `orchestrator.enabled` gates their activation. Do not let orchestrator code paths execute on non-orchestrator bots.

## Config File

Default location: next to the binary (`./config.yaml`). Override with `--config` flag. Never commit a real config file — use `config.example.yaml` as the template.

## Docs to Update When Changing This Module

- `docs/technical-details.md` — if architecture or key packages change.
- `docs/product-details.md` — if agent behaviour changes.
- `docs/architectural-decision-record.md` — for significant decisions.
- Root `docs/technical-details.md` — if system-level architecture changes.
