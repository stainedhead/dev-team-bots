# Technical Details — boabot

## Package Layout

```
cmd/boabot/
  main.go               # wiring only — no business logic

internal/
  domain/
    agent.go            # Agent interface and lifecycle types
    worker.go           # Worker interface and task types
    memory.go           # MemoryStore, VectorStore, Embedder, MemoryBackup interfaces
    message.go          # message types: registration, heartbeat, task, delegation, broadcast, shutdown
    provider.go         # ModelProvider and ProviderFactory interfaces
    mcp.go              # MCPClient interface
    queue.go            # MessageQueue and Broadcaster interfaces
    tool.go             # Tool, ToolGater, and ToolScorer interfaces
    skill.go            # Skill and SkillRegistry interfaces
    budget.go           # BudgetTracker interface (token + tool-call caps; local JSON-backed)
    dlq.go              # DLQStore interface (list / retry / discard dead-letter queue items)
    card.go             # AgentCard types and CardRegistry interface
    orchestrator.go     # ControlPlane, BoardStore interfaces (orchestrator mode)
    user.go             # User, Role, and Auth interfaces (orchestrator mode)
    mocks/              # generated/hand-written mocks for all interfaces

  application/
    run_agent.go        # top-level agent loop use case
    process_message.go  # message routing and dispatch
    execute_task.go     # worker goroutine execution harness; optional BudgetTracker enforcement
                        # (WithBudgetTracker wires cost gate: CheckAndRecordToolCall before
                        #  Invoke, CheckAndRecordTokens after using actual usage counts)
    context_manager.go  # progressive disclosure, checkpoint-and-restart
    register.go         # bot registration, heartbeat use cases
    memory_ops.go       # read, write, search memory use cases
    delegation.go       # send/receive structured delegation messages
    skills.go           # skill index loading and script execution
    backup/             # ScheduledBackupUseCase (cron-driven GitHub backup)
    cost/               # EnforceBudgetUseCase, DailyCostReviewUseCase
    scheduler/          # cron-based task scheduler; TriageUseCase for label-based routing
    team/               # TeamManager (goroutine-per-bot), BotRegistry, localProviderFactory
    workflow/           # CreateWorkItem, AdvanceWorkflow, AssignBot, StalledItemRecovery
    subteam/            # SubTeamManager: spawn/terminate/heartbeat for tech-lead sub-agents
    pool/               # TechLeadPool: allocate/deallocate pool of tech-lead instances (orchestrator)
    orchestrator/       # QueueRunner (poll/dispatch/reconcile Kanban queue), BoardDispatch

  infrastructure/
    aws/
      bedrock/          # Bedrock ModelProvider adapter (optional); RateLimitError for ThrottlingException
    anthropic/          # Anthropic API ModelProvider adapter (primary); rate-limit mapping
    credentials/        # INI credentials loader (~/.boabot/credentials); BOABOT_PROFILE env var
    github/
      backup/           # GitHubBackup implementing domain.MemoryBackup; go-git v5 based
    local/
      queue/            # in-process Router + per-bot Queue implementing domain.MessageQueue
      bus/              # in-process Bus implementing domain.Broadcaster
      fs/               # local filesystem FS implementing domain.MemoryStore
      budget/           # local JSON-backed BudgetTracker implementing domain.BudgetTracker
      vector/           # cosine similarity VectorStore implementing domain.VectorStore
                        # on-disk format: <key>.vec (LE binary) + <key>.meta (JSON)
                        # atomic writes via temp-file + os.Rename; O(n) flat-slice search
      bm25/             # BM25 feature-hash Embedder; FNV-1a hashing, L2-normalised, 512-dim
      watchdog/         # heap watchdog; injectable readMem seam; warn/hard limit goroutine
    auth/local/         # LocalAuthProvider: bcrypt (cost 12) + HS256 JWT (24 h TTL)
                        # VerifyPassword validates credentials without issuing a token
    db/                 # PostgreSQL repository: work items, workflow, metrics, users
                        # UserRepo: CRUD against `users` table; Enabled inverts the `disabled` column
    cliagent/           # SubprocessRunner: domain.CLIAgentRunner; SIGTERM→SIGKILL (5s), stdin channel, progress callback
    mcp/                # MCP client adapter
    slack/              # Slack Socket Mode ChannelMonitor adapter
    buzz/               # Buzz (Nostr relay) ChannelMonitor adapter over fiatjaf.com/nostr:
                        #   relay_client.go/reconnect.go (WebSocket, NIP-42 auth, backoff+jitter),
                        #   nipoa.go (NIP-OA/NIP-AA attestation), keypair.go/token.go (SecretStore-
                        #   backed nsec/API token resolution), monitor.go/discovery.go/presence.go/
                        #   trigger.go/guard.go (NIP-29 channel discovery, dispatch, replies,
                        #   presence, p-gate guards), lock.go (FR-031 process-singleton lock)
    secret/             # SecretStore provider chain: env/, file/ (wraps credentials/), systemd/
                        #   ($CREDENTIALS_DIRECTORY), keystore/ (zalando/go-keyring) — ordered,
                        #   first-hit-wins, per-provider 2s timeout (store.go)
    otel/               # OpenTelemetry provider: OTLP/HTTP trace + metric exporters; noop fallback
    screening/          # RegexScreener: injection-pattern detection + [REDACTED] sanitisation
    workflow/           # ConfigLoader: YAML workflow config with SIGHUP hot-reload
    http/               # Orchestrator REST API (Go 1.22 ServeMux) + HTMX Kanban board web UI
                        # Auth: Bearer JWT middleware; admin-only guard on protected routes
                        # Routes: /api/v1/{auth,board,team,skills,users,profile,dlq,pool}
                        # Web UI: / → HTMX Kanban board (auto-refreshes every 30s)
                        # htmx loaded from unpkg.com with SHA-384 SRI hash for supply-chain safety
    session_file.go     # SessionFile: atomic JSON persistence for spawned sub-agent records
                        # (<memory>/session.json; write-tmp-then-rename; corrupt file → empty slice)
    pool_state_file.go  # PoolStateFile: atomic JSON persistence for pool state records
                        # (<orchestrator-memory>/pool.json; same atomic write strategy)
    config/             # config file loading (YAML)
```

## Threading Model

```
main goroutine
  └── TeamManager.Run()
        ├── bot goroutine (per enabled bot, runBotWithRestart + recover)
        │     └── RunAgentUseCase.Run()
        │           └── Worker goroutines (per task, recover() guards panic)
        │                 └── ContextManager (progressive disclosure, checkpoint-and-restart)
        │                 └── ToolGater (BM25 scoring, schema injection)
        │     └── [tech-lead only] SubTeamManager
        │           └── spawned bot goroutine (per sub-agent, recover() guards panic)
        │                 └── heartbeat watchdog timer (90s timeout → self-terminate)
        └── watchdog goroutine (heap monitor, optional; cancels shared context on hard limit)
```

## Config File Structure

```yaml
bot:
  name: <name>
  type: <type>

orchestrator:
  enabled: false
  api_port: 8080
  web_port: 8081

models:
  default: <provider-name>
  providers:
    - name: <provider-name>
      type: anthropic | bedrock | openai
      model_id: <model-id>
      endpoint: <url>        # openai only
      region: <region>       # bedrock only

memory:
  path: ./memory             # default: <binary-dir>/memory
  vector_index: cosine       # "cosine" (default) | future options
  embedder: bm25             # "bm25" (default) | "openai" (requires OPENAI_API_KEY)
  heap_warn_mb: 512          # 0 = disabled
  heap_hard_mb: 1024         # 0 = disabled

backup:
  enabled: false
  schedule: "*/30 * * * *"
  restore_on_empty: true
  github:
    repo: org/repo
    branch: main
    author_name: BaoBot
    author_email: baobot@example.com

team:
  file_path: ./team.yaml
  bots_dir: ./bots

buzz:
  enabled: false             # single early activation guard (FR-036) — everything below is inert
  relay_url: wss://relay.example.com
  bot_name: <name>           # must match an enabled bot's name in team.yaml
  owner_pubkey: <hex>        # consulted only by the !shutdown wider gate; optional
  respond_to: <hex>          # optional single-pubkey author gate
  respond_to_allowlist:      # optional; nil (omitted) = no gate, [] = allow-none
    - <hex>
  presence_interval: 60s     # must stay under the 180s FR-023 staleness bound

tools:
  allowed_tools:            # built-in tools this bot may use
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
  http_allowed_hosts:       # hosts http_request may contact
    - api.github.com
    - hooks.slack.com
  receive_from:             # bots permitted to send action-triggering messages
    - orchestrator
    - architect

budget:
  token_spend_daily: 1000000
  tool_calls_hourly: 500

context:
  threshold_tokens: 150000  # trigger checkpoint-and-restart at this context size
```

Credentials (API keys) are **never** stored in `config.yaml`. They are read from `~/.boabot/credentials` (INI format) or environment variables at startup. The `BOABOT_PROFILE` environment variable selects a non-default profile.

Note `buzz:` has no `channels:` field. Channel discovery (`internal/infrastructure/buzz/discovery.go`) is entirely dynamic — the bot subscribes to whichever channels the relay confirms it as a member of (`kind:39000`/`kind:39002`) and reacts to `kind:44100`/`44101` membership events at runtime; there is no static list to configure. `config.Load`'s `yaml.Decoder.KnownFields(true)` rejects any key under `buzz:` that is not one of `BuzzConfig`'s literal fields — including a `channels:` key and any secret-looking key (`nsec`, `private_key`, `api_token`, etc.), per FR-035. See [`user-docs/Buzz-Adoption-Config.md`](../user-docs/Buzz-Adoption-Config.md).

## SubTeamManager

**Clean Architecture placement:** Application layer (`internal/application/subteam/`). Implements `domain.SubTeamManager`. Depends on `infrastructure.SessionFile` for persistence.

**Isolation model.** Each spawned sub-agent receives a dedicated `context.CancelFunc` derived from the tech-lead's context. The sub-agent's bus ID (`bus-<name>`) and queue router are created fresh per spawn; no shared state exists between the parent and the sub-agent or between sibling sub-agents. The `botState` struct holds the cancel function, a `done` channel (closed when the goroutine exits), and a buffered heartbeat channel.

**Heartbeat mechanism.** `SendHeartbeat(ctx)` acquires a lock over all live bots and sends a `struct{}{}` to each bot's `heartbeat` channel (non-blocking, drops on full). Inside `runBot`, a `time.Timer` is reset on each received heartbeat. If the timer fires before the next heartbeat, the goroutine logs a warning and returns, triggering the deferred `markTerminated` cleanup. Default interval: 30s. Default timeout: 90s (three missed intervals).

**Panic recovery.** `runBot` defers a `recover()`. Any panic inside a spawned bot goroutine is caught, logged with `slog.Error`, and the goroutine exits cleanly. The panic does not propagate to the tech-lead.

**Session file persistence.** A `SessionFile` path is `<memory>/session.json`. On `Spawn`, the new `SessionRecord` (name, bot_type, work_dir, bus_id, status, spawned_at) is appended and the file re-saved atomically (write .tmp → `os.Rename`). On `markTerminated`, the record for that name is filtered out and the file re-saved. If the file does not exist on `Load`, an empty slice is returned — no error. If the file is corrupt, a warning is logged and an empty slice is returned.

**Soft spawn limit.** Default: 5. When `len(bots)+1 > SoftSpawnLimit`, `slog.Warn` is emitted. Spawning proceeds regardless.

**`Terminate` timeout.** `Terminate` cancels the bot's context then waits on `done` with a 5-second `context.WithTimeout`. If the goroutine does not exit in time, `Terminate` returns a timeout error but the goroutine may still be running — callers should treat the agent as unreliable after this point.

**`TearDownAll` timeout.** Cancels all bot contexts concurrently, then waits on each `done` channel with a shared 10-second deadline. Errors are accumulated and returned via `errors.Join`.

## TechLeadPool

**Clean Architecture placement:** Application layer (`internal/application/pool/`). Implements `domain.TechLeadPool`. Depends on `infrastructure.PoolStateFile` for persistence.

**Pool lifecycle.** The pool is a slice of `*domain.PoolEntry` protected by a `sync.Mutex`. All mutating operations (`Allocate`, `Deallocate`) hold the mutex for their full duration, ensuring serialised access. An auto-incrementing counter generates instance names in the form `tech-lead-N`.

**Allocate.** Scans entries for an idle entry first. If found, status is set to `allocated`, `ItemID` and `AllocatedAt` are updated, the file is saved, and the entry is returned. If no idle entry exists, `spawnFn(ctx, name)` is called with a `SpawnTimeout` (default 1s) context. On spawn success, a new `PoolEntry` is appended. On spawn failure, the counter is rolled back.

**Deallocate.** Finds the entry by `ItemID`. If it is the only entry in the slice, the entry is converted to idle warm standby (status `idle`, `ItemID` cleared) — `stopFn` is not called. If there are multiple entries, the entry is removed from the slice and `stopFn(ctx, instanceName)` is called (errors are logged but not returned).

**Warm standby guarantee.** At least one `idle` entry always remains in the pool once the first allocation has occurred. This eliminates cold-start latency for the next `Allocate` call.

**Reconcile.** On startup, `Reconcile(ctx)` loads the `PoolStateFile`, calls `isRunFn(ctx, instanceName)` for each record, discards records whose instances are not running, and rebuilds the live slice. The file is re-saved after reconciliation.

**Persistence.** `PoolStateFile` writes to `<orchestrator-memory>/pool.json` atomically (write .tmp → `os.Rename`). Each `PoolStateRecord` carries: `instance_name`, `status`, `item_id` (omitempty), `bus_id`, `allocated_at` (omitempty).

**Soft pool limit.** Default: 10. `slog.Warn` is emitted when `len(entries)+1 > SoftPoolLimit`. Allocation is not blocked.

## New Message Types

Three new message types were added to `domain.MessageType` in `internal/domain/message.go`:

| Constant | Wire value | Payload struct | Description |
|---|---|---|---|
| `MessageTypeSubTeamSpawn` | `subteam.spawn` | `SubTeamSpawnPayload` | Instructs a tech-lead to spawn a named sub-agent |
| `MessageTypeSubTeamTerminate` | `subteam.terminate` | `SubTeamTerminatePayload` | Instructs a tech-lead to terminate a named sub-agent |
| `MessageTypeSubTeamHeartbeat` | `subteam.heartbeat` | — | Heartbeat signal sent to a tech-lead's queue; tech-lead calls `SubTeamManager.SendHeartbeat` in response |

**`SubTeamSpawnPayload`**
```json
{
  "bot_type": "implementer",
  "name":     "impl-auth",
  "work_dir": "/workspace/auth-service"
}
```

**`SubTeamTerminatePayload`**
```json
{
  "name": "impl-auth"
}
```

`work_dir` is optional in `SubTeamSpawnPayload` — omit or set to `""` to inherit the tech-lead's working directory.

## REST API — GET /api/v1/pool

Returns the current tech-lead pool state. No authentication required. Available only when `orchestrator.enabled: true`.

**Response schema:**
```json
{
  "pool": [
    {
      "InstanceName": "tech-lead-1",
      "Status":       "allocated",
      "ItemID":       "item-42",
      "AllocatedAt":  "2026-05-07T14:30:00Z",
      "BusID":        "bus-tech-lead-1"
    }
  ]
}
```

Field types follow `domain.PoolEntry`. `Status` values: `idle`, `allocated`, `terminating`. `ItemID` and `AllocatedAt` are empty/zero on idle entries.

If the pool is not configured (nil), the endpoint returns `{"pool": []}` with status 200.

## Plugin System Architecture

### New Packages

| Package | Purpose |
|---|---|
| `internal/domain/plugin.go` | All plugin domain types and interfaces: `Plugin`, `PluginStatus`, `PluginManifest`, `PluginProvides`, `PluginPermissions`, `PluginRegistry`, `RegistryIndex`, `RegistryEntry`, `InstallPluginRequest`, `AddRegistryRequest`, `PluginStore` (interface), `RegistryManager` (interface) |
| `internal/application/plugin/install.go` | `InstallUseCase` — orchestrates the full install path: look up registry → fetch index → locate entry → fetch manifest → fetch archive → delegate to `PluginStore.Install` → emit audit log |
| `internal/application/plugin/manage.go` | `ManageUseCase` — wraps all post-install lifecycle operations (List, Get, Approve, Reject, Enable, Disable, Reload, Remove) with audit logging |
| `internal/application/plugin/registry.go` | Registry management use case: List, Add, Remove, FetchIndex — thin orchestration over `RegistryManager` |
| `internal/infrastructure/local/plugin/store.go` | `LocalPluginStore` — filesystem-backed `PluginStore`. Manages an in-memory index (`map[string]domain.Plugin`) protected by `sync.RWMutex`. On startup, scans `install_dir` for subdirectories containing `status.json` and `plugin.yaml`; skips corrupt or missing files without crashing. |
| `internal/infrastructure/local/plugin/installer.go` | `Extract` — atomic tar.gz extraction: verify SHA-256 → extract to `<plugin-name>-tmp` → rename to `<plugin-name>`. Enforces zip-slip protection (path prefix check per member) and 50 MB total size cap. Cleanup on any error deletes the temp directory. |
| `internal/infrastructure/http/registry_client.go` | `HTTPRegistryManager` — implements `domain.RegistryManager`. Fetches `<registry-url>/index.json` with a 10s timeout, caching results in a `map[string]cachedIndex` (5-min TTL). Fetches manifests (YAML or JSON, auto-detected) and archives. Persists runtime-added registries to `install_dir/registries.json`. |

### Modified Packages

| Package | Change |
|---|---|
| `internal/infrastructure/local/mcp/client.go` | `ListTools` now appends active plugin tools to the built-in tool list, detecting and skipping name collisions with a warning. `CallTool` checks the plugin store first; if the named tool belongs to an active plugin, the plugin's entrypoint is executed as a subprocess with args passed as JSON on stdin. Subprocess timeout: 30s. |
| `internal/infrastructure/http/server.go` | Registers 14 plugin/registry endpoints (4 registry + 10 plugin) when `Config.Plugins` is non-nil. All write operations require admin role. Read endpoints (`GET /api/v1/plugins`, `GET /api/v1/plugins/{id}`, `GET /api/v1/registries`) do not require auth. |
| `internal/infrastructure/config/config.go` | Added `PluginsConfig` struct under `OrchestratorConfig.Plugins`: `install_dir` (string), `registries` (list of `PluginRegistryConfig`), `auto_update` (bool). |

### install_dir Layout

```
<install_dir>/
  registries.json         # runtime-added registries (written by HTTPRegistryManager)
  <plugin-name>/
    plugin.yaml           # manifest, stored as JSON (despite .yaml extension)
    status.json           # Plugin struct minus Manifest (id, name, version, registry, status, installed_at)
    run.sh                # or whatever entrypoint is declared in plugin.yaml
    ... (other plugin files)
  <plugin-name>-tmp/      # transient: present only during extraction; always cleaned up
```

### Atomic Install Strategy

1. SHA-256 checksum of the raw archive bytes is verified against `manifest.Checksums["sha256"]` before touching the filesystem.
2. A temporary directory `<install_dir>/<plugin-name>-tmp` is created.
3. The `.tar.gz` is extracted into the temp directory. Size and zip-slip checks run per-member.
4. On success, `os.Rename` atomically moves the temp directory to `<install_dir>/<plugin-name>`.
5. On any failure, `os.RemoveAll(tmpDir)` cleans up. No partial state remains.

### Registry Index Cache

`HTTPRegistryManager` holds a `map[string]cachedIndex` protected by `sync.RWMutex`. Each cached entry records the `RegistryIndex` value and the `fetchedAt` time. On `FetchIndex(ctx, url, force=false)`, the cache is checked first; if the entry is younger than 5 minutes it is returned without a network call. `force=true` bypasses the cache and always fetches from the network.

The cache lives in the `HTTPRegistryManager` instance (infrastructure layer) rather than the application use case, keeping the use case stateless and enabling the cache to serve concurrent callers without application-layer locking.

### MCP Client Plugin Dispatch

The MCP client (`internal/infrastructure/local/mcp/client.go`) holds optional references to a `domain.PluginStore` and an `installDir` string, injected via functional options (`WithPluginStore`, `WithInstallDir`). Both are nil by default, preserving backward compatibility for bots that do not use the plugin system.

On each `CallTool` call, if `pluginStore` is set, `callPluginTool` scans active plugins for the named tool before falling through to builtins. The plugin entrypoint is executed with `exec.CommandContext` with a 30-second timeout; arguments are passed as a JSON object on stdin; the result is read from stdout as a `domain.MCPToolResult` JSON object. Non-zero exit or decode failure returns an `MCPToolResult` with `IsError: true`.

If the plugin's entrypoint is a `plugin.json` file (detected by `filepath.Base(entrypoint) == "plugin.json"`), `callPluginTool` delegates to `readSkill` instead of attempting exec. This enables Claude Code plugins (whose entrypoints are JSON manifests rather than executables) to surface their Markdown skill instructions to bots.

### `read_skill` Built-in Tool

`read_skill(name string) → string` is a built-in MCP tool added when a `pluginStore` is configured. It scans active plugins for a tool matching the requested name, then reads `<installDir>/<pluginName>/commands/<name>.md` and returns the full Markdown content. Bots use this to load skill instructions before executing multi-step skill workflows using their own built-in tools (no external executor required).

### Plugin Store Race Fix

`TeamManager.Run()` pre-resolves the plugin store before launching any bot goroutines by scanning the team entries for the orchestrator, loading its config, and calling `localplugin.NewLocalPluginStore(installDir)`. The result is captured in `resolvedPluginStore` and `resolvedInstallDir` struct fields, which are read-only after Run starts goroutines. This eliminates the previous race where `startBot` goroutines could write `tm.pluginStore` concurrently while other goroutines read it.

### CLIAgentRunner

`domain.CLIAgentRunner` (in `internal/domain/cliagent.go`) is an interface for spawning CLI agent subprocesses. `CLIAgentConfig` carries binary path, work directory, additional args, optional model, and timeout (default 30 min).

`cliagent.SubprocessRunner` (in `internal/infrastructure/cliagent/runner.go`) implements the interface:
- Verifies the binary via `exec.LookPath` before starting.
- Wraps the context in a timeout (30 min default, or `cfg.Timeout`).
- Sends SIGTERM on cancel/timeout via `cmd.Cancel`; gives 5-second grace period via `cmd.WaitDelay` before SIGKILL.
- Reads stdout line-by-line in a goroutine; calls `progress(line)` per non-empty line; accumulates full output.
- Optional stdin channel: a goroutine drains the channel and writes to `cmd.Stdin`; closes the pipe on channel close or context cancellation. If `stdin == nil`, no stdin goroutine is started.
- Scanner goroutine is always drained after `cmd.Wait()` to prevent goroutine leaks.

### CLI Agent MCP Tools

Four CLI agent tools are available in the local MCP client when a `CLIAgentRunner` is wired and the corresponding tool is enabled in config:

| Tool | Binary | Key Flags |
|---|---|---|
| `run_claude_code` | `claude` | `--output-format=stream-json --dangerously-skip-permissions -p` |
| `run_codex` | `codex` | `-q --approval-mode=full-auto` |
| `run_openai_codex` | `openai-codex` | `--full-auto` |
| `run_opencode` | `opencode` | `-q` |

All tools accept `instruction` (required), `work_dir` (required), and `model` (optional) as tool arguments. The model flag is passed as `--model <value>` when non-empty.

Binary availability is checked at `ListTools` call time via `exec.LookPath` (or `os.Stat` for absolute paths). Tools whose binary is not found or is disabled are silently omitted — this is a normal condition and generates no log output.

Claude Code output is post-processed through `codeagent.ParseStreamLine` to extract text from stream-json events. Other tools accumulate plain-text stdout.

A `progressFn func(line string)` field on the MCP client (set via `WithProgressFn`) receives each stdout line from CLI subprocesses, enabling real-time progress in the operator UI.

## QueueRunner

**Clean Architecture placement:** Application layer (`internal/application/orchestrator/queue_runner.go`).

**Poll loop.** `QueueRunner.Start(ctx)` ticks at a configurable interval (default 5s). Each tick:
1. **Reconcile** — fetches all `in-progress` items that have an `ActiveTaskID`, queries `DirectTaskStore` for each task, and transitions items whose tasks have succeeded (`done`) or failed (`errored`). Update errors are logged as warnings; the loop continues.
2. **Capacity check** — counts `in-progress` items; if already at `MaxConcurrent` (default 3) the tick exits early.
3. **Sort queue** — fetches `queued` items; sorts with ASAP items first (FIFO by `QueuedAt`), then scheduled items (FIFO by `QueuedAt`). Items without a `QueuedAt` fall back to `CreatedAt`.
4. **Dispatch** — iterates sorted items, calls `isReady` for each, and calls `launch` for up to `slots` ready items.

**Queue modes (isReady).**

| Mode | Ready when |
|---|---|
| `asap` / `""` | Immediately |
| `run_at` | `QueueRunAt` ≤ now |
| `run_after` | Predecessor `status == done` (or `done\|errored` when `require_success: false`) |
| `run_when` | Both time condition (`run_at`) AND predecessor condition (`run_after`) satisfied; either may be omitted |

**Launch.** Sets `status = in-progress`, clears scheduling fields, calls `Board.Update`, then calls `Dispatcher.DispatchBoardItem`. If `Update` fails, `launch` returns false and the item is not dispatched. If `DispatchBoardItem` fails, the item remains `in-progress` for operator investigation — it is not rolled back.

## CLI Agent Delegation

### New Package: `internal/infrastructure/cliagent/`

`SubprocessRunner` implements `domain.CLIAgentRunner`. Each `Run` call is independent and safe for concurrent use.

Key behaviours:
- Verifies the binary via `exec.LookPath` before starting. Returns a descriptive error if the binary is not found.
- Wraps the parent context in a `context.WithTimeout` (default 30 minutes; overridable via `CLIAgentConfig.Timeout`).
- Sends `SIGTERM` on context cancellation via `cmd.Cancel`; gives 5 seconds for the process to exit cleanly before `WaitDelay` triggers force-close of pipes and `SIGKILL`.
- Reads stdout line-by-line in a goroutine using `bufio.Scanner`. Each non-empty line is passed to `progress(line)` and accumulated in a `strings.Builder`. The scanner goroutine is always drained after `cmd.Wait()` to prevent goroutine leaks.
- Optional stdin channel: a `drainStdin` goroutine reads from the channel and writes each line to the subprocess stdin pipe. Exits when the channel is closed or ctx is cancelled. If `stdin == nil`, no stdin goroutine is started.
- Captures stderr to a buffer; includes it in the error message on non-zero exit.
- Returns the accumulated stdout, trimmed of trailing newlines.

### New Domain Interface: `internal/domain/cliagent.go`

```go
type CLIAgentConfig struct {
    Binary  string
    WorkDir string
    Args    []string
    Model   string
    Timeout time.Duration
}

type CLIAgentRunner interface {
    Run(ctx context.Context, cfg CLIAgentConfig, instruction string,
        stdin <-chan string, progress func(line string)) (string, error)
}
```

`CLIAgentRunner` is distinct from `domain.ModelProvider`. It models a long-running subprocess invoked as an MCP tool, not a turn-based prompt/response cycle.

### MCP Client Changes (`internal/infrastructure/local/mcp/client.go`)

**`read_skill` tool:** Added to `ListTools` when `pluginStore` is non-nil. Resolves the named skill across active plugins, reads `<installDir>/<pluginName>/commands/<name>.md`, and returns the Markdown content. Returns a descriptive error string if the skill is not found or the file cannot be read.

**CLI tool dispatch:** Four tools (`run_claude_code`, `run_codex`, `run_openai_codex`, `run_opencode`) are appended to `ListTools` when a `CLIAgentRunner` is configured and the corresponding tool is enabled in config. Binary availability is checked at `ListTools` call time via `exec.LookPath` (or `os.Stat` for absolute paths). Tools whose binary is absent or disabled are silently omitted — no log output.

`CallTool` dispatches to the appropriate `CLIAgentRunner.Run` call when a CLI tool name is matched. The `instruction`, `work_dir`, and `model` fields are extracted from the tool input. For `run_claude_code`, stdout is post-processed through `codeagent.ParseStreamLine` to extract text from `stream-json` events. Other tools accumulate plain-text stdout.

**`isPluginJSONEntrypoint` routing fix:** `callPluginTool` now distinguishes three cases: (1) plugin not found or inactive — fall through to builtins; (2) active plugin with a `plugin.json` entrypoint — delegate to `readSkill`; (3) active plugin with an executable entrypoint — spawn subprocess. This prevents `exec: "plugin.json": executable file not found in $PATH` errors when bots call Claude Code plugin tools.

### Config Additions (`internal/infrastructure/config/config.go`)

```yaml
orchestrator:
  cli_tools:
    claude_code:
      enabled: false
      binary_path: ""   # empty → PATH lookup of "claude"
    codex:
      enabled: false
      binary_path: ""
    openai_codex:
      enabled: false
      binary_path: ""
    opencode:
      enabled: false
      binary_path: ""
```

`CLIToolsConfig` and `CLIToolConfig` are nested under `OrchestratorConfig`. The config loader uses `dec.KnownFields(true)`, so YAML tags must exactly match: `cli_tools`, `claude_code`, `codex`, `openai_codex`, `opencode`, `enabled`, `binary_path`.

### Thread Safety: Plugin Store Pre-Resolution

`TeamManager.Run()` now pre-resolves the plugin store before launching any bot goroutines. It scans `teamCfg.Team` for the orchestrator entry (the one with `Orchestrator.Plugins.InstallDir` set), loads that bot's config, and calls `localplugin.NewLocalPluginStore(installDir)`. The result is stored in read-only local variables (`resolvedPluginStore`, `resolvedInstallDir`) that are passed as parameters to each `startBot` call. No struct fields are written from goroutines. This eliminates the previous data race where concurrent `startBot` goroutines wrote `tm.pluginStore` while others read it.

## Task Scheduling and Notifications

### New Domain Files

| File | Purpose |
|---|---|
| `internal/domain/schedule.go` | `Schedule` (mode + `NextRunAt`), `RecurrenceRule` (frequency, `DaysMask`, `TimeOfDaySeconds`, `MonthDay`), `ScheduleMode` constants (`asap`, `future`, `recurring`), `DirectTask.Schedule` field extension |
| `internal/domain/agent_notification.go` | `AgentNotification`, `NotificationStatus` (`unread`, `read`, `actioned`), `DiscussEntry`, `AgentNotificationStore` interface |

### New Application Packages

**`internal/application/scheduling/`**

`SchedulerService` polls `DirectTaskStore.ListDue` every 10 seconds. For each due task it calls `DirectTaskStore.ClaimDue`, which atomically sets the task status to `dispatching` under a mutex. Only the goroutine that wins `ClaimDue` dispatches the task; concurrent ticks skip tasks already in `dispatching` state. On startup, `CatchUpMissedRuns` performs one immediate pass to cover tasks that became due during downtime. After a recurring task fires, `AdvanceRecurrence` computes the next `NextRunAt` and persists it before dispatching.

**`internal/application/notifications/`**

`NotificationService` implements create, list (with bot / status / search filters), read-mark, discuss-append, requeue, and bulk-delete operations. Requeue prepends the full discuss thread as context to the originating task payload and calls the task dispatcher at ASAP priority. All mutations are delegated to `AgentNotificationStore`.

**`internal/application/orchestrator/chat_task_manager.go`**

`ChatTaskManager` scores operator chat messages across three keyword categories (action verbs, time expressions, bot names). Messages scoring 2+ signals enter a two-step confirmation flow: the bot responds with its interpretation; the operator confirms or cancels. `ParseScheduleNL` converts natural-language strings (e.g., "every Monday at 9am") into a `RecurrenceRule`. This path is active only in orchestrator mode; non-orchestrator bot chat is unaffected.

### New Infrastructure

**`internal/infrastructure/local/orchestrator/agent_notification_store.go`**

File-backed `AgentNotificationStore`. Reads and writes `notifications.json` in the orchestrator memory directory. All mutations lock a `sync.Mutex`, marshal the full notification slice to JSON, and write atomically via temp-file + `os.Rename`. On load, a missing file returns an empty slice; a corrupt file logs a warning and returns an empty slice.

**`internal/infrastructure/local/orchestrator/direct_task_store.go` (extended)**

`ListDue` returns all tasks whose `NextRunAt` is non-nil and in the past. `ClaimDue(id)` is mutex-guarded: it sets the task's status to `dispatching` and persists the change only if the task is still in its previous (non-dispatching) state. Concurrent callers for the same task ID see a not-eligible error and skip dispatch, preventing double-execution.

### New HTTP Endpoints

Five notification endpoints are registered in `internal/infrastructure/http/server.go`:

| Method | Path | Handler |
|---|---|---|
| `GET` | `/api/notifications` | `listNotifications` — supports `?bot=`, `?status=`, `?search=` |
| `GET` | `/api/notifications/count` | `countNotifications` — returns `{"unread": N}` |
| `POST` | `/api/notifications/:id/discuss` | `discussNotification` — appends a `DiscussEntry` |
| `POST` | `/api/notifications/:id/requeue` | `requeueNotification` — prepends discuss as task context and dispatches |
| `DELETE` | `/api/notifications` | `deleteNotifications` — accepts `{"ids": [...]}` |

`POST /api/bots/:name/tasks` is extended: if the request body includes a `schedule` key, it is decoded into a `domain.Schedule` and attached to the created `DirectTask`.

## Buzz (Nostr) Channel Monitor

**Clean Architecture placement:** `domain.RelayClient`/`Event`/`Filter` (`internal/domain/buzz.go`) have zero infrastructure imports. `internal/infrastructure/buzz/` implements the port over [`fiatjaf.com/nostr`](https://pkg.go.dev/fiatjaf.com/nostr) and provides `Monitor`, a `domain.ChannelMonitor` adapter registered via `TeamManager.WithChannelMonitor` exactly like `slack.Monitor` — `internal/application/team` never imports `infrastructure/slack` or `infrastructure/buzz` directly (FR-034; enforced by `grep -r "infrastructure/slack\|infrastructure/buzz" internal/application`).

**Activation.** `cmd/boabot/main.go`'s `buildBuzzMonitor` is a single early guard: its first statement returns `nil` when `buzz.enabled` is false, so no `SecretStore` lookup, no `RelayClient`, and no relay connection is ever attempted (FR-036). When enabled, it resolves the agent's keypair via `buzzinfra.LoadKeypair` (nsec, `SecretRef.Name = "buzz_private_key"`) and fails closed — logs and returns `nil` — on any resolution failure (FR-003), leaving Slack and every other bot in the process to start normally.

**Identity and auth.** NIP-42 challenge/response AUTH (`kind:22242`) is signed by the agent's own key; an optional NIP-OA `auth` tag (owner-issued, Schnorr-signed) can be attached via `AuthTagFunc` to grant NIP-AA virtual channel membership without explicit enrollment. `AUTH` event `created_at` is stamped from wall-clock UTC and checked against the relay's ±120s freshness window before signing (`ErrAuthClockSkew`). Relay AUTH rejections are classified `invalid:`/`restricted:` and never collapsed into one generic failure (`AuthFailureClass`). The owner-attestation tag itself resolves through a fourth `SecretStore` secret, `buzz_auth_tag` (`AuthTagSecretName`/`LoadAuthTag`, `token.go`) — a pipe-delimited `owner_pubkey_hex|conditions|sig_hex` string, parsed and validated against `nipoa.go`'s `SignAuthTag`/`ValidateAuthTag` format before use. `cmd/boabot/main.go`'s `buildBuzzMonitor` resolves it the same way as `buzz_private_key`/`buzz_api_token` and appends `WithAuthTagFunc` to `opts` when present and valid; a missing or malformed secret is logged and the bot continues without owner attestation (log-and-continue, not fail-closed — see `docs/architectural-decision-record.md` ADR-B022).

**Channel participation.** Discovery is entirely dynamic over the authenticated WebSocket (`kind:39000`/`39002`, `kind:44100`/`44101` membership events) — no REST call, no static channel list. Inbound `kind:9` messages that `@mention` the agent's own pubkey, pass the FR-029 author gate, and are not self-authored are dispatched onto the existing `domain.MessageQueue` exactly like a Slack task; `RelayClient.Subscribe` itself rejects (before ever reaching the relay) any `44100`/`44101`/`1059` subscription lacking a matching `#p` filter, and any reaction (`kind:7`) subscription lacking `#h` (FR-016/FR-027). `Monitor.dispatch` also rejects any `kind:9` event whose `Content` exceeds `maxContentLen` (64 KiB, a package constant in `monitor.go`) before it is ever built into a `TaskPayload`, logging a structured `slog.Warn` (`event_id`, `content_len`, `max_content_len`, `channel`) — see ADR-B025 for why this is a constant rather than a `Monitor.Config` field.

**Reconnection and presence.** Bounded exponential backoff with jitter re-dials, re-authenticates, and re-subscribes everything after a disconnect, with no lost pending task-ID correlations. A `kind:20001` presence loop (default well under the FR-023 180s staleness bound) suspends while disconnected and resumes on reconnect; `kind:20002` typing indicators run for the duration of a dispatched task; `Monitor.Stop` publishes an `offline` presence event before the connection closes.

**Subscribe/reconnect/close concurrency (`relay_client.go`/`reconnect.go`).** Each `subEntry` carries a `generation int` (bumped by `attachSub` on every attach) and a per-entry `sync.WaitGroup`. A concurrent `Subscribe` and reconnect-driven `resubscribeAll` can both legitimately attach the same entry; rather than prevent this, every attach is made safe: `pumpSub` compares its captured generation against the entry's live generation before each forward and exits silently once superseded, so only the newest generation ever delivers, and `removeAndClose`/`Close` wait on every generation ever started (not just the latest) before closing `entry.out` — no pump can send on a closed channel. The entry-existence check, generation bump, and `WaitGroup.Add()` all happen inside one `rc.mu`-guarded section, the same lock `Close()` uses to set `rc.closed = true`, which strictly orders attach-vs-close under any interleaving without introducing `atomic.Bool` or any lock nesting between `rc.mu` and `subMu` (exactly one call site, `attachSub`, ever holds both, always `rc.mu` outer/`subMu` inner). See `docs/architectural-decision-record.md` ADR-B023 for the full rationale, including the rejected `atomic.Bool` and continuous-lock-holding alternatives.

**Process-singleton lock (FR-031/OQ-1).** `lock.go`'s `AcquireLock`, keyed on the agent's *derived pubkey* (never the raw nsec) and rooted at the shared `team.ManagerConfig.MemoryRoot` (not a per-bot directory — see `buzzinfra.Config.LockDir`'s doc comment), refuses a second concurrent `Monitor.Start` against the same identity, logs why, and leaves every other channel/bot unaffected. The lock file's content is published atomically: `publishLockFile` writes the PID to a same-directory temp file, `fsync`'s and closes it, then publishes it under the lock's final name via `os.Link` (`EEXIST`-checked) — there is no create-then-write step observable under the final path by a concurrent reader. See ADR-B024 for why `os.Link` was chosen over `os.Rename` (rename provides no mutual exclusion at all) and the precise, bounded evidence for its Windows behavior.

## ACP Stdio Harness Mode

**Clean Architecture placement:** `internal/infrastructure/acp/` implements [`github.com/coder/acp-go-sdk`](https://pkg.go.dev/github.com/coder/acp-go-sdk)'s `acp.Agent` interface as a thin adapter over the existing `domain.Worker`/`WorkerFactory` — no new domain or application interfaces (see ADR-B026). It is entirely independent of `TeamManager`, `ChannelMonitor`, and `MessageQueue`: `boabot -acp` services exactly one persona, driven synchronously by a single peer (`buzz-acp`, or any other ACP host) over stdio, rather than an async multi-bot queue.

**Activation.** `cmd/boabot/main.go`'s `-acp` flag routes to `runACP` instead of the normal `TeamManager.Run` daemon loop. `runACP`/`buildACPAgent` (`cmd/boabot/acp.go`) load `-config`'s path directly as one persona's `config.yaml` — not `team.yaml` — read `SOUL.md` from the same directory, and construct an `application.ExecuteTaskUseCase` using the same provider-factory (`team.NewLocalProviderFactory`), embedder (`bm25.DefaultEmbedder`), memory/vector store (`fs.New`/`vector.New`), and MCP client (`localmcp.NewClient`) construction primitives `TeamManager.startBot` uses for native mode — duplicated at this small scale rather than extracted from `startBot`'s larger, orchestrator-entangled body.

**Protocol surface.** `Agent` implements the full `acp.Agent` interface (11 methods): `Initialize` (advertises no separate auth methods — `buzz-acp` already owns relay authentication), `NewSession`/`Prompt`/`Cancel` (the core turn-execution path), `SetSessionConfigOption`/`SetSessionMode`/`Authenticate` (accepted as no-op successes — `buzz-acp` calls `SetSessionConfigOption` for its permission-mode config option on every session by default), and `Logout`/`CloseSession`/`ListSessions`/`ResumeSession` (return `MethodNotFound`, matching `coder/acp-go-sdk`'s own reference `example/agent`'s treatment of capabilities `Initialize` doesn't advertise).

**Turn execution and the keep-alive mechanism.** `Prompt` (`turn.go`) builds a `domain.Task` from the prompt's text content (already fully assembled by `buzz-acp` — platform context, team instructions, memory, persona system prompt, user message — treated as opaque instruction text) and calls the resolved `Worker.Execute` synchronously. Because `Worker.Execute` blocks with no incremental-output hook, a concurrent goroutine emits `session/update` (`acp::thought`) keep-alive notifications for the duration of the call — sourced from `WithProgressHandler` when the `Worker` offers it (type-asserted via a package-local `progressReporter` interface, detected without changing `domain.Worker`), with a ticker fallback. This is required so `buzz-acp`'s `--idle-timeout` (kills a turn after N seconds of stdout silence) never fires on a long tool-using turn — not a cosmetic streaming nicety. `Prompt` waits for the keep-alive goroutine to fully exit (`sync.WaitGroup`) before emitting the final `acp::stream` update and returning, so no update can arrive after the RPC response.

**Cancellation and panics.** `session/cancel` reads the target session's `context.CancelFunc` under `Agent.mu` (captured into a local variable, then called outside the lock) and cancels the in-flight `Worker.Execute` call; `Prompt` detects `ctx.Err() != nil` afterward and returns `StopReasonCancelled`. A panicking `Worker` is recovered in `runWorkerSafely` and mapped to `StopReasonRefusal` — never a raw process crash.

**Process lifecycle.** `buzz-acp` runs a **persistent pool of agent processes reused across many turns** (confirmed via `strings` on the actual bundled binary — `--agents`/`--lazy-pool`/`--idle-pool-sleep`/respawn-with-backoff), not spawn-per-turn — `boabot -acp` is a long-lived process accordingly, exiting cleanly on stdin EOF.

**Usage/budget reporting.** `PromptResponse.Usage` is left `nil` for v1. `internal/domain/cost.CostEnforcer`/`internal/application/cost.EnforceBudgetUseCase` exist and are fully tested, but `grep` confirms `NewEnforceBudgetUseCase` is constructed nowhere in the application — no budget enforcement is wired into the live task-execution path in native daemon mode either, so there is nothing for ACP mode to source usage figures from without independently wiring cost enforcement into the whole system for the first time (out of scope here).

## Secret Storage

**Clean Architecture placement:** `domain.SecretStore`/`SecretProvider`/`SecretRef` (`internal/domain/secret.go`) have zero keystore/D-Bus/OS-specific imports. `internal/infrastructure/secret.Store` implements the ordered-chain resolution; `env/`, `file/`, `systemd/`, `keystore/` are the four provider adapters.

**Resolution order and semantics.** Default chain: `env` → `systemd` → `keystore` → `file` (FR-040, configurable, any provider omissible). `Store.Get` tries each in order with its own `context.WithTimeout` (2s default, one deadline per provider, not per chain) and treats a provider error or timeout as a miss, not a chain-halting failure (FR-039). An all-miss resolution returns `*secret.NotFoundError`, naming the reference and every provider consulted (FR-053).

**Per-bot namespacing (FR-045, OQ-9 resolved as bot *name*, not type):** `env` ignores `SecretRef.Bot` (env vars are process-global; `BUZZ_PRIVATE_KEY` resolves `buzz_private_key` regardless of bot). `file`/`systemd` use the filename/INI-key `"<bot>_<name>"` when `Bot` is set. `keystore` uses service `"boabot"` + account `"<bot>/<name>"`. All three bot-scoped providers are **strict-match with no fallback to the global key** — a per-bot secret absent under its bot-scoped key is a miss, never a silent match against a differently-scoped entry.

**Never logged (FR-051, FR-052).** No provider logs a resolved value at any level; `Store` itself logs only the provider name and reference name. `keystore`'s writes go through `zalando/go-keyring`'s stdin (`-i`) path on macOS, never a subprocess argument — mechanically verified by a call-inspection test.

## Key Design Decisions

See [`architectural-decision-record.md`](architectural-decision-record.md) for module-specific ADRs. See root [`docs/architectural-decision-record.md`](../../docs/architectural-decision-record.md) for system-level decisions.
