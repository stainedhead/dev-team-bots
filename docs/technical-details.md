# Technical Details — BaoBot Dev Team

This document describes the system-level architecture. For module-specific technical details see the `docs/technical-details.md` within each module directory.

## Technology Stack

| Concern | Technology |
|---|---|
| Language | Go 1.26 |
| Container runtime | AWS ECS (Fargate) |
| Container registry | Amazon ECR |
| Messaging | Amazon SQS (per-bot), Amazon SNS (broadcast) |
| Memory — vector | Amazon S3 Vectors |
| Memory — structured | Amazon S3 Files |
| Databases | Amazon RDS MariaDB (x2) |
| Model inference | AWS Bedrock, OpenAI-compatible endpoints (incl. Ollama) |
| Tool integration | MCP (Model Context Protocol) |
| Infrastructure as Code | AWS CDK |
| CI/CD | GitHub Actions |
| Load balancing | AWS ALB |
| Secrets | AWS Secrets Manager |
| Event scheduling | AWS EventBridge |
| Authentication | JWT (username/password, HS256) |

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
Bot A ──SQS──► Orchestrator queue   (registration, board updates, queries)
Orchestrator ──SQS──► Bot A queue   (work assignments, notifications)
Any bot ──SNS──► All bot queues     (orchestrator startup, shutdown broadcast)
EventBridge ──► Target bot SQS queue (scheduled and reactive events)
```

## Clean Architecture Layers

```
domain/         — interfaces, entities, value objects (no external imports)
application/    — use cases orchestrating domain logic
infrastructure/ — adapters: S3, SQS, SNS, RDS, Bedrock, Slack, Teams, HTTP
cmd/            — wiring: instantiate infrastructure, inject into application
```

## CDK Stack Dependency

```
boabot/cdk (shared stack)
  └── boabot-team/cdk (per-bot stack, imports shared ARNs via cross-stack ref)
```

Shared stack must be deployed before the per-bot stack.

## Bot Lifecycle

1. Start → load config, SOUL.md, mcp.json (shared + optional private)
2. Register → post registration message to orchestrator SQS queue
3. Run → poll SQS queue on main thread; spawn worker threads for tasks
4. Heartbeat → periodic liveness message to orchestrator
5. Shutdown → publish shutdown broadcast to SNS, drain workers, exit

## Orchestrator Startup Sequence

1. Start → publish presence broadcast to SNS
2. Conflict check → if another orchestrator responds, log error and exit
3. Start control plane, Kanban board, HTTP server
4. Receive re-registration messages from running bots (triggered by broadcast)
5. Begin normal operation
