# Getting Started — Self-Hosting boabot

This guide walks through building and running a local BaoBot team from source. No cloud account is required.

## Prerequisites

- Go 1.26+
- `golangci-lint` installed (`brew install golangci-lint` on macOS)
- An Anthropic API key (`ANTHROPIC_API_KEY`)

## 1. Build the Binary

```bash
cd boabot
go build -o bin/boabot ./cmd/boabot
```

The binary is written to `boabot/bin/boabot`.

## 2. Create a Credentials File

```bash
mkdir -p ~/.boabot
chmod 700 ~/.boabot
cat > ~/.boabot/credentials << 'EOF'
[default]
anthropic_api_key = sk-ant-YOUR_KEY_HERE
EOF
chmod 600 ~/.boabot/credentials
```

Alternatively, export `ANTHROPIC_API_KEY` directly — environment variables take precedence over the credentials file.

## 3. Create a Team Directory

Create a working directory with the team file and per-bot configuration:

```
myteam/
  team.yaml
  bots/
    orchestrator/
      config.yaml
      SOUL.md
    implementer/
      config.yaml
      SOUL.md
```

### team.yaml

```yaml
team:
  - name: orchestrator
    type: orchestrator
    enabled: true
    orchestrator: true
  - name: implementer
    type: implementer
    enabled: true
    orchestrator: false
```

### bots/orchestrator/config.yaml

```yaml
bot:
  name: orchestrator
  type: orchestrator

orchestrator:
  enabled: true
  api_port: 8080
  web_port: 8081

models:
  default: claude
  providers:
    - name: claude
      type: anthropic
      model_id: claude-sonnet-4-6

memory:
  path: ./memory/orchestrator
  heap_warn_mb: 512
  heap_hard_mb: 1024

budget:
  token_spend_daily: 1000000
  tool_calls_hourly: 500

context:
  threshold_tokens: 150000
```

### bots/orchestrator/SOUL.md

A system prompt that defines the bot's personality, role, and working principles.

## 4. Create a Main Config File

Next to the binary (or specify with `--config`), create `config.yaml`:

```yaml
team:
  file_path: /path/to/myteam/team.yaml
  bots_dir: /path/to/myteam/bots
```

## 5. Run

```bash
./bin/boabot
```

The TeamManager reads `team.yaml`, starts each enabled bot as an in-process goroutine, and begins polling for tasks. The orchestrator bot also starts the REST API (`:8080`) and web UI (`:8081`).

Press `Ctrl+C` to stop. The TeamManager broadcasts a shutdown message to all bots and waits for their goroutines to exit cleanly.

## 6. Connect with baobotctl

Once the orchestrator is running, use `baobotctl` to manage the team:

```bash
baobotctl config set endpoint http://localhost:8080
baobotctl login
baobotctl team list
baobotctl board list
```

See [`boabotctl/user-docs/baobotctl.md`](../../boabotctl/user-docs/baobotctl.md) for the full CLI reference.

## Memory and Backup (Optional)

By default memory is stored in the `memory/` directory next to the binary. To enable GitHub git backup, add to each bot's `config.yaml`:

```yaml
backup:
  enabled: true
  schedule: "*/30 * * * *"
  restore_on_empty: true
  github:
    repo: myorg/baobot-memory
    branch: main
    author_name: BaoBot
    author_email: baobot@example.com
```

Add your GitHub token to `~/.boabot/credentials`:

```ini
[default]
anthropic_api_key = sk-ant-...
backup_token = ghp_...
```

## Heap Watchdog

To protect against memory leaks, configure soft and hard heap limits in each bot's `config.yaml`:

```yaml
memory:
  heap_warn_mb: 512    # logs a warning when heap exceeds 512 MiB
  heap_hard_mb: 1024   # shuts down gracefully when heap exceeds 1 GiB
```

## Using boabot-team Personalities

The `boabot-team/bots/` directory contains pre-built bot personalities. Point `bots_dir` at it:

```yaml
team:
  file_path: /path/to/boabot-team/team.yaml
  bots_dir: /path/to/boabot-team/bots
```

See [`boabot-team/README.md`](../../boabot-team/README.md) for the available bots.
