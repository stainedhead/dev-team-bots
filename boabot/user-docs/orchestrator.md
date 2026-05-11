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

## Task Scheduling

When creating a task via `POST /api/bots/:name/tasks`, include a `schedule` field to control when it runs:

- **ASAP** (default) — task is dispatched immediately when a slot is free.
- **Future** — provide `"mode": "future"` and `"run_at": "<RFC3339 timestamp>"`. The task is held until that time.
- **Recurring** — provide `"mode": "recurring"` and a `"rule"` object with `"frequency"` (`"daily"`, `"weekly"`, or `"monthly"`), and the appropriate time fields (`"time_of_day_seconds"`, `"days_mask"` for weekly, `"month_day"` for monthly). After each run the next occurrence is computed automatically.

The scheduling loop checks for due tasks every 10 seconds. If the process was restarted, any tasks that became due during downtime are dispatched immediately on startup (catch-up pass).

When using the Assign Task dialog in the web UI, a schedule builder lets you select the mode and configure recurrence options — day checkboxes, a time picker, or a natural-language text field (e.g., "every Monday at 9am").

You can also create scheduled tasks through operator chat. Type a request such as "ask Claude to run the daily report every morning at 8am" — the orchestrator detects the scheduling intent, describes what it understood, and asks for confirmation before creating the task.

## Agent Notifications

Bots can surface notifications in the orchestrator UI when they need operator attention (for example, when a task is blocked or requires a decision). Notifications appear on the Notifications tab of the kanban board.

### Lifecycle

| Status | Meaning |
|---|---|
| `unread` | Notification has not been viewed. The tab badge shows the unread count. |
| `read` | Operator has opened the discuss thread. |
| `actioned` | Operator has explicitly marked the notification actioned. |

### Discuss Thread

Each notification has a discuss thread (capped at 100 entries). You can post messages to the bot directly from the notification detail panel. The bot can reply, giving you a back-and-forth channel on the specific notification without leaving the UI.

### Requeue

The Requeue button re-submits the originating task to the bot at ASAP priority, with the full discuss thread prepended as context. Use this to give the bot updated instructions or clarification after a discussion.

### Bulk Delete

Select one or more notifications and use the Delete button to remove them. This is a permanent action.

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
