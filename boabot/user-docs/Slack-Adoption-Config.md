# Slack — Adoption & Configuration

boabot connects to Slack via Socket Mode, which maintains a persistent WebSocket connection. No public inbound URL or webhook is required — the bot initiates the connection outward.

## What's supported

- **Direct messages** — users DM the bot directly; the bot replies in the same conversation
- **Channel @mentions** — users `@mention` the bot in any channel it has been invited to; the bot replies in the thread

The bot does not read every message in a channel — only events where it is explicitly mentioned. This minimises scope and required permissions.

---

## Step 1 — Create a Slack app

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and click **Create New App → From scratch**.
2. Name the app (e.g. `boabot`) and select your workspace.

---

## Step 2 — Configure OAuth scopes

Under **OAuth & Permissions → Bot Token Scopes**, add:

| Scope | Purpose |
|---|---|
| `chat:write` | Post messages and replies |
| `im:history` | Read direct messages sent to the bot |
| `app_mentions:read` | Receive events when the bot is @mentioned |

Install the app to your workspace (**OAuth & Permissions → Install to Workspace**) and copy the **Bot User OAuth Token** (`xoxb-...`).

---

## Step 3 — Enable Socket Mode

1. Under **Socket Mode**, toggle **Enable Socket Mode** on.
2. Click **Generate an app-level token**, name it (e.g. `boabot-socket`), grant the `connections:write` scope, and copy the token (`xapp-...`).

---

## Step 4 — Subscribe to events

Under **Event Subscriptions**, enable events and subscribe to:

| Event | Trigger |
|---|---|
| `message.im` | Direct messages to the bot |
| `app_mention` | @mentions in channels |

---

## Step 5 — Invite the bot to channels

For @mention support in a channel, invite the bot:

```
/invite @boabot
```

The bot only needs to be invited to channels where you want it to respond to mentions.

---

## Step 6 — Configure boabot

Add to `config.yaml`:

```yaml
slack:
  bot_token: "xoxb-..."      # Bot User OAuth Token (from OAuth & Permissions)
  app_token: "xapp-..."      # App-Level Token (from Socket Mode)
  bot_name: "tech-lead"      # Name of the boabot bot that handles Slack messages
```

`bot_name` must match the `name` field of an enabled bot in `team.yaml`. Slack messages are dispatched as tasks to that bot.

**Keep tokens out of version control.** Store them in the credentials file (`~/.boabot/credentials.yaml`) or inject them as environment variables and reference them via your secrets manager.

---

## Credentials file alternative

Instead of hardcoding tokens in `config.yaml`, store them in the credentials file:

```yaml
# ~/.boabot/credentials.yaml  (chmod 600)
slack_bot_token: "xoxb-..."
slack_app_token: "xapp-..."
```

The boabot credentials loader picks these up automatically and applies them at startup.

---

## Behaviour notes

- **Loop prevention**: messages sent by the bot itself (`BotID` set or `SubType: bot_message`) are silently dropped to prevent reply loops.
- **Threading**: @mention replies are posted into the original message thread. DM replies go into the DM conversation.
- **Task result routing**: when the bot finishes processing a Slack message, the result is posted back to Slack automatically. The same task also appears in the boabot web UI chat history (orchestrator mode).
- **One bot per Slack app**: the `bot_name` field routes all Slack messages to a single named bot. If you need different bots to handle different Slack channels, use separate Slack apps and boabot instances.
