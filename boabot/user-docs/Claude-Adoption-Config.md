# Claude (Anthropic API) — Adoption & Configuration

boabot ships with a first-class Anthropic API provider. This is the primary and most thoroughly tested backend — all core development uses it.

## Provider type: `anthropic`

The Anthropic provider calls the Messages API directly using the official Anthropic Go SDK. It supports tool use, multi-turn conversations, and maps rate-limit errors to a structured `RateLimitError` type so the bot can back off gracefully.

---

## Step 1 — Get an API key

Create an account at [console.anthropic.com](https://console.anthropic.com), navigate to **API Keys**, and generate a key.

---

## Step 2 — Set the API key

**Option A — environment variable (recommended for development):**

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

**Option B — credentials file (recommended for production):**

```yaml
# ~/.boabot/credentials.yaml  (chmod 600)
anthropic_api_key: "sk-ant-..."
```

boabot reads the credentials file at startup and sets the environment variable automatically. Never put the key in `config.yaml` — that file is typically committed to version control.

---

## Step 3 — Configure `config.yaml`

```yaml
models:
  default: claude-primary

  providers:
    - name: claude-primary
      type: anthropic
      model_id: claude-sonnet-4-6
```

### Choosing a model

| Model ID | Best for |
|---|---|
| `claude-opus-4-7` | Complex multi-step reasoning, architecture decisions |
| `claude-sonnet-4-6` | General-purpose tasks, good speed/quality balance (recommended default) |
| `claude-haiku-4-5-20251001` | High-throughput, latency-sensitive, or cost-sensitive tasks |

Use the model IDs exactly as shown — the Anthropic SDK validates them against the API.

---

## Multiple providers

You can configure multiple Anthropic providers pointing at different models and select between them per bot or per task type:

```yaml
models:
  default: claude-sonnet
  chat_provider: claude-haiku   # lighter model for interactive chat tasks

  providers:
    - name: claude-sonnet
      type: anthropic
      model_id: claude-sonnet-4-6

    - name: claude-haiku
      type: anthropic
      model_id: claude-haiku-4-5-20251001

    - name: claude-opus
      type: anthropic
      model_id: claude-opus-4-7
```

`chat_provider` overrides `default` for tasks sourced from the chat interface (Slack DMs, web UI chat, direct API chat calls). Use a faster/cheaper model there and reserve `default` for background tasks and board work.

---

## Rate limits and retries

The Anthropic provider maps HTTP 429 and 529 (overloaded) responses to a `RateLimitError`. The bot's worker loop detects this and backs off before retrying. No configuration is required — this is handled automatically.

Anthropic's default rate limits are tier-based. If you hit them regularly, request a tier upgrade at [console.anthropic.com/settings/limits](https://console.anthropic.com/settings/limits).

---

## Usage and cost tracking

Token usage (input and output) is returned on every API call and recorded by the budget tracker. View current spend and limits in the boabot web UI under **Budget** (orchestrator mode), or inspect `budget.json` in the memory directory.

Configure per-bot spend limits in the bot's `config.yaml`:

```yaml
# boabot-team/bots/<type>/config.yaml
budget:
  max_tokens_per_day: 1000000
  max_tool_calls_per_task: 50
```
