# Microsoft Teams — Adoption & Configuration

> **Status: planned — not yet implemented.**
>
> The `domain.ChannelMonitor` interface is in place and the team manager is designed to accept Teams adapters alongside Slack. The Teams adapter is on the roadmap. This document describes the intended configuration so you can plan your Azure setup in advance.

---

## Planned support

- **Direct messages** — users DM the bot via the Teams chat interface
- **Channel @mentions** — users `@mention` the bot in a Teams channel or team it has been added to

---

## Prerequisites

- An Azure subscription
- Permission to register an app in Azure Active Directory
- Permission to install apps in your Microsoft Teams tenant (Teams admin or user-level sideloading enabled)

---

## Step 1 — Register an Azure Bot

1. In the [Azure portal](https://portal.azure.com), search for **Azure Bot** and create a new resource.
2. Choose **Multi-tenant** unless your deployment is single-tenant.
3. Note the **Microsoft App ID** and generate a **Client Secret** under **Configuration → Manage Password**.
4. Under **Channels**, add the **Microsoft Teams** channel.

---

## Step 2 — Configure the messaging endpoint

The Azure Bot Service delivers events to a webhook endpoint on the bot. boabot will expose this under the orchestrator API when the Teams adapter is implemented:

```
https://<your-host>/api/teams/messages
```

Set this as the **Messaging endpoint** in the Azure Bot resource.

---

## Step 3 — Grant API permissions

In Azure Active Directory → App registrations → your bot app:

- `TeamsActivity.Send` — post messages
- `ChatMessage.Read.All` — read DMs (or `Chat.ReadBasic` for lighter scope)

Grant admin consent for your tenant.

---

## Step 4 — Install the bot in Teams

Create an app manifest (or use Teams Toolkit) referencing your Azure Bot App ID, and sideload or publish it via the Teams Admin Center.

---

## Planned `config.yaml` shape

```yaml
teams:
  app_id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"   # Azure Bot App ID
  app_secret: "..."                                  # Azure App Client Secret
  bot_name: "tech-lead"                              # boabot bot to dispatch messages to
  tenant_id: ""                                      # optional; omit for multi-tenant
```

---

## Interim alternative

Until the Teams adapter is available, consider using the boabot REST API or web UI to interact with bots directly, or use the Slack integration if your team uses both platforms.

---

## Tracking

When the Teams adapter is implemented this document will be updated with the full working configuration. The `domain.ChannelMonitor` interface the adapter will implement is identical to the Slack adapter — the integration pattern is the same, only the underlying protocol differs (Azure Bot Framework webhook vs. Slack Socket Mode).
