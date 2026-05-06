# AGENTS.md — boabot

This is the BaoBot agent runtime. All bots in the team run this binary, differentiated by injected configuration.

## Module Purpose

Local single-binary agent runtime. `TeamManager` runs all enabled bots as in-process goroutines. It provides:
- Main thread: monitors the local in-process message queue, Slack, and Microsoft Teams for inbound messages.
- Worker threads: agent harness instances that execute tasks using model inference, built-in tools, MCP tools, and skills. Each worker is guarded with `recover()`.
- Tool Attention: BM25 scoring selects which tool schemas are fully injected (cap: 20); the rest are stubs.
- Context management: checkpoints durable state and restarts worker threads when context window approaches the threshold.
- Budget tracking: token spend and tool call counts enforced in memory, persisted to `budget.json` (`local/budget`).
- Memory: local filesystem (`local/fs`) with optional GitHub git backup; BM25 + cosine similarity vector store (`local/vector`) for semantic search.
- Agent Skills: modular capability packages; scripts run in restricted subprocesses.
- Orchestrator mode: control plane, Kanban board, REST API, and web UI (enabled by config flag).
- Model provider abstraction: Anthropic (primary) and Bedrock/OpenAI-compatible endpoints via a named provider factory.
- Heap watchdog: graceful shutdown when memory exceeds the configured hard limit.
- No AWS services are required to run.

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
    queue.go        # MessageQueue
    tool.go         # Tool, ToolStub, ToolScorer, ToolGater
    skill.go        # Skill, SkillStatus, SkillRegistry
    budget.go       # BudgetTracker
    card.go         # AgentCard, CardRegistry
    orchestrator.go # ControlPlane, BoardStore, UserStore and related types
    mocks/          # generated/hand-written mocks for all interfaces
  application/
    team/           # TeamManager — starts and supervises all bot goroutines
    backup/         # GitHub memory backup use case
    orchestrator/   # orchestrator-mode use cases
    ... (other use case packages)
  infrastructure/
    anthropic/      # Anthropic ModelProvider adapter (primary)
    aws/bedrock/    # Bedrock ModelProvider adapter (optional)
    local/
      queue/        # in-process message queue and router
      bus/          # in-process event bus
      fs/           # filesystem MemoryStore adapter
      vector/       # cosine similarity VectorStore adapter
      bm25/         # BM25 Embedder/ToolScorer
      budget/       # budget.json BudgetTracker adapter
      watchdog/     # heap watchdog goroutine
    github/backup/  # GitHub git backup adapter
    openai/         # OpenAI-compatible ModelProvider adapter
    mcp/            # MCP client adapter
    slack/          # Slack ChannelMonitor adapter
    teams/          # Microsoft Teams adapter
    http/           # REST API server and web UI handler (orchestrator mode)
    db/             # MariaDB adapters (orchestrator mode)
    config/         # config file loading (YAML)
    credentials/    # credentials file loader
```

## Key Interfaces (domain layer)

- `Agent` — lifecycle: Start, Stop.
- `ChannelMonitor` — Start, Stop (Slack/Teams monitors).
- `Worker` — Execute(task) Task — runs a single agentic task.
- `WorkerFactory` — New() Worker.
- `MessageQueue` — Send, Receive, Delete (local queue adapter target).
- `MemoryStore` — Write, Read, Delete (local filesystem adapter target).
- `VectorStore` — Upsert, Search (local cosine similarity adapter target).
- `Embedder` — Embed(text) []float32 (local BM25 adapter target).
- `ModelProvider` — Invoke(prompt, options) (Anthropic, Bedrock, OpenAI adapters).
- `BudgetTracker` — CheckAndRecordTokens, CheckAndRecordToolCall, Flush (budget.json adapter target).
- `ProviderFactory` — Get(name) ModelProvider.
- `MCPClient` — ListTools, CallTool.
- `ToolScorer` — Score(query, tools) []ScoredTool (BM25 adapter target).
- `ToolGater` — Select(intent, allTools) → full schemas + stubs.
- `SkillRegistry` — List, Get, Approve, Reject, Revoke.
- `CardRegistry` — Get, Set, List (local in-memory Agent Card cache).
- `ControlPlane`, `BoardStore`, `UserStore` — orchestrator mode only.

## Development Rules

- Follow TDD: failing test before any production code.
- 90%+ coverage target. Run `go test -race -coverprofile=coverage.out ./...` to check.
- All infrastructure calls go through interfaces. No AWS SDK imports in `domain/` or `application/`.
- **Orchestrator mode is additive.** All orchestrator features are behind the `orchestrator.enabled` config flag — removing the flag must leave a normal bot running cleanly.
- **Worker thread panics must not kill the main thread.** Use `recover()` in all worker goroutines.
- `cmd/boabot/main.go` does wiring only — instantiate adapters, inject into use cases, start the agent.
- Mocks live in `internal/domain/mocks/`.

## Pull Requests

After opening a PR with `gh pr create`, immediately enable automerge:

```bash
gh pr merge --auto --merge <PR-number>
```

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

## Adding a New Infrastructure Adapter

1. Define the interface in `internal/domain/` if it doesn't exist.
2. Write a failing test for the use case that needs it (mock the interface).
3. Implement the adapter in `internal/infrastructure/<service>/`.
4. Write integration tests for the adapter separately, tagged `//go:build integration`.
5. Wire it in `cmd/boabot/main.go`.

## Adding Orchestrator Features

All orchestrator-specific code lives in packages clearly named or tagged for orchestrator mode. The config flag `orchestrator.enabled` gates their activation. Do not let orchestrator code paths execute on non-orchestrator bots.

## Docs to Update When Changing This Module

- `docs/technical-details.md` — if architecture or key packages change.
- `docs/product-details.md` — if agent behaviour changes.
- `docs/architectural-decision-record.md` — for significant decisions.
- Root `docs/technical-details.md` — if system-level architecture changes.
