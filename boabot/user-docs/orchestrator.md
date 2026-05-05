# Running in Orchestrator Mode — boabot

The orchestrator is the same bot binary with additional services enabled via config. Only one orchestrator may run in the cluster at a time.

## Enabling Orchestrator Mode

In `config.yaml`:

```yaml
orchestrator:
  enabled: true
  api_port: 8080
  web_port: 8081
```

In `boabot-team/team.yaml`, set `orchestrator: true` on the bot entry.

## What Starts

When `orchestrator.enabled: true`, the bot additionally starts:
- Control plane service (team registry in RDS MariaDB, bot registration handler).
- Kanban board service (work item management in RDS MariaDB).
- REST API server on `api_port` (`/api/*`, JWT-authenticated).
- Web UI server on `web_port` (`/*`, HTML Kanban board).
- Shared memory write serialiser (applies `memory_write` SQS messages to the team S3 bucket sequentially).
- `team_snapshot` responder (replies to newly started bots with the full current registry and all cached Agent Cards).
- Agent Card distributor (fetches each registering bot's Agent Card from S3 and broadcasts it via SNS).

All of these stop when the bot stops.

## Conflict Detection

On startup, the orchestrator broadcasts its presence to the SNS topic. If another orchestrator is already running, it responds and the new instance exits with an error. Check logs for the message `orchestrator conflict detected`.

## Restart Durability

All message handlers in the orchestrator are idempotent. SQS visibility timeouts re-deliver messages if the orchestrator crashes before acknowledging them — no message is lost. Kanban board mutations require a client-supplied idempotency token (UUID) so that retried messages do not create duplicate state changes.

## First Admin User

On first startup with an empty database, the orchestrator creates a bootstrap Admin account. Credentials are logged once and only once — capture them immediately. Use `baobotctl user set-pwd` to change the password after first login.

## Monitoring

The orchestrator logs:
- All bot registrations and deregistrations.
- All Agent Card fetches and SNS broadcasts.
- All `team_snapshot` requests served.
- All shared memory writes applied.
- All control plane and board mutations (actor, action, timestamp).
- All REST API requests (user, endpoint, status, latency).
- Missing shared `mcp.json` on startup (warning).
- Heartbeat timeouts resulting in bot deregistration.
