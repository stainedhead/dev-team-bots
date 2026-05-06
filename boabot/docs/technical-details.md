# Technical Details — boabot

## Package Layout

```
cmd/boabot/
  main.go               # wiring only — no business logic

internal/
  domain/
    agent.go            # Agent interface and lifecycle types
    worker.go           # Worker interface and task types
    memory.go           # MemoryStore interface (vectors + files)
    message.go          # message types: registration, heartbeat, task, delegation, broadcast, shutdown
    provider.go         # ModelProvider and ProviderFactory interfaces
    mcp.go              # MCPClient interface
    queue.go            # MessageQueue and Broadcaster interfaces
    tool.go             # Tool, ToolGater, and ToolScorer interfaces
    skill.go            # Skill and SkillRegistry interfaces
    budget.go           # BudgetTracker interface (token + tool-call caps; DynamoDB-backed)
    dlq.go              # DLQStore interface (list / retry / discard dead-letter queue items)
    card.go             # AgentCard types and CardRegistry interface
    orchestrator.go     # ControlPlane, BoardStore interfaces (orchestrator mode)
    user.go             # User, Role, and Auth interfaces (orchestrator mode)
    mocks/              # generated/hand-written mocks for all interfaces

  application/
    run_agent.go        # top-level agent loop use case
    process_message.go  # message routing and dispatch
    execute_task.go     # worker thread execution harness; optional BudgetTracker enforcement
                        # (WithBudgetTracker wires cost gate: CheckAndRecordToolCall before
                        #  Invoke, CheckAndRecordTokens after using actual usage counts)
    context_manager.go  # progressive disclosure, checkpoint-and-restart
    register.go         # bot registration, team_snapshot, heartbeat use cases
    memory_ops.go       # read, write, search memory use cases
    delegation.go       # send/receive structured delegation messages
    skills.go           # skill index loading and script execution
    cost/               # EnforceBudgetUseCase, DailyCostReviewUseCase
    scheduler/          # cron-based task scheduler; TriageUseCase for label-based routing
    workflow/           # CreateWorkItem, AdvanceWorkflow, AssignBot, StalledItemRecovery

  infrastructure/
    aws/
      sqs/              # SQS MessageQueue adapter
      sns/              # SNS Broadcaster adapter
      s3/               # S3 object sync adapter (ETag-based memory sync)
      s3vectors/        # S3 Vectors semantic search adapter
      bedrock/          # Bedrock ModelProvider adapter; RateLimitError for ThrottlingException
      secrets/          # Secrets Manager credential loader
      secretsmanager/   # SecretStore: TTL-cached GetSecret / GetSecretJSON
      dynamodb/         # DynamoDB BudgetTracker: per-bot daily spend, CheckBudget, DailySpend
                        # BudgetTrackerAdapter wraps BudgetTracker to satisfy domain.BudgetTracker
                        #   (botID, perBotCap, systemBudget injected at construction; Flush is no-op)
    auth/local/         # LocalAuthProvider: bcrypt (cost 12) + HS256 JWT (24 h TTL)
                        # VerifyPassword validates credentials without issuing a token
    db/                 # PostgreSQL repository: work items, workflow, metrics, users
                        # UserRepo: CRUD against `users` table; Enabled inverts the `disabled` column
    mcp/                # MCP client adapter (with typed credential resolution)
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
  └── Agent.Start()
        ├── SQS poll loop (main thread)
        ├── Slack monitor goroutine
        ├── Teams monitor goroutine
        ├── Budget flush goroutine (30s interval)
        └── Worker pool
              └── worker goroutine (per task, recover() guards panic)
                    └── ContextManager (progressive disclosure, checkpoint-and-restart)
                    └── ToolGater (BM25 scoring, schema injection)
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
      type: bedrock | openai
      model_id: <model-id>
      endpoint: <url>        # openai only
      region: <region>       # bedrock only

aws:
  region: us-east-1
  sqs_queue_url: <url>
  sns_topic_arn: <arn>
  private_bucket: <name>
  team_bucket: <name>
  dynamodb_budget_table: <name>

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

## Key Design Decisions

See [`architectural-decision-record.md`](architectural-decision-record.md) for module-specific ADRs. See root [`docs/architectural-decision-record.md`](../../docs/architectural-decision-record.md) for system-level decisions.
