# boabot — Agent Runtime

The core BaoBot agent binary. All bots in the team run this binary, differentiated at startup by injected configuration and SOUL.md.

## What It Does

- Polls the in-process queue, monitors Slack and Teams, spawns worker threads for incoming tasks.
- Executes tasks agentically using a configured language model, built-in harness tools, MCP tools, and Agent Skills.
- Maintains a local git-backed memory directory with optional GitHub backup; uses a local BM25 embedder and cosine similarity vector store for semantic search.
- Enforces Tool Attention (BM25 scoring) to keep injected tool schemas under the 20-tool cap.
- Checkpoints worker state and restarts when context window approaches capacity.
- Tracks token spend and tool call counts in a local JSON-backed budget tracker.
- When `orchestrator.enabled: true`: runs the control plane, Kanban board, REST API, web UI, and tech-lead pool.
- Tech-lead bots can dynamically spawn and manage isolated sub-agent goroutines via `SubTeamManager`.

## Documentation

- [`docs/product-summary.md`](docs/product-summary.md) — what this module does
- [`docs/product-details.md`](docs/product-details.md) — features and behaviour
- [`docs/technical-details.md`](docs/technical-details.md) — architecture and key packages
- [`docs/architectural-decision-record.md`](docs/architectural-decision-record.md) — decisions specific to this module

## User Documentation

- [`user-docs/getting-started.md`](user-docs/getting-started.md) — quick start
- [`user-docs/configuration.md`](user-docs/configuration.md) — config file reference
- [`user-docs/orchestrator.md`](user-docs/orchestrator.md) — running in orchestrator mode
- [`user-docs/subteam-spawning.md`](user-docs/subteam-spawning.md) — tech-lead subteam spawning
- [`user-docs/pool-management.md`](user-docs/pool-management.md) — orchestrator tech-lead pool
- [`user-docs/Claude-Adoption-Config.md`](user-docs/Claude-Adoption-Config.md) — Anthropic Claude API (model selection, rate limits, multi-provider)
- [`user-docs/AWS-Bedrock-Adoption-Config.md`](user-docs/AWS-Bedrock-Adoption-Config.md) — AWS Bedrock (SSO, service account, machine identity)
- [`user-docs/OpenAI-Adoption-Config.md`](user-docs/OpenAI-Adoption-Config.md) — OpenAI-compatible endpoints (OpenAI, Ollama, vLLM, OpenRouter, Azure)
- [`user-docs/Slack-Adoption-Config.md`](user-docs/Slack-Adoption-Config.md) — Slack Socket Mode (DMs and @mentions)
- [`user-docs/Microsoft-Teams-Adoption-Config.md`](user-docs/Microsoft-Teams-Adoption-Config.md) — Microsoft Teams (planned)

### Plugin Registry

- [`user-docs/plugins-getting-started.md`](user-docs/plugins-getting-started.md) — install your first plugin
- [`user-docs/plugins-configuration.md`](user-docs/plugins-configuration.md) — config reference for the plugin system
- [`user-docs/plugins-api.md`](user-docs/plugins-api.md) — REST API reference (all 14 endpoints)
- [`user-docs/plugins-manifest.md`](user-docs/plugins-manifest.md) — `plugin.yaml` format reference

### Bot Capabilities

- [`user-docs/plugin-skills.md`](user-docs/plugin-skills.md) — how bots discover and use plugin skills via `read_skill`
- [`user-docs/cli-agent-tools.md`](user-docs/cli-agent-tools.md) — delegating tasks to Claude Code, Codex, and opencode via MCP tools

## Plugin Registry

Admins can install versioned capability packages from one or more HTTPS-hosted registries. Each plugin provides MCP tools that are dynamically available to all bots.

- Default registry: `stainedhead/shared-plugins` (trusted).
- Trusted-registry plugins activate immediately after checksum verification; untrusted-registry plugins require admin approval.
- Install, approve, reload, and remove plugins via the admin UI or `boabotctl plugin` commands.
- Plugin archives are extracted atomically with SHA-256 checksum verification, zip-slip protection, and a 50 MB size cap.

## Bot Capabilities

Beyond the built-in harness tools, all bots have access to two additional capability layers:

**Plugin skills via `read_skill`:** Any bot can call `read_skill(<name>)` to load the Markdown instruction file for any skill provided by an active plugin (e.g. `read_skill("review-code")`). The bot reads the instructions and executes each step autonomously — no separate executor is required. This is how Claude Code plugins (such as the `dev-flow` suite) are consumed by the bot ecosystem.

**CLI agent delegation:** When enabled in config and the binary is on `PATH`, bots can delegate coding tasks to external CLI agents via four MCP tools:

| Tool | Binary required | Use case |
|---|---|---|
| `run_claude_code` | `claude` | Delegate implementation or review tasks to Claude Code |
| `run_codex` | `codex` | Delegate to the OpenAI Codex CLI |
| `run_openai_codex` | `openai-codex` | Delegate to the open-source OpenAI Codex CLI |
| `run_opencode` | `opencode` | Delegate to the opencode CLI |

All four tools accept `instruction`, `work_dir`, and an optional `model` override. They are gated by config (`orchestrator.cli_tools.*`) and silently absent when the binary is not found. See [`user-docs/cli-agent-tools.md`](user-docs/cli-agent-tools.md) for setup instructions.

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

## Package Coverage and Size

Measured on domain and application packages (excluding `mocks/`, `cmd/`, `config/`). Target: ≥ 90% per package.

| Package | LOC | Coverage |
|---|---|---|
| `internal/domain` | 933 | 100% |
| `internal/domain/cost` | 126 | 100% |
| `internal/domain/eta` | 74 | 100% |
| `internal/domain/screening` | 41 | 100% |
| `internal/domain/workflow` | 225 | 100% |
| `internal/application` | 543 | 98.9% |
| `internal/application/backup` | 74 | 100% |
| `internal/application/cost` | 156 | 100% |
| `internal/application/eta` | 30 | 100% |
| `internal/application/metrics` | 66 | 100% |
| `internal/application/orchestrator` | 309 | 97.8% |
| `internal/application/plugin` | 256 | 93.1% |
| `internal/application/pool` | 259 | 97.8% |
| `internal/application/rebalancing` | 74 | 100% |
| `internal/application/scheduler` | 296 | 98.6% |
| `internal/application/screening` | 37 | 100% |
| `internal/application/subteam` | 328 | 91.6% |
| `internal/application/team` | 1176 | 76.3% |
| `internal/application/workflow` | 393 | 98.9% |

Run `go test -race -coverprofile=coverage.out ./internal/domain/... ./internal/application/... && go tool cover -func=coverage.out` to reproduce.
