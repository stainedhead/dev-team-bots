# boabot — Agent Runtime

The core BaoBot agent binary. All bots in the team run this binary, differentiated at startup by injected configuration and SOUL.md.

## What It Does

- Polls the in-process queue, monitors Slack and Buzz (a Nostr-based relay protocol), spawns worker threads for incoming tasks. Alternatively, `boabot -acp` runs a single persona as a stdio Agent Client Protocol agent, registrable as a `buzz-acp` custom harness — it shares the same task-execution engine as native mode (including `models.chat_provider` support), conversation continuity, recurring-task scheduling, and per-task board visibility, and gets its own board-completion, plugin, and CLI tools when the persona's config enables them, though it has no mid-task clarifying-question support (blocked on an unstable upstream ACP protocol extension `buzz-acp` doesn't implement — see ADR-B030).
- Any number of `boabot-team` personas with their own `buzz:` config can each run their own Buzz identity and `ChannelMonitor` at once, all as goroutines in one shared process with one orchestrator web UI — true multi-agent Buzz conversations, not a single process-wide identity. A Buzz-dispatched task creates a real, `bot_name`-tagged Task and Kanban board item, visible live in the orchestrator UI.
- Every Buzz-enabled persona is also reachable via a direct 1:1 NIP-17 encrypted DM to its own pubkey — no separate DM key or config flag — and a human replying in-thread to a bot's prior channel or DM message continues that conversation without re-`@mention`ing it. See [`user-docs/Buzz-Adoption-Config.md`](user-docs/Buzz-Adoption-Config.md) for the unauthorized-DM-sender default (silently ignored, not declined) and other DM-specific behavior.
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
- [`user-docs/Buzz-Adoption-Config.md`](user-docs/Buzz-Adoption-Config.md) — Buzz (Nostr relay): enabling the channel, secret provisioning per OS/mode, multi-agent (one identity per persona), NIP-17 encrypted DMs, and threaded-reply continuation
- [`user-docs/buzz-multi-agent-getting-started.md`](user-docs/buzz-multi-agent-getting-started.md) — walkthrough: provisioning a second (or third) Buzz-enabled persona in the same process
- [`user-docs/ACP-Harness-Adoption-Config.md`](user-docs/ACP-Harness-Adoption-Config.md) — registering a persona as a `buzz-acp` custom harness (Agent Client Protocol over stdio), an alternative to the native Buzz channel

**Planned / roadmap (not yet implemented — no code exists for this today):**

- [`user-docs/Microsoft-Teams-Adoption-Config.md`](user-docs/Microsoft-Teams-Adoption-Config.md) — Microsoft Teams; describes the intended configuration so you can plan ahead, not a usable integration

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

Measured on domain and application packages (excluding `mocks/`, `cmd/`, `config/`). **The 90% target is an aggregate gate, not a per-package minimum**: CI's `boabot.yml` Coverage-check step computes one combined total across every package below (`go tool cover -func` on a single `-coverprofile` spanning `internal/domain/...` + `internal/application/...`) and checks that one number against 90% — currently 92.2%. Individual packages below 90% in the table (e.g. `internal/application/team` at 81.9%, dragged down by large pre-existing, mostly-untested functions like `startBot`) are not, on their own, a gate failure — AGENTS.md's actual hard rule is "do not reduce coverage when adding code" to any package, not that every package individually clears 90%.

| Package | LOC | Coverage |
|---|---|---|
| `internal/domain` | 1507 | 94.9% |
| `internal/domain/cost` | 126 | 100% |
| `internal/domain/eta` | 74 | 100% |
| `internal/domain/screening` | 41 | 100% |
| `internal/domain/workflow` | 225 | 100% |
| `internal/application` | 579 | 99.0% |
| `internal/application/backup` | 74 | 100% |
| `internal/application/cost` | 156 | 100% |
| `internal/application/eta` | 30 | 100% |
| `internal/application/metrics` | 66 | 100% |
| `internal/application/notifications` | 181 | 94.8% |
| `internal/application/orchestrator` | 1143 | 95.3% |
| `internal/application/plugin` | 256 | 93.1% |
| `internal/application/pool` | 259 | 97.8% |
| `internal/application/rebalancing` | 74 | 100% |
| `internal/application/scheduler` | 296 | 98.6% |
| `internal/application/scheduling` | 129 | 91.3% |
| `internal/application/screening` | 37 | 100% |
| `internal/application/subteam` | 328 | 91.6% |
| `internal/application/team` | 1456 | 81.9% |
| `internal/application/workflow` | 393 | 98.9% |

Run `go test -race -coverprofile=coverage.out ./internal/domain/... ./internal/application/... && go tool cover -func=coverage.out` to reproduce.
