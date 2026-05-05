# Technical Details — BaoBot Dev Team

This document describes the system-level architecture. For module-specific technical details see the `docs/technical-details.md` within each module directory.

## Technology Stack

| Concern | Technology |
|---|---|
| Language | Go 1.26 |
| Container runtime | AWS ECS (Fargate) |
| Container registry | Amazon ECR |
| Messaging | Amazon SQS (per-bot), Amazon SNS (broadcast) |
| Structured delegation | SQS with A2A-shaped envelope |
| Agent Card storage | Amazon S3 (well-known path per bot) |
| Memory — structured | Local git + S3 object sync (ETag-based) |
| Memory — semantic | Amazon S3 Vectors |
| Budget counters | Amazon DynamoDB |
| Databases | Amazon RDS MariaDB (x2) |
| Model inference | AWS Bedrock, OpenAI-compatible endpoints (incl. Ollama) |
| Tool integration | MCP (Model Context Protocol) |
| Tool gating | BM25 scoring (Tool Attention, 20-tool cap) |
| Infrastructure as Code | AWS CDK |
| CI/CD | GitHub Actions |
| Load balancing | AWS ALB |
| Secrets | AWS Secrets Manager |
| Event scheduling | AWS EventBridge |
| Authentication | JWT (username/password, HS256) |
| Observability | OpenTelemetry (traces, metrics, logs) |

## System Architecture

```
┌─────────────────────────────────────────────────────┐
│                    AWS ECS Cluster                   │
│                                                      │
│  ┌─────────────┐  ┌──────────┐  ┌────────────────┐  │
│  │Orchestrator │  │Architect │  │  Implementer   │  │
│  │  + Control  │  │          │  │                │  │
│  │    Plane    │  └──────────┘  └────────────────┘  │
│  │  + Kanban   │                                     │
│  └──────┬──────┘                                     │
│         │                                            │
└─────────┼────────────────────────────────────────────┘
          │
     ┌────▼─────┐     ┌──────────────┐
     │   ALB    │     │  SNS Topic   │──► All bot SQS queues
     └────┬─────┘     └──────────────┘
          │
    /api/* │ /*
          │
   ┌──────▼──────┐
   │ baobotctl / │
   │  Browser    │
   └─────────────┘
```

## Messaging Topology

```
Bot A ──SQS──► Orchestrator queue     (registration, board updates, shared memory writes)
Orchestrator ──SQS──► Bot A queue     (work assignments, notifications, team_snapshot reply)
Any bot ──SNS──► All bot queues       (orchestrator startup, shutdown broadcast, Agent Card distribution)
Bot A ──SQS──► Bot B queue            (structured delegation: A2A-shaped task envelope)
Bot B ──SQS──► Bot A queue            (delegation status updates: working, completed, failed)
EventBridge ──► Target bot SQS queue  (scheduled and reactive events)
```

## Clean Architecture Layers

```
domain/         — interfaces, entities, value objects (no external imports)
application/    — use cases orchestrating domain logic
infrastructure/ — adapters: S3, SQS, SNS, RDS, Bedrock, Slack, Teams, HTTP, DynamoDB
cmd/            — wiring: instantiate infrastructure, inject into application
```

## CDK Stack Dependency

```
boabot/cdk (shared stack)
  └── boabot-team/cdk (per-bot stack, imports shared ARNs via cross-stack ref)
```

Shared stack must be deployed before the per-bot stack.

## Bot Lifecycle

1. Start → load config, SOUL.md, mcp.json (shared + optional private), seed budget counters from DynamoDB
2. Request `team_snapshot` from orchestrator → populate local Agent Card cache
3. Publish own Agent Card to S3, send registration message to orchestrator SQS queue
4. Receive registration acknowledgement + Agent Card broadcast via SNS
5. Run → poll SQS queue on main thread; spawn worker threads for tasks
6. Heartbeat → periodic liveness message to orchestrator
7. Shutdown → checkpoint active worker state to memory, publish shutdown broadcast to SNS, flush budget counters to DynamoDB, drain workers, exit

## Orchestrator Startup Sequence

1. Start → publish presence broadcast to SNS
2. Conflict check → if another orchestrator responds, log error and exit
3. Start control plane, Kanban board, HTTP server
4. Receive re-registration messages from running bots (triggered by broadcast); fetch each bot's Agent Card from S3
5. Respond to `team_snapshot` requests from newly started bots
6. Begin normal operation

## Worker Thread Context Lifecycle

```
Receive task
  └── Build initial context: SOUL.md + todo list + skill index (stubs) + task
  └── Execute (Tool Attention gates schema injection, BM25 scores tools)
  └── On context threshold → checkpoint to memory → restart worker from checkpoint
  └── On completion → write result, update todo list, flush memory
```
