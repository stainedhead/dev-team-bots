# AGENTS.md — boabot

This is the BaoBot agent runtime. All bots in the team run this binary, differentiated by injected configuration.

## Module Purpose

`boabot` implements the long-running agent process. It provides:
- Main thread: monitors SQS queue, Slack, and Microsoft Teams for inbound messages.
- Worker threads: agent harness instances that execute tasks using model inference, built-in tools, MCP tools, and skills. Each worker is guarded with `recover()`.
- Tool Attention: BM25 scoring selects which tool schemas are fully injected (cap: 20); the rest are stubs.
- Context management: checkpoints durable state and restarts worker threads when context window approaches the threshold.
- Budget tracking: token spend and tool call counts enforced in memory, flushed to DynamoDB every 30 seconds.
- Agent Skills: modular capability packages loaded from S3; scripts run in restricted subprocesses.
- Orchestrator mode: control plane, Kanban board, REST API, and web UI (enabled by config flag).
- Memory: local git-backed directory synced to private S3 via ETag comparison; S3 Vectors for semantic search.
- Model provider abstraction: Bedrock and OpenAI-compatible endpoints via a named provider factory.

## Package Structure

```
cmd/boabot/         # main — wiring only
internal/
  domain/
    agent.go        # Agent, ChannelMonitor, BotIdentity
    worker.go       # Worker, Task, TaskResult, WorkerFactory
    message.go      # message types and payloads (register, heartbeat, task,
                    #   delegation, team_snapshot, memory_write, shutdown, ...)
    memory.go       # MemoryStore, VectorStore, Embedder
    provider.go     # ModelProvider, ProviderFactory
    mcp.go          # MCPClient, MCPTool, MCPToolResult
    queue.go        # MessageQueue, Broadcaster
    tool.go         # Tool, ToolStub, ToolScorer, ToolGater
    skill.go        # Skill, SkillStatus, SkillRegistry
    budget.go       # BudgetTracker
    card.go         # AgentCard, CardRegistry
    orchestrator.go # ControlPlane, BoardStore, UserStore and related types
    mocks/          # generated/hand-written mocks for all interfaces
  application/
    run_agent.go        # top-level agent loop use case
    process_message.go  # message routing and dispatch
    execute_task.go     # worker thread execution harness
    context_manager.go  # progressive disclosure, checkpoint-and-restart
    register.go         # registration, team_snapshot, heartbeat use cases
    memory_ops.go       # read, write, search memory use cases
    delegation.go       # send/receive structured delegation messages
    skills.go           # skill index loading and script execution
    budget.go           # budget cap enforcement and DynamoDB flush
    orchestrator/       # orchestrator-mode use cases
  infrastructure/
    aws/
      sqs/        # SQS MessageQueue adapter
      sns/        # SNS Broadcaster adapter
      s3/         # S3 MemoryStore adapter (ETag-based object sync)
      s3vectors/  # S3 Vectors VectorStore adapter
      bedrock/    # Bedrock ModelProvider adapter
      secrets/    # Secrets Manager credential loader
      dynamodb/   # DynamoDB BudgetTracker adapter
    mcp/          # MCP client adapter (with typed credential resolution)
    bm25/         # BM25 ToolScorer implementation
    openai/       # OpenAI-compatible ModelProvider adapter
    slack/        # Slack channel monitor adapter
    teams/        # Microsoft Teams adapter
    http/         # REST API server and web UI handler (orchestrator mode)
    db/           # MariaDB adapters (orchestrator mode)
    config/       # config file loading (YAML)
```

## Key Interfaces (domain layer)

- `Agent` — lifecycle: Start, Stop.
- `ChannelMonitor` — Start, Stop (Slack/Teams monitors).
- `Worker` — Execute(task) Task — runs a single agentic task.
- `WorkerFactory` — New() Worker.
- `MessageQueue` — Send, Receive, Delete (SQS adapter target).
- `Broadcaster` — Broadcast(message) (SNS adapter target).
- `MemoryStore` — Write, Read, Delete (S3 ETag-sync adapter target).
- `VectorStore` — Upsert, Search (S3 Vectors adapter target).
- `Embedder` — Embed(text) []float32.
- `ModelProvider` — Invoke(prompt, options) (Bedrock and OpenAI adapters).
- `ProviderFactory` — Get(name) ModelProvider.
- `MCPClient` — ListTools, CallTool.
- `ToolScorer` — Score(query, tools) []ScoredTool (BM25 adapter target).
- `ToolGater` — Select(intent, allTools) → full schemas + stubs.
- `SkillRegistry` — List, Get, Approve, Reject, Revoke.
- `BudgetTracker` — CheckAndRecordTokens, CheckAndRecordToolCall, Flush.
- `CardRegistry` — Get, Set, List (local in-memory Agent Card cache).
- `ControlPlane`, `BoardStore`, `UserStore` — orchestrator mode only.

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
