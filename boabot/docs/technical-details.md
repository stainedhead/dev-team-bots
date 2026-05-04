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
    message.go          # message types: registration, heartbeat, task, broadcast, shutdown
    provider.go         # ModelProvider and ProviderFactory interfaces
    mcp.go              # MCPClient interface
    queue.go            # MessageQueue and Broadcaster interfaces
    orchestrator.go     # ControlPlane and BoardStore interfaces (orchestrator mode)
    user.go             # User, Role, and Auth interfaces (orchestrator mode)
    mocks/              # generated/hand-written mocks for all interfaces

  application/
    run_agent.go        # top-level agent loop use case
    process_message.go  # message routing and dispatch
    execute_task.go     # worker thread execution harness
    register.go         # bot registration and heartbeat use cases
    memory_ops.go       # read, write, search memory use cases
    orchestrator/       # orchestrator-mode use cases (board, control plane, auth)

  infrastructure/
    aws/
      sqs/              # SQS MessageQueue adapter
      sns/              # SNS Broadcaster adapter
      s3/               # S3 Files MemoryStore adapter
      s3vectors/        # S3 Vectors MemoryStore adapter
      bedrock/          # Bedrock ModelProvider adapter
      secrets/          # Secrets Manager loader
    mcp/                # MCP client adapter
    openai/             # OpenAI-compatible ModelProvider adapter
    slack/              # Slack channel monitor adapter
    teams/              # Microsoft Teams adapter
    http/               # REST API server and web UI handler (orchestrator mode)
    db/                 # MariaDB adapters for control plane and board (orchestrator mode)
    config/             # config file loading (YAML), SOUL.md and mcp.json loading from S3
```

## Threading Model

```
main goroutine
  └── Agent.Start()
        ├── SQS poll loop (main thread)
        ├── Slack monitor goroutine
        ├── Teams monitor goroutine
        └── Worker pool
              └── worker goroutine (per task, recover() guards panic)
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
```

## Key Design Decisions

See [`architectural-decision-record.md`](architectural-decision-record.md) for module-specific ADRs. See root [`docs/architectural-decision-record.md`](../../docs/architectural-decision-record.md) for system-level decisions.
