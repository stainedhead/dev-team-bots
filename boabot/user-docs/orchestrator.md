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
- Control plane service (team registry, bot registration handler).
- Kanban board service (work item management).
- REST API server on `api_port`.
- Web UI server on `web_port`.

All of these stop when the bot stops.

## Conflict Detection

On startup, the orchestrator broadcasts its presence to the SNS topic. If another orchestrator is already running, it responds and the new instance exits with an error. Check logs for the message `orchestrator conflict detected`.

## First Admin User

On first startup with an empty database, the orchestrator creates a bootstrap Admin account. Credentials are logged once and only once — capture them immediately. Use `baobotctl user set-pwd` to change the password after first login.

## Monitoring

The orchestrator logs:
- All bot registrations and deregistrations.
- All control plane and board mutations (actor, action, timestamp).
- All REST API requests (user, endpoint, status, latency).
- Missing shared `mcp.json` on startup (warning).
- Heartbeat timeouts resulting in bot deregistration.
