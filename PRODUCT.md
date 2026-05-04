# BaoBot Dev Team

## Vision

A team of cooperative, always-on AI agents that function as a software development team. Each agent carries a specialized role, its own evolving memory, and the ability to communicate with teammates — forming a self-coordinating unit capable of responding to commands, reacting to events, and completing complex work autonomously.

"Bao" (เบา) is the Thai word for servant or helper, reflecting the cooperative and assistive nature of the team.

---

## Repository Structure

This project is a monorepo containing three top-level modules:

```
dev-team-bots/
├── boabot/          # Agent runtime — the BaoBot binary and shared infrastructure
├── boabotctl/       # CLI application for operators and users
└── boabot-team/     # Bot personalities and per-bot infrastructure
```

### boabot/

The core agent runtime. All bots run the same compiled binary, differentiated at startup by their injected configuration and SOUL.md. This module is containerized and pushed to Amazon ECR on each release.

Also contains the CDK stack for **shared infrastructure** — resources that are cluster-wide and not specific to any single bot:

- ECS cluster
- Application Load Balancer (ALB)
- Shared team S3 bucket
- RDS MariaDB instances (control plane DB, Kanban board DB)
- SNS broadcast topic (team-wide messaging)
- ECR repository
- VPC, subnets, and security group baselines
- EventBridge rules for shared events

### boabotctl/

The operator CLI. A standalone Go binary that communicates with the orchestrator's REST API via the ALB. Built and distributed separately from the agent runtime — users install it locally, not in the cluster. Releases are published as pre-built binaries for each supported platform via GitHub Releases.

### boabot-team/

Defines the team: which bots exist, what their personalities are, and what bot-specific infrastructure they require. Contains:

- A **team configuration file** (`team.yaml`) that declares each bot by name and type, and controls which bots are deployed to the cluster.
- Per-bot directories, each containing the bot's `SOUL.md`, `AGENTS.md`, role configuration, and any bot-specific assets.
- CDK stacks for **per-bot infrastructure** — driven by the team configuration file, iterating over declared bots to provision:
  - Private S3 memory bucket
  - IAM role and policies
  - ECS task definition and service
  - SQS inbound message queue
  - Secrets Manager entries

The CDK scripts in `boabot-team/` read `team.yaml` and reconcile the cluster state: deploying new bots, updating existing ones, and (when configured) tearing down removed ones.

---

## Core Concepts

### Agents

Every member of the team is an instance of **BaoBot**, a long-running agent implemented in Go 26.x. While all bots share the same binary and container image, each one is given a unique identity and role through:

- **SOUL.md** — a customized system prompt that defines the bot's role, personality, and behavioral boundaries.
- **AGENTS.md** — the bot's public interface document. Describes what it does, what inputs it accepts, what it needs to do its job well, and what it will not do. Read by teammates and human operators to know how to interact with it.
- **Private memory store** — a personal second-brain that allows the bot to accumulate knowledge and improve over time.
- **Shared team memory** — a collective knowledge base that the whole team reads and writes, capturing project context, conventions, and shared learnings.

### Memory Architecture

Memory is hosted on Amazon S3 using two native S3 access mechanisms:

- **S3 Vectors** — Amazon S3's native vector storage feature. Used for semantic search and retrieval-augmented generation. Bots write embeddings to their S3 Vectors store and query it for fuzzy, context-aware memory retrieval.
- **S3 Files (filesystem access)** — standard S3 object storage accessed via the S3 filesystem API. Used for structured, direct-access storage: notes, documents, state, and any memory that is retrieved by key rather than by semantic similarity.

Each bot has a **personal S3 bucket** containing both its S3 Vectors store and its S3 Files store. A separate **team S3 bucket** holds shared memory accessible by all agents, structured the same way. IAM roles provide each bot with a unique AWS identity, enforcing access boundaries between personal and shared stores.

The write/read pattern is:
- Writes go to S3 Files (durable, direct).
- Semantic retrieval queries S3 Vectors.

### MCP Client

Every bot is an **MCP (Model Context Protocol) client**. MCP provides the mechanism through which bots connect to external tools, data sources, and services in a standardised way.

Tool configuration is defined in `mcp.json` files loaded from S3 at startup:

| File | Location | Behaviour |
|---|---|---|
| `mcp.json` | Team shared S3 bucket | Optional. Loaded by all bots. Defines tools available across the whole team. Missing file is silently ignored, but the orchestrator logs a warning at startup. |
| `mcp.json` | Bot's private S3 bucket | Optional. Loaded only by that bot. Defines tools specific to its role. Missing file is silently ignored — not an error. |

At startup the bot attempts to load the shared file first, then the private file. The two configurations are merged, with the private file able to extend or override the shared entries. A bot can function with either, both, or neither file present. The orchestrator is responsible for reporting the absence of the shared file via a structured log warning so the team can be made aware that no shared tools are configured.

### Communication Channels

Bots monitor and respond to messages from:

- **Slack** — inbound message monitoring with event-driven response.
- **Microsoft Teams** — equivalent channel monitoring.
- **SQS (bot-to-bot)** — each bot has a dedicated SQS inbound queue. Other bots and the orchestrator post messages directly to a target bot's queue. The bot's main thread polls this queue continuously.
- **SNS (broadcast)** — a shared SNS topic fans out to every bot's SQS queue simultaneously. Used for team-wide announcements: orchestrator presence, shutdown signals, and other broadcasts that all bots must receive.

All SQS queue ARNs and the SNS topic ARN are injected into each bot's environment at deploy time by CDK, so no runtime discovery is required.

---

## Architecture

### Threading Model

Each bot runs a **main thread** that monitors incoming messages from all configured channels (Slack, Teams, SQS queue) and routes them to worker threads. **Worker threads** are agent harness instances that:

- Execute dispatched commands.
- Handle triggered events (EventBridge timers, S3 drop events, and other configured event types).
- Provide the agent with access to tools and skills to solve problems agentically.

Each worker thread is a self-contained execution context: it receives a task, reasons over it using the configured model, invokes tools or skills as needed, and reports a result.

**EventBridge events** are routed to each bot's SQS queue by EventBridge rules. The main thread processes them alongside A2A messages, distinguished by message type. No additional ingestion infrastructure is required.

### Orchestrator Mode

One bot in the cluster may be designated as the **orchestrator**. This role is enabled via a configuration flag. The orchestrator is the single authoritative source of control for the team — it owns the control plane, the Kanban board, and is the sole writer to both backing databases.

When orchestrator mode is on, the bot additionally:

- Starts the **control plane** and **Kanban board** services (HTTP server, REST API, database connections).
- Accepts bot registration messages and maintains the agent registry.
- Ensures only one instance of each agent type is active at any time.
- Monitors the Kanban board and dispatches work-assignment notifications to teammates.
- Broadcasts its presence to the SNS topic on startup.
- Accepts inbound messages from other bots via its SQS queue.
- Accepts REST API requests from authenticated users via `baobotctl` or the web UI.

**Conflict detection:** On startup, an orchestrator-enabled bot publishes a presence broadcast to the SNS topic. Any running orchestrator receives this on its SQS queue and responds directly to the new instance's queue. The new instance logs an error identifying the conflict and shuts down. Only one orchestrator may be active at any time.

**Team re-registration:** Non-orchestrator bots that receive the orchestrator startup broadcast respond by sending a registration message to the orchestrator's SQS queue. This ensures that if the orchestrator restarts it automatically rebuilds the team registry from the bots that are currently running, without requiring each bot to restart itself.

The control plane, Kanban board, and HTTP services start and stop with orchestrator mode — none are standalone services.

### Control Plane

The control plane is the orchestrator's registry of the running team. It tracks:

- Which bots are registered and active.
- The agent type of each bot (only one instance per type is permitted).
- Bot metadata: name, type, SQS queue ARN, last heartbeat, status.

**Bot registration:** When any bot starts, it posts a registration message to the orchestrator's SQS queue. The orchestrator records the registration in the control plane database. If a bot of the same type is already registered as active, the orchestrator rejects the registration and notifies the new bot via its SQS queue, which logs the error and shuts down. Bots also re-register in response to an orchestrator startup broadcast, allowing the orchestrator to rebuild its registry after a restart.

**Bot shutdown:** When any bot shuts down gracefully, it publishes a shutdown broadcast to the SNS topic before exiting. The orchestrator receives this and deregisters the bot from the control plane. Other bots receive it and can update their local awareness of available teammates. Ungraceful shutdowns (crash, OOM kill) are handled by heartbeat timeout — the orchestrator marks a bot as inactive when its heartbeat lapses past a configurable threshold.

**Database access pattern:** All writes to the control plane and Kanban board databases are performed exclusively by the orchestrator. Other bots have no database credentials and no direct database access. They interact with these stores only by sending messages to the orchestrator's SQS queue, which validates and applies the change. This keeps the database boundary clean and all mutations auditable through a single actor.

### Work Queue

The Kanban board is the team's work management system, backed by a dedicated RDS MariaDB instance. It supports:

- Creating, assigning, and updating work items.
- Tracking item state: backlog, in-progress, blocked, done.
- Notifying the assigned bot when work lands in its queue.
- Providing visibility across the team into overall workload and progress.

**Web UI:** The orchestrator serves an HTTP/HTML Kanban board accessible to human users via browser. The ALB routes `/*` traffic to the web UI port. Access requires an authenticated session. The board provides read/write access to work items appropriate to the authenticated user's role.

**Database access pattern:** Same as the control plane — all board mutations are routed through the orchestrator. No bot or user writes to the board database directly.

---

## User Access

### Network Topology

The orchestrator's ECS task is not publicly reachable. All external traffic routes through an **Application Load Balancer (ALB)**:

- `/api/*` → orchestrator REST API port
- `/*` → orchestrator web UI port

The orchestrator's ECS security group permits inbound traffic only from the ALB. The ALB handles TLS termination and provides a stable DNS endpoint for both `baobotctl` and browser access.

### Authentication

All user-facing access uses **username and password authentication with JWT sessions**.

- On account creation, an Admin sets a temporary password delivered to the user out-of-band.
- On first login (via `baobotctl login` or the web UI), the user is required to change their password.
- A signed JWT is issued on successful login, with a configurable expiry.
- `baobotctl` stores the JWT locally and attaches it to all subsequent requests.
- The web UI stores the JWT as a session cookie.
- The same JWT works for both surfaces — there is one credential per user.

### REST API

The orchestrator exposes a **REST API** accessible via the ALB at `/api/*`. It is the single access point for all human-initiated management operations. Unauthenticated requests are rejected.

The API covers:

- Control plane queries: list bots, inspect agent status, view team health.
- Kanban board management: create, update, assign, and close work items.
- User management (Admin only): add, remove, disable users; reset passwords.
- Profile management (all users): update own display name and password.

### baobotctl

`baobotctl` is the command-line interface for human operators. It follows the `kubectl` pattern: a single binary with subcommands that communicate with the orchestrator REST API via the ALB.

Example command groups:

| Group | Purpose |
|---|---|
| `baobotctl login / logout` | Session management |
| `baobotctl board ...` | Kanban board operations (list, create, assign, close items) |
| `baobotctl team ...` | Control plane queries (list bots, inspect status) |
| `baobotctl user ...` | User management (Admin only: add, remove, disable, set-pwd, set-role) |
| `baobotctl profile ...` | Own profile and password update |

### User Roles

The system has two user types:

| Role | Capabilities |
|---|---|
| **User** | View board, manage work items, update own profile and password |
| **Admin** | All User capabilities, plus: add users, remove users, disable users, reset any user's password, change user roles |

Users cannot elevate their own role. Only an Admin can change a user's type via `baobotctl user set-role`.

---

## Model Providers

Bots invoke language models through a **provider interface** that abstracts over multiple backends. Two provider types are supported:

| Provider | Description |
|---|---|
| **AWS Bedrock** | Invokes models via `InvokeModel`. Supports any Bedrock-hosted model. |
| **OpenAI-compatible** | Calls OpenAI API endpoints, including locally-hosted Ollama instances. |

### Configuration

Model providers are defined in per-bot configuration as named entries, each specifying the provider type, endpoint (where applicable), model ID, and any provider-specific parameters.

A **provider factory** initializes, caches, and exposes providers by name at runtime. Each bot instance has its own provider factory, initialized from its own configuration. Consumers reference providers by name, with no awareness of the underlying type.

### Secrets

API keys and other provider credentials are stored in **AWS Secrets Manager**. The agent retrieves secrets at startup via its IAM role; secrets are never stored in configuration files or environment variables directly.

---

## Infrastructure

### Compute

All bots run on **AWS ECS** within a single cluster. Each bot is a distinct ECS service and task definition, identified by name and assigned a unique IAM role. All bots run the same shared container image, with role-specific differentiation applied at runtime via injected environment configuration (SOUL.md path, config file path, SQS queue ARN, etc.).

### Storage

| Store | Type | Scope | Access |
|---|---|---|---|
| Personal memory (vectors) | S3 Vectors (per bot bucket) | Private to each bot | Bot's IAM role |
| Personal memory (files) | S3 Files (per bot bucket) | Private to each bot | Bot's IAM role |
| Team memory (vectors) | S3 Vectors (shared bucket) | All bots | All bot IAM roles |
| Team memory (files) | S3 Files (shared bucket) | All bots | All bot IAM roles |
| Control plane DB | RDS MariaDB | Orchestrator only | Orchestrator IAM role |
| Kanban board DB | RDS MariaDB | Orchestrator only | Orchestrator IAM role |
| Secrets | AWS Secrets Manager | Per-bot, scoped by IAM | Each bot's IAM role |

The control plane and Kanban board databases are accessible **only** from the orchestrator's ECS task. No other bot has credentials or network access to these instances. All reads and writes from other components are mediated through the orchestrator via SQS message.

### Messaging

| Resource | Type | Purpose |
|---|---|---|
| Per-bot inbound queue | SQS | Direct messages, work assignments, EventBridge events |
| Team broadcast topic | SNS (fan-out to all SQS queues) | Orchestrator presence, team-wide announcements |

### Events

**AWS EventBridge** is used to trigger scheduled and reactive events:

- Timer-based scheduled tasks (cron-style triggers).
- Reactive events such as S3 object creation (file drop) and other AWS service events.

EventBridge rules target each bot's SQS queue directly. The bot's main thread processes EventBridge events alongside other inbound messages, distinguished by message type.

### Infrastructure as Code

All AWS resources are defined and managed using **AWS CDK**, split across two modules:

**boabot/cdk — shared infrastructure stack**

- VPC, subnets, and baseline security groups.
- ECS cluster.
- ECR repository.
- Application Load Balancer (ALB) with listener rules for `/api/*` and `/*`.
- Shared team S3 bucket (S3 Vectors + S3 Files).
- SNS broadcast topic.
- RDS MariaDB instances (control plane DB, Kanban board DB), VPC-isolated with security groups permitting only the orchestrator task.
- EventBridge rules for shared/cluster-wide events.

**boabot-team/cdk — per-bot infrastructure stack**

Driven by `team.yaml`. For each declared bot:

- Private S3 memory bucket (S3 Vectors + S3 Files).
- SQS inbound queue with dead-letter queue.
- IAM role and least-privilege policy set.
- ECS task definition (referencing the shared ECR image) and ECS service.
- Secrets Manager entries scoped to that bot's IAM role.
- EventBridge rules targeting the bot's SQS queue where configured.

The team CDK stack imports shared resource ARNs from the boabot stack via CDK cross-stack references, ensuring the shared stack is always deployed first.

---

## CI/CD

Three GitHub Actions workflows manage delivery, all defined at `.github/workflows/` in the repository root. Each workflow uses `paths:` filters to trigger only when files in its module directory change.

### boabot pipeline (agent runtime)

1. **Test** — unit and integration tests on every push and pull request.
2. **Build** — Go binary compilation and validation.
3. **Containerize** — Docker image build and push to Amazon ECR.
4. **Deploy** — CDK deploy to the target ECS cluster.

Pipeline stages are gated: a failing test stage blocks containerization; a failing build blocks deployment.

### boabotctl pipeline (CLI)

1. **Test** — unit tests on every push and pull request.
2. **Build** — cross-compile Go binaries for each supported platform (macOS arm64, macOS amd64, Linux amd64).
3. **Release** — on tag `boabotctl/v*`, binaries are attached to a GitHub Release for user download.

### boabot-team pipeline (CDK)

1. **CDK Test** — CDK assertion tests on every push and pull request.
2. **CDK Diff** — on pull requests, posts the infrastructure diff as a PR comment.
3. **CDK Deploy** — on merge to main, deploys the per-bot infrastructure stack.

---

## Design Principles

### Clean Architecture

The codebase enforces a strict boundary between domain logic and infrastructure concerns. Core agent behavior, memory access, tool execution, and model invocation are all defined as interfaces. Infrastructure implementations (S3, Bedrock, SQS, Slack, etc.) depend inward on those interfaces, never the reverse. This keeps the core logic testable and portable.

### Test-Driven Development

All development follows the red-green-refactor cycle. No production code is written without a failing test first. Integration tests are included where behavior spans infrastructure boundaries; mocks are used only at defined adapter seams.

### Observability

Every component emits structured logs, metrics, and traces. Distributed tracing spans agent worker threads end-to-end. Key observability surfaces:

- Worker thread lifecycle (start, completion, failure).
- Model invocation latency and token usage.
- Memory read/write operations.
- SQS message ingestion, routing, and processing latency.
- Orchestrator state and Kanban board changes.
- All bot registration and deregistration events (timestamp, reason).
- All control plane and Kanban board mutations (actor, action, timestamp, before/after state).
- REST API access per request (user, endpoint, status, latency).
- Duplicate-agent-type rejections surfaced as warnings and metrics.

### Resiliency

- Worker threads fail in isolation; a panicking worker does not bring down the main thread.
- Model provider calls use retry logic with exponential backoff and circuit breaking.
- SQS dead-letter queues capture messages that repeatedly fail processing.
- Orchestrator conflict detection prevents split-brain coordination.
- Graceful shutdown on SIGTERM drains in-flight workers, publishes a shutdown broadcast to the SNS topic, then exits.
- Ungraceful shutdowns are detected by the orchestrator via heartbeat timeout, which triggers deregistration and a team notification.

---

## Bot Identity Summary

| Component | Per-Bot | Shared | Orchestrator only |
|---|---|---|---|
| ECS Task Definition | Yes | — | — |
| IAM Role | Yes | — | — |
| S3 Memory Bucket | Yes | Team bucket shared | — |
| SQS Inbound Queue | Yes | — | — |
| SOUL.md | Yes | — | — |
| AGENTS.md | Yes | — | — |
| mcp.json (private, optional) | Yes | — | — |
| mcp.json (shared) | — | Yes | — |
| Model config | Yes | — | — |
| Binary / Container image | — | Yes (shared) | — |
| SNS Broadcast Topic | — | Yes (shared) | — |
| Control plane DB | — | — | Yes |
| Kanban board DB | — | — | Yes |
| ALB + REST API | — | — | Yes |
| HTTP/HTML web UI | — | — | Yes |
| Bot registry | — | — | Yes |
