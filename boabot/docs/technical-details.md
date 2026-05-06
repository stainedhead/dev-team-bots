# Technical Details — boabot

## Package Layout

```
cmd/boabot/
  main.go               # wiring only — no business logic

internal/
  domain/
    agent.go            # Agent interface and lifecycle types
    worker.go           # Worker interface and task types
    memory.go           # MemoryStore, VectorStore, Embedder, MemoryBackup interfaces
    message.go          # message types: registration, heartbeat, task, delegation, broadcast, shutdown
    provider.go         # ModelProvider and ProviderFactory interfaces
    mcp.go              # MCPClient interface
    queue.go            # MessageQueue and Broadcaster interfaces
    tool.go             # Tool, ToolGater, and ToolScorer interfaces
    skill.go            # Skill and SkillRegistry interfaces
    budget.go           # BudgetTracker interface (token + tool-call caps; local JSON-backed)
    dlq.go              # DLQStore interface (list / retry / discard dead-letter queue items)
    card.go             # AgentCard types and CardRegistry interface
    orchestrator.go     # ControlPlane, BoardStore interfaces (orchestrator mode)
    user.go             # User, Role, and Auth interfaces (orchestrator mode)
    mocks/              # generated/hand-written mocks for all interfaces

  application/
    run_agent.go        # top-level agent loop use case
    process_message.go  # message routing and dispatch
    execute_task.go     # worker goroutine execution harness; optional BudgetTracker enforcement
                        # (WithBudgetTracker wires cost gate: CheckAndRecordToolCall before
                        #  Invoke, CheckAndRecordTokens after using actual usage counts)
    context_manager.go  # progressive disclosure, checkpoint-and-restart
    register.go         # bot registration, heartbeat use cases
    memory_ops.go       # read, write, search memory use cases
    delegation.go       # send/receive structured delegation messages
    skills.go           # skill index loading and script execution
    backup/             # ScheduledBackupUseCase (cron-driven GitHub backup)
    cost/               # EnforceBudgetUseCase, DailyCostReviewUseCase
    scheduler/          # cron-based task scheduler; TriageUseCase for label-based routing
    team/               # TeamManager (goroutine-per-bot), BotRegistry, localProviderFactory
    workflow/           # CreateWorkItem, AdvanceWorkflow, AssignBot, StalledItemRecovery

  infrastructure/
    aws/
      bedrock/          # Bedrock ModelProvider adapter (optional); RateLimitError for ThrottlingException
    anthropic/          # Anthropic API ModelProvider adapter (primary); rate-limit mapping
    credentials/        # INI credentials loader (~/.boabot/credentials); BOABOT_PROFILE env var
    github/
      backup/           # GitHubBackup implementing domain.MemoryBackup; go-git v5 based
    local/
      queue/            # in-process Router + per-bot Queue implementing domain.MessageQueue
      bus/              # in-process Bus implementing domain.Broadcaster
      fs/               # local filesystem FS implementing domain.MemoryStore
      budget/           # local JSON-backed BudgetTracker implementing domain.BudgetTracker
      vector/           # cosine similarity VectorStore implementing domain.VectorStore
                        # on-disk format: <key>.vec (LE binary) + <key>.meta (JSON)
                        # atomic writes via temp-file + os.Rename; O(n) flat-slice search
      bm25/             # BM25 feature-hash Embedder; FNV-1a hashing, L2-normalised, 512-dim
      watchdog/         # heap watchdog; injectable readMem seam; warn/hard limit goroutine
    auth/local/         # LocalAuthProvider: bcrypt (cost 12) + HS256 JWT (24 h TTL)
                        # VerifyPassword validates credentials without issuing a token
    db/                 # PostgreSQL repository: work items, workflow, metrics, users
                        # UserRepo: CRUD against `users` table; Enabled inverts the `disabled` column
    mcp/                # MCP client adapter
    otel/               # OpenTelemetry provider: OTLP/HTTP trace + metric exporters; noop fallback
    screening/          # RegexScreener: injection-pattern detection + [REDACTED] sanitisation
    workflow/           # ConfigLoader: YAML workflow config with SIGHUP hot-reload
    http/               # Orchestrator REST API (Go 1.22 ServeMux) + HTMX Kanban board web UI
                        # Auth: Bearer JWT middleware; admin-only guard on protected routes
                        # Routes: /api/v1/{auth,board,team,skills,users,profile,dlq}
                        # Web UI: / → HTMX Kanban board (auto-refreshes every 30s)
                        # htmx loaded from unpkg.com with SHA-384 SRI hash for supply-chain safety
    config/             # config file loading (YAML)
```

## Threading Model

```
main goroutine
  └── TeamManager.Run()
        ├── bot goroutine (per enabled bot, runBotWithRestart + recover)
        │     └── RunAgentUseCase.Run()
        │           └── Worker goroutines (per task, recover() guards panic)
        │                 └── ContextManager (progressive disclosure, checkpoint-and-restart)
        │                 └── ToolGater (BM25 scoring, schema injection)
        └── watchdog goroutine (heap monitor, optional; cancels shared context on hard limit)
```

## Config File Structure

```yaml
bot:
  name: <name>
  type: <type>

orchestrator:
  enabled: false
  api_port: 8080
  web_port: 8081

models:
  default: <provider-name>
  providers:
    - name: <provider-name>
      type: anthropic | bedrock | openai
      model_id: <model-id>
      endpoint: <url>        # openai only
      region: <region>       # bedrock only

memory:
  path: ./memory             # default: <binary-dir>/memory
  vector_index: cosine       # "cosine" (default) | future options
  embedder: bm25             # "bm25" (default) | "openai" (requires OPENAI_API_KEY)
  heap_warn_mb: 512          # 0 = disabled
  heap_hard_mb: 1024         # 0 = disabled

backup:
  enabled: false
  schedule: "*/30 * * * *"
  restore_on_empty: true
  github:
    repo: org/repo
    branch: main
    author_name: BaoBot
    author_email: baobot@example.com

team:
  file_path: ./team.yaml
  bots_dir: ./bots

tools:
  allowed_tools:            # built-in tools this bot may use
    - read_file
    - list_dir
    - glob
    - grep
    - write_file
    - edit_file
    - memory_search
    - send_message
    - read_messages
    - todo_write
    - todo_read
    - http_request
    - get_metrics
  http_allowed_hosts:       # hosts http_request may contact
    - api.github.com
    - hooks.slack.com
  receive_from:             # bots permitted to send action-triggering messages
    - orchestrator
    - architect

budget:
  token_spend_daily: 1000000
  tool_calls_hourly: 500

context:
  threshold_tokens: 150000  # trigger checkpoint-and-restart at this context size
```

Credentials (API keys) are **never** stored in `config.yaml`. They are read from `~/.boabot/credentials` (INI format) or environment variables at startup. The `BOABOT_PROFILE` environment variable selects a non-default profile.

## Key Design Decisions

See [`architectural-decision-record.md`](architectural-decision-record.md) for module-specific ADRs. See root [`docs/architectural-decision-record.md`](../../docs/architectural-decision-record.md) for system-level decisions.
