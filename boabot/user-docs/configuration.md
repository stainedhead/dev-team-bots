# Configuration Reference — boabot

The agent reads `config.yaml` from the same directory as the binary by default. Override with `--config <path>`.

Credentials (API keys, tokens) are **never** stored in `config.yaml`. They are resolved at startup through an ordered `SecretStore` provider chain — see [Secret Storage](#secret-storage) below for the full chain and how to provision a value with each provider.

## Minimal Required Fields

```yaml
bot:
  name: <string>      # unique name for this bot instance
  type: <string>      # bot type — must match a directory in boabot-team/bots/

models:
  default: <provider-name>   # name of the default provider
  providers:
    - name: <provider-name>
      type: anthropic         # or: bedrock | openai
      model_id: <string>      # e.g. claude-sonnet-4-6

budget:
  token_spend_daily: 1000000   # 0 = disabled
  tool_calls_hourly: 500       # 0 = disabled

context:
  threshold_tokens: 150000
```

## Team File

```yaml
team:
  file_path: ./team.yaml    # path to team.yaml (required if using TeamManager)
  bots_dir: ./bots          # directory containing per-bot subdirectories
```

## Memory

```yaml
memory:
  path: ./memory             # local directory for bot memory files (default: <binary-dir>/memory)
  vector_index: cosine       # only "cosine" supported today
  embedder: bm25             # "bm25" (default, no API key needed) | provider name (e.g. "openai")
  heap_warn_mb: 512          # log warning at this heap usage (0 = disabled)
  heap_hard_mb: 1024         # shut down gracefully at this heap usage (0 = disabled)
```

## GitHub Backup (Optional)

```yaml
backup:
  enabled: false
  schedule: "*/30 * * * *"    # cron expression (default: every 30 minutes)
  restore_on_empty: true       # clone from remote if local memory directory is empty on startup
  github:
    repo: org/repo             # e.g. myorg/baobot-memory
    branch: main
    author_name: BaoBot
    author_email: baobot@example.com
```

The GitHub token resolves through the `SecretStore` chain under the logical name `boabot_backup_token` (env var `BOABOT_BACKUP_TOKEN`, or the `boabot_backup_token` key in `~/.boabot/credentials`). It is never read from `config.yaml`. See [Secret Storage](#secret-storage) above.

## Orchestrator Mode

```yaml
orchestrator:
  enabled: false    # set to true to activate the control plane, Kanban board, REST API, and web UI
  api_port: 8080
  web_port: 8081
  plugins:
    install_dir: "./plugins"
    registries:
      - name: "official"
        url: "https://raw.githubusercontent.com/stainedhead/shared-plugins/main"
        trusted: true
    auto_update: false
```

## Channel Monitors (Slack, Buzz)

boabot reaches humans (and, for Buzz, other agents) through **channel monitors** — adapters that watch an external channel for mentions/DMs and dispatch them as tasks to a named bot. Both are optional and independent; enable either, both, or neither.

```yaml
slack:
  bot_token: "xoxb-..."      # Bot User OAuth Token; leave empty to resolve via SecretStore
  app_token: "xapp-..."      # App-Level Token (Socket Mode); leave empty to resolve via SecretStore
  bot_name: "tech-lead"      # boabot bot that receives dispatched Slack messages

buzz:
  enabled: true
  relay_url: wss://relay.example.com   # buzz-relay endpoint
  bot_name: "tech-lead"                # boabot bot that receives dispatched Buzz messages
  owner_pubkey: <hex>                  # optional; widens the `!shutdown` gate
  respond_to: <hex>                    # optional; single-pubkey author gate
  respond_to_allowlist:                # optional; omit the key entirely for no gate — an
    - <hex-of-a-trusted-pubkey>        #   empty list ([]) is an active allow-none gate, not "no gate"
  presence_interval: 60s               # optional; must stay under 180s
```

Both blocks' secret fields (`slack.bot_token`/`slack.app_token`, and Buzz's private key/API token, which have **no** `config.yaml` field at all) resolve through the `SecretStore` chain described below when left empty. Inline Slack tokens still work for backward compatibility but log a deprecation warning recommending the store instead.

See [`Slack-Adoption-Config.md`](Slack-Adoption-Config.md) and [`Buzz-Adoption-Config.md`](Buzz-Adoption-Config.md) for full setup walkthroughs (app registration, OAuth scopes, keypair generation, usage examples).

## Secret Storage

Every secret boabot uses — `ANTHROPIC_API_KEY`, `BOABOT_BACKUP_TOKEN`, Slack's bot/app tokens, and Buzz's private key/API token alike — resolves through the same ordered `SecretStore` provider chain, first hit wins:

1. **Environment variable** — e.g. `ANTHROPIC_API_KEY`, `BUZZ_PRIVATE_KEY`. Process-global; always wins if set.
2. **systemd credential** — `$CREDENTIALS_DIRECTORY/<key>`, materialised by `LoadCredentialEncrypted=`/`SetCredentialEncrypted=` in a unit file. Linux-service only; a clean, costless no-op miss when `$CREDENTIALS_DIRECTORY` is unset.
3. **OS keystore** — macOS Keychain, Windows Credential Manager, or Linux Secret Service, set via `boabotctl secret set <key> [--bot <name>]`.
4. **Credentials file** — `~/.boabot/credentials` (INI, `chmod 600`), the same file described above.

**Key naming and per-bot namespacing:** each secret has a logical name (e.g. `anthropic_api_key`, `buzz_private_key`, `slack_bot_token`). Secrets tied to a specific bot (Slack and Buzz tokens) are namespaced by that bot's `name` from `team.yaml`:

| Provider | Global secret | Bot-scoped secret |
|---|---|---|
| Environment variable | `<KEY>` (e.g. `ANTHROPIC_API_KEY`) | Same — env vars ignore bot scoping. Two bots on one host cannot get different values for the same secret name via env var; the first one boabot reads wins for both. Use the keystore, systemd, or file provider instead if you need per-bot values with env-var-style provisioning. |
| systemd / credentials file | `<key>` (e.g. `boabot_backup_token`) | `<bot>_<key>` (e.g. `tech-lead_buzz_private_key`) |
| OS keystore | service `boabot`, account `<key>` | service `boabot`, account `<bot>/<key>` |

Bot-scoped providers are **strict-match with no fallback to the global key** — a per-bot secret that isn't set under its bot-scoped key is a miss, not a silent match against a different bot's (or the global) entry. If no provider in the chain has the value, boabot's startup error names the secret and every provider it checked.

**Diagnosing resolution:** run `boabot --diag-secrets` to print, for each configured secret, which provider resolved it (provider name only — the value itself is never printed or logged, by any provider, ever).

**Managing keystore entries:** use `boabotctl secret set|get|delete <key> [--bot <name>]` (local machine only, no remote-host mode). `set` prompts interactively (or reads piped stdin) and never accepts the value as a command-line argument; `get` reports presence/absence only.

See [`Buzz-Adoption-Config.md`](Buzz-Adoption-Config.md#secret-provisioning-the-per-os--per-mode-matrix) for the per-OS × per-mode matrix covering interactive vs. unattended-service provisioning — the OS keystore behaves very differently under a headless service than in a logged-in session, and that matrix applies to every secret in this chain, not just Buzz's.

## Tools

```yaml
tools:
  allowed_tools:
    - read_file
    - list_dir
    - glob
    - grep
    - write_file
    - edit_file
    - memory_search
    - send_message
    - read_messages
    - todo_write
    - todo_read
    - http_request
    - get_metrics
  http_allowed_hosts:     # hosts the http_request tool may contact
    - api.github.com
  receive_from:           # bot names permitted to send action-triggering messages to this bot
    - orchestrator
```

## Budget Caps

```yaml
budget:
  token_spend_daily: 1000000   # maximum tokens per calendar day (UTC); 0 = disabled
  tool_calls_hourly: 500       # maximum tool dispatches per hour; 0 = disabled
```

Counters are persisted to `budget.json` in the bot's memory directory and restored on startup.

## Context Management

```yaml
context:
  threshold_tokens: 150000   # context size at which checkpoint-and-restart is triggered
```

## Credentials File

API keys are stored in `~/.boabot/credentials` (INI format, mode 0600):

```ini
[default]
anthropic_api_key = sk-ant-...
boabot_backup_token = ghp_...

[staging]
anthropic_api_key = sk-ant-...
```

Select a non-default profile with `BOABOT_PROFILE=staging`. Values in the credentials file are applied only if the corresponding environment variable is not already set — environment variables always take precedence.

## Provider Types

| Type | Env var / credential key | Notes |
|---|---|---|
| `anthropic` | `ANTHROPIC_API_KEY` / `anthropic_api_key` | Primary provider. Any non-empty key accepted at startup. |
| `bedrock` | AWS SDK credentials (standard chain) | Requires AWS account; region set in provider config. |
| `openai` | `OPENAI_API_KEY` / `openai_api_key` | OpenAI-compatible; `endpoint` overrides base URL (e.g. for Ollama). |
