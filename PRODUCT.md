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

Memory is organised in two tiers that align with how agents naturally access it.

**Structured memory** uses a file-based interface backed by S3 object storage. Each agent's memory is a local directory of files; agents interact with it using the same file tools they use for all other work (`read_file`, `write_file`, `edit_file`, `list_dir`). There is no separate memory API. The harness maintains a local git repository over the memory directory — every durable write is committed locally, giving the agent history, diffs, and local conflict detection. S3 is the persistence layer, not a git remote: the harness syncs changed objects individually using S3 ETag comparison (changed ETag on pull = fetch; changed mtime on push = upload). S3 object versioning is enabled on all memory buckets, providing a durable revision history without requiring git semantics at the remote.

Personal memory sync uses `ours` conflict resolution — local is authoritative, S3 is the backup. Shared team memory writes are not written to S3 directly by the bot. Instead, the bot sends a `memory_write` message to the orchestrator's SQS queue; the orchestrator applies writes to the shared bucket sequentially, eliminating concurrent write conflicts. This is consistent with the orchestrator's role as the sole writer to all shared mutable state. Shared memory reads remain direct S3 operations — only writes are serialised.

**Semantic memory** is indexed in **S3 Vectors** — Amazon S3's native vector storage. When the agent saves a memory worth indexing for fuzzy retrieval, the harness writes the embedding to S3 Vectors alongside the file write. Semantic queries return ranked file paths and content excerpts; the agent fetches full content via file tools. Semantic memory is accessed via the `memory_search` harness tool.

Each bot has a **personal S3 bucket** containing its local-git-backed memory files and its S3 Vectors index. A separate **team S3 bucket** holds the shared memory files and shared vector index accessible by all agents. IAM roles enforce access boundaries. Personal memory is private to its owner; the shared bucket is readable and writable by all bot IAM roles.

### MCP Client

Every bot is an **MCP (Model Context Protocol) client**. MCP provides the mechanism through which bots connect to external tools, data sources, and services in a standardised way.

Tool configuration is defined in `mcp.json` files loaded from S3 at startup:

| File | Location | Behaviour |
|---|---|---|
| `mcp.json` | Team shared S3 bucket | Optional. Loaded by all bots. Defines tools available across the whole team. Missing file is silently ignored, but the orchestrator logs a warning at startup. |
| `mcp.json` | Bot's private S3 bucket | Optional. Loaded only by that bot. Defines tools specific to its role. Missing file is silently ignored — not an error. |

At startup the bot attempts to load the shared file first, then the private file. The two configurations are merged, with the private file able to extend or override the shared entries. A bot can function with either, both, or neither file present. The orchestrator is responsible for reporting the absence of the shared file via a structured log warning so the team can be made aware that no shared tools are configured.

**MCP server authentication.** Each server entry in `mcp.json` may include a `credential` field with a typed credential descriptor. The harness resolves credentials before initialising MCP connections:

| Credential type | Behaviour |
|---|---|
| *(absent)* | No authentication — server is treated as unauthenticated |
| `static_secret` | Harness retrieves the value from a referenced Secrets Manager ARN using the bot's IAM role and passes it to the MCP client as configured (header, token, API key, etc.) |
| `oauth2` | Reserved for future implementation — harness will acquire and refresh tokens via the OAuth 2.1 flow using a configured token endpoint and client credentials |

The credential field is a typed union; adding `oauth2` support is a new harness credential provider and requires no changes to `mcp.json` schema or connection logic. Static secrets are the current implementation target — they cover API keys, bearer tokens, and any other long-lived credential stored in Secrets Manager and scoped to the bot's IAM role.

### Agent Harness — Built-in Tools

Every worker thread runs inside the **agent harness**, which provides a fixed set of built-in tools available to all agents regardless of MCP configuration. Built-in tools are the agent's direct interface with the system; MCP tools extend that with external services.

#### Built-in tool set

| Tool | Purpose | Safety scope |
|---|---|---|
| `read_file`, `list_dir`, `glob`, `grep` | Read files and search the filesystem | Read-only; workspace-scoped |
| `write_file`, `edit_file` | Write or patch files | Write; workspace-scoped |
| `memory_search(query)` | Semantic retrieval from S3 Vectors index | Read-only; namespaced to bot + team |
| `send_message(to, subject, body, reply_to?)` | Send a message to a named bot | Audited; subject to `receive_from` allowlist on recipient |
| `read_messages(filter)` | Read inbound messages from own queue | Read-only; scoped to own queue |
| `todo_write(tasks)`, `todo_read()` | Persistent per-bot task list | Scoped to calling bot |
| `http_request(method, url, headers, body)` | HTTP call to an allowed external host | Allowlisted hosts only; fully logged |
| `get_metrics(metric)` | Read own operational metrics | Read-only; scoped to calling bot |

All file tools operate within the bot's designated workspace and memory directory. The harness enforces this boundary at dispatch time — it is not a convention the agent can override.

#### Dynamic tool gating (Tool Attention)

Injecting every available tool schema into the model's context on every turn imposes a significant token cost. In multi-server MCP deployments, naïve eager injection can consume 10,000–60,000 tokens per turn in tool descriptions alone. The harness implements **Tool Attention**: a middleware layer that scores each available tool's description against the current task intent and injects only the top-k matching tools as full JSON schemas. All other tools are held as compact name-and-summary stubs and promoted on demand. This reduces per-turn tool token cost substantially and keeps effective context utilisation high. The hard cap on simultaneously fully-injected tools is 20.

Scoring uses **BM25** — a pure-Go, zero-dependency term-frequency ranking function. Tool descriptions and task prompts share enough vocabulary that BM25 match quality is sufficient for a closed, well-named tool set. The scorer is a single interface; it can be swapped for neural embeddings (e.g. AWS Bedrock Titan Embeddings, computed at startup for tool descriptions and per-turn for task intent) if match quality degrades as the tool set grows, without changing the gating or injection logic.

#### Agent Skills

The harness supports **Agent Skills** — modular capability packages that agents discover and load on demand. Each skill is a directory in the bot's private S3 bucket or the shared team bucket containing a `SKILL.md` (description, inputs, outputs, usage contract) and optional supporting scripts. At runtime the harness maintains a skill index — names and one-line summaries — in the agent's context. When a task matches a skill, the full `SKILL.md` is promoted into context and supporting scripts are made available as harness-executed tools. Skills are distinct from MCP tools: they are owned and versioned by the team, not external servers, and follow four-stage progressive disclosure (index → full description → supplementary files → script execution).

**Skill lifecycle.** Skills are uploaded at runtime via `baobotctl skills push`, which places the skill in a `skills/staging/` prefix in S3. A staged skill is not visible to agents. An Admin must explicitly promote it via `baobotctl skills approve`, which moves it to the active prefix and makes it discoverable. Skills can be demoted back to staging or deleted via `baobotctl skills revoke`. This gives operators fast iteration without requiring a full deploy cycle, while keeping a mandatory human approval gate before any skill becomes executable.

**Skill script isolation.** Supporting scripts are executed as restricted subprocesses: no environment variables are inherited (credentials are stripped before exec), filesystem access is limited to a temporary working directory created by the harness for that invocation, and network access is constrained by the ECS task's existing security group. The harness passes only explicitly declared inputs to the script via arguments or stdin. A buggy or malicious skill script cannot access bot credentials, read or write outside its working directory, or make network calls beyond what the security group already permits.

### Tool Safety and Permission Model

The harness enforces a layered permission model to limit the blast radius of any single agent's actions and to defend against indirect prompt injection — malicious instructions embedded in content the agent reads (files, HTTP responses, MCP tool outputs, inbound messages).

**Bot role → tool set (startup binding).** Each bot's `config.yaml` declares an `allowed_tools` list. The harness binds the tool set at startup. A tool not in the allowlist is not injected into context, not available as a schema stub, and cannot be invoked. The model has no awareness of tools it cannot use.

**Filesystem boundary enforcement.** All file tools are scoped to the bot's designated workspace and memory directory. Write operations outside these paths are rejected by the harness before reaching the filesystem.

**Allowlisted HTTP hosts.** The `http_request` tool only contacts hosts declared in the bot's configuration. Requests to unlisted hosts are rejected. All calls are logged with full request and response metadata.

**Calibrated autonomy.** Actions vary in reversibility and blast radius. The harness assigns each action a gate type rather than applying uniform approval:

| Gate type | Behaviour | Example |
|---|---|---|
| **Advisory** | Agent proceeds; operator is notified after | File reads, status queries |
| **Validating** | Agent proceeds; action is logged and reviewable | Memory writes, task updates |
| **Blocking** | Agent pauses; human must approve before proceeding | Messages to another bot that trigger an action; destructive file changes |
| **Escalating** | Agent pauses and notifies a designated approver via Slack or Teams | Any cross-bot action with an external side effect (PR creation, deployment trigger) |

Approvals are never cached — a cached approval can be replayed by a prompt-injected instruction.

**Prompt injection defence.** All tool outputs are sanitised before being returned to the model (control characters stripped, suspicious instruction patterns flagged). MCP tool outputs and inbound messages from external senders are treated as untrusted content regardless of source.

**Per-bot budget caps.** Hard limits on token spend per day and tool calls per hour are configured per bot in `config.yaml` and enforced by the harness before dispatching any tool call or model invocation. Agents can read their current usage via `get_metrics`. Cap breaches surface as metrics and alerts. Counters are maintained in memory and flushed to **DynamoDB** every 30 seconds and on graceful shutdown. On startup the harness reads the current window's totals from DynamoDB to seed the in-memory counter, ensuring caps survive process restarts. A crash loses at most one flush interval of recorded spend — acceptable given the cap windows (daily token spend, hourly tool calls). The budget table uses `(bot_id, window)` as the composite key, shared across all bots in the cluster.

**Cross-bot messaging allowlists.** Each bot's `config.yaml` declares a `receive_from` list of bot identifiers permitted to send it messages that trigger actions. Messages from unlisted senders are rejected. This limits the blast radius if a bot is compromised — it cannot be used as a pivot to route instructions to other bots.

### Communication Channels

Bots monitor and respond to messages from:

- **Slack** — inbound message monitoring with event-driven response.
- **Microsoft Teams** — equivalent channel monitoring.
- **SQS (bot-to-bot)** — each bot has a dedicated SQS inbound queue. Other bots and the orchestrator post messages directly to a target bot's queue. The bot's main thread polls this queue continuously.
- **SNS (broadcast)** — a shared SNS topic fans out to every bot's SQS queue simultaneously. Used for team-wide announcements: orchestrator presence, shutdown signals, and other broadcasts that all bots must receive.
- **Structured delegation (SQS, A2A-shaped envelope)** — bot-to-bot task delegation uses a structured message envelope whose schema is modelled on the A2A task format. Messages carry a task lifecycle: `submitted → working → input-required → completed → failed`. The delegating bot sends the envelope to the target bot's SQS queue; the receiving bot posts status updates back to the delegating bot's queue. This gives the orchestrating agent reliable progress tracking without polling, and gives the receiving agent a structured, cancellable work unit rather than a free-form message. The transport is SQS throughout — no additional HTTP listeners or ephemeral IP management required. The envelope schema is intentionally A2A-compatible so the transport can be upgraded to the A2A HTTP protocol in future without changing the data model. Each bot publishes an **Agent Card** to a well-known path in its S3 bucket. At registration time, the orchestrator fetches the card and includes it in the registration acknowledgement broadcast to the SNS topic. All running bots receive the broadcast and cache the card locally, maintaining a local registry of known teammates. Delegation lookups are served from this local cache — no round-trip required at delegation time. On startup, a bot requests a `team_snapshot` from the orchestrator as part of its startup sequence; the orchestrator replies with the current registry including all cached Agent Cards, closing the cold-start gap before the first incremental broadcast is received.

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
- Accepts bot registration messages, fetches the registering bot's Agent Card from S3, and maintains the agent registry.
- Broadcasts each registration acknowledgement (including Agent Card) to the SNS topic so all running bots update their local teammate registry.
- Responds to `team_snapshot` requests from newly started bots with the full current registry and all cached Agent Cards.
- Ensures only one instance of each agent type is active at any time.
- Monitors the Kanban board and dispatches work-assignment notifications to teammates.
- Broadcasts its presence to the SNS topic on startup.
- Accepts inbound messages from other bots via its SQS queue.
- Accepts REST API requests from authenticated users via `baobotctl` or the web UI.
- Serialises writes to the shared team memory bucket — bots send `memory_write` messages via SQS; the orchestrator applies them sequentially to eliminate concurrent write conflicts.

**Conflict detection:** On startup, an orchestrator-enabled bot publishes a presence broadcast to the SNS topic. Any running orchestrator receives this on its SQS queue and responds directly to the new instance's queue. The new instance logs an error identifying the conflict and shuts down. Only one orchestrator may be active at any time.

**Team re-registration:** Non-orchestrator bots that receive the orchestrator startup broadcast respond by sending a registration message to the orchestrator's SQS queue. This ensures that if the orchestrator restarts it automatically rebuilds the team registry from the bots that are currently running, without requiring each bot to restart itself.

**Restart durability.** In-flight work survives an orchestrator restart via SQS visibility timeouts. If the orchestrator crashes before acknowledging a message, the message reappears in the queue after the visibility timeout and is reprocessed after restart. In-flight structured delegation tasks are owned by the receiving bot and are unaffected by orchestrator restarts. Blocking approval gates that were awaiting operator input are re-queued and re-presented. All orchestrator message handlers are **idempotent** — applying the same message twice produces the same result as applying it once. Kanban mutations include a client-supplied idempotency token (a UUID generated by the sending bot) to enforce this. Duplicate processing on restart is harmless as long as handlers are correct, and at-least-once delivery with idempotent handlers is the durability guarantee the system provides.

The control plane, Kanban board, and HTTP services start and stop with orchestrator mode — none are standalone services.

### Control Plane

The control plane is the orchestrator's registry of the running team. It tracks:

- Which bots are registered and active.
- The agent type of each bot (only one instance per type is permitted).
- Bot metadata: name, type, SQS queue ARN, Agent Card, last heartbeat, status.

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

### Context Management

Long-running always-on agents accumulate state in their context window that, left unmanaged, degrades performance — a phenomenon known as **context rot**. The harness manages context as a first-class concern.

**Progressive disclosure** governs what enters context at the start of each worker thread invocation. The agent starts with its SOUL.md, its current todo list, a compact skill index (names and one-line summaries only), and the current task definition. Additional material — memory chunks, full skill descriptions, file contents — is fetched on demand via tool calls, not pre-loaded.

**Checkpoint-and-restart** is the context management strategy. When the context window approaches a configurable token threshold, the harness checkpoints all durable state — todo list, any memory writes from the current session, and the structured task state — to the git-backed memory store, then starts a fresh worker thread with a cold context reinitialised from the checkpoint. The agent resumes from a clean slate with its persistent state intact. This is provider-agnostic and works identically across Bedrock, OpenAI-compatible, and Anthropic API providers. The agent loses the in-flight conversational thread but retains everything written to memory, so the quality of the restart depends directly on how well the agent has been writing to memory during the session — which the todo list and file tools actively encourage.

**Structured handoffs** are used when work spans multiple worker threads or bot-to-bot delegations. The handoff artifact is a JSON document capturing current task state, a plain-text progress note, and a git reference to the last committed memory state. The receiving thread or bot starts from this artifact, not from a conversational transcript.

**Sub-agents as context firewall.** When a task is delegated to a peer bot via A2A, the delegating bot summarises what the sub-agent needs; the sub-agent operates in an isolated context window. Results are returned as structured output in the A2A task completion message, not as a full context replay.

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
| `baobotctl skills ...` | Skill management (Admin only: push, approve, revoke, list) |

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
| Budget counters | DynamoDB (shared table, `bot_id + window` key) | All bots | All bot IAM roles |
| Secrets | AWS Secrets Manager | Per-bot, scoped by IAM | Each bot's IAM role |

The control plane and Kanban board databases are accessible **only** from the orchestrator's ECS task. No other bot has credentials or network access to these instances. All reads and writes from other components are mediated through the orchestrator via SQS message.

### Messaging

| Resource | Type | Purpose |
|---|---|---|
| Per-bot inbound queue | SQS | Direct messages, work assignments, EventBridge events, structured delegation messages |
| Team broadcast topic | SNS (fan-out to all SQS queues) | Orchestrator presence, team-wide announcements |
| Agent Cards | S3 (well-known path in each bot's bucket) | Bot capability advertisement; served via orchestrator REST API for discovery |

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
- DynamoDB budget table (per-bot token spend and tool call counters, shared across all bots).
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

Every component emits structured logs, metrics, and traces following the **OpenTelemetry** standard. Every model invocation and tool execution generates an OTel trace span with structured attributes: `bot_id`, `session_id`, tool name, sanitised input and output, latency, and token cost where applicable. Distributed tracing spans agent worker threads end-to-end, including MCP tool calls and A2A delegation legs. Key observability surfaces:

- Worker thread lifecycle (start, completion, failure).
- Model invocation latency and token usage.
- Every tool call: tool name, input, output, latency, success or failure (built-in tools and MCP tools alike).
- Memory read/write operations and git commit events.
- SQS message ingestion, routing, and processing latency.
- A2A task lifecycle events (submitted, working, completed, failed) per delegation.
- Per-bot budget utilisation: token spend vs. daily cap, tool calls vs. hourly cap.
- Orchestrator state and Kanban board changes.
- All bot registration and deregistration events (timestamp, reason).
- All control plane and Kanban board mutations (actor, action, timestamp, before/after state).
- REST API access per request (user, endpoint, status, latency).
- Duplicate-agent-type rejections surfaced as warnings and metrics.
- Tool gate events: advisory notifications, blocking approvals requested, escalations raised.

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
| Skills library (private, optional) | Yes | — | — |
| Skills library (shared) | — | Yes | — |
| Agent Card (signed) | Yes | — | — |
| Todo list | Yes | — | — |
| Model config | Yes | — | — |
| Binary / Container image | — | Yes (shared) | — |
| SNS Broadcast Topic | — | Yes (shared) | — |
| Control plane DB | — | — | Yes |
| Kanban board DB | — | — | Yes |
| ALB + REST API | — | — | Yes |
| HTTP/HTML web UI | — | — | Yes |
| Bot registry | — | — | Yes |
