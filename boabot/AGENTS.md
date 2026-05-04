# AGENTS.md — boabot

This is the BaoBot agent runtime. All bots in the team run this binary, differentiated by injected configuration.

## Module Purpose

`boabot` implements the long-running agent process. It provides:
- Main thread: monitors SQS queue, Slack, and Microsoft Teams for inbound messages.
- Worker threads: agent harness instances that execute tasks using model inference, tools (MCP), and skills.
- Orchestrator mode: control plane, Kanban board, REST API, and web UI (enabled by config flag).
- Memory access: S3 Vectors (semantic search) and S3 Files (structured storage).
- Model provider abstraction: Bedrock and OpenAI-compatible endpoints via a named provider factory.

## Package Structure

```
cmd/boabot/         # main — wiring only
internal/
  domain/           # Agent, Worker, Memory, Provider, Message interfaces and entities
  application/      # use cases: RunAgent, ProcessMessage, ExecuteTask, RegisterBot, etc.
  infrastructure/
    aws/            # S3, SQS, SNS, Bedrock, Secrets Manager adapters
    mcp/            # MCP client implementation
    slack/          # Slack event monitoring adapter
    teams/          # Microsoft Teams adapter
    http/           # REST API and web UI server (orchestrator mode)
    db/             # RDS MariaDB adapters (orchestrator mode)
    config/         # config file loading and validation
```

## Key Interfaces (domain layer)

- `Agent` — lifecycle: Start, Stop, Register, Heartbeat.
- `Worker` — Execute(task) — runs a single agentic task.
- `MessageQueue` — Send, Receive, Delete (SQS adapter target).
- `Broadcaster` — Broadcast(message) (SNS adapter target).
- `MemoryStore` — Read, Write, Search (S3 Files + S3 Vectors adapters).
- `ModelProvider` — Invoke(prompt, options) (Bedrock and OpenAI adapters).
- `ProviderFactory` — Get(name) ModelProvider.
- `MCPClient` — ListTools, CallTool.

## Development Rules

- Follow TDD: failing test before any production code.
- 90%+ coverage target. Run `go test -race -coverprofile=coverage.out ./...` to check.
- All infrastructure calls go through interfaces. No AWS SDK imports in `domain/` or `application/`.
- `cmd/boabot/main.go` does wiring only — instantiate adapters, inject into use cases, start the agent.
- Mocks live in `internal/domain/mocks/`.

## Build

```bash
go build -o bin/boabot ./cmd/boabot
```

Config file is expected next to the binary at runtime. The binary reads `config.yaml` from its own directory by default.

## Test

```bash
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

## Lint

```bash
go fmt ./...
go vet ./...
golangci-lint run
```
