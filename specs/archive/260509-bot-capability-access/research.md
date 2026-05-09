# Research: Bot Capability Access

## 1. Plugin Store Race — Exact Location and Fix

### Where `tm.pluginStore` is written (the race)

File: `boabot/internal/application/team/team_manager.go`

The `pluginStore` and `pluginInstallDir` struct fields are written inside `startBot`, which is called from `runBotWithRestart` → goroutines launched in the `Run()` loop (lines ~272–283). All bot goroutines start concurrently.

```go
// lines ~597–599 in team_manager.go (inside startBot, inside the orchestrator block)
tm.pluginStore = ps
tm.pluginInstallDir = pluginInstallDir
```

These writes occur deep inside `startBot`, which is the goroutine function. All other bots (reviewer, developer, etc.) also call `startBot` concurrently. When a non-orchestrator bot reaches the MCP-client wiring block (lines ~708–715):

```go
if tm.pluginStore != nil {
    mcpOpts = append(mcpOpts, localmcp.WithPluginStore(tm.pluginStore))
    mcpOpts = append(mcpOpts, localmcp.WithInstallDir(tm.pluginInstallDir))
}
```

...the orchestrator goroutine may not yet have written `tm.pluginStore`. The Go race detector will flag this as a concurrent read-write on the same struct field without synchronisation.

### Root cause

The struct field `tm.pluginStore` is shared state written by one goroutine (orchestrator's `startBot`) and read by N other goroutines (all other `startBot` calls). There is no mutex protecting these reads and writes.

### Fix approach (FR-1)

Pre-resolve the plugin store in `Run()` before launching any goroutines. The orchestrator bot entry can be identified at this point by loading its config file and checking `botCfg.Orchestrator.Plugins.InstallDir`. The resolved `pluginStore` and `pluginInstallDir` are then captured in local variables closed over by all goroutines, eliminating any writes to shared struct fields from goroutines.

**Concrete change:**
1. In `Run()`, iterate `teamCfg.Team` to find the orchestrator entry.
2. Load that bot's `config.yaml` (just for the plugin config).
3. If `InstallDir` is set, build the `LocalPluginStore` and resolve the path.
4. Pass these as parameters to `startBot` (or update `ManagerConfig` to carry them).
5. Remove the struct-field writes `tm.pluginStore = ps` and `tm.pluginInstallDir = pluginInstallDir` from inside `startBot`.

The `tm.pluginStore` and `tm.pluginInstallDir` struct fields may then be removed entirely; the values are local to `Run()` and passed down.

---

## 2. Full `callPluginTool` Flow in `mcp/client.go`

File: `boabot/internal/infrastructure/local/mcp/client.go`

### Current flow

1. `CallTool` checks if `c.pluginStore != nil`, then calls `callPluginTool(ctx, name, args)`.
2. `callPluginTool` calls `c.pluginStore.List(ctx)` to get all plugins.
3. Iterates plugins; skips if `p.Status != domain.PluginStatusActive`.
4. Iterates `p.Manifest.Provides.Tools`; looks for `t.Name == name`.
5. On match, resolves `pluginDir = filepath.Join(c.installDir, p.Name)` and `entrypoint = filepath.Join(pluginDir, p.Manifest.Entrypoint)`.
6. Checks if entrypoint file exists via `os.Stat`.
7. Marshals `args` to JSON, sets as `cmd.Stdin`.
8. Runs `exec.CommandContext(callCtx, entrypoint)` with `pluginTimeout` (30s).
9. Decodes stdout as `domain.MCPToolResult` (JSON).
10. Returns the decoded result.

### What happens when entrypoint is non-executable

Claude Code plugins store `.claude-plugin/plugin.json` as their entrypoint — a JSON file, not an executable. When `exec.CommandContext` tries to execute a JSON file, the OS returns `exec format error` (ENOEXEC on Linux, "exec format error" on macOS). `cmd.Run()` returns this as an error, which becomes:

```
plugin "dev-flow" exited with error: exec format error
stderr: 
```

This is silently returned as an `IsError: true` MCPToolResult with the error message. No panic, but the tool call fails with an unhelpful message.

### Fix approach (FR-2)

After resolving `entrypoint`, check if it is a `.claude-plugin/plugin.json` file. Detection:
- Check if `filepath.Base(entrypoint) == "plugin.json"`, OR
- Check if `p.Manifest.Entrypoint` ends with `.json`.

If detected as non-executable JSON, delegate to `readSkill(ctx, name)` rather than calling `exec.Command`.

---

## 3. How `ListTools` Currently Works (builtin + plugin merging)

File: `boabot/internal/infrastructure/local/mcp/client.go`, function `ListTools`.

1. Builds a hardcoded slice of builtin tools: `read_file`, `write_file`, `create_dir`, `list_dir`, `run_shell`.
2. Conditionally appends `complete_board_item` if `c.boardStore != nil`.
3. If `c.pluginStore != nil`:
   a. Calls `c.pluginStore.List(context.Background())`.
   b. Builds a `seen` map seeded with all builtin tool names → `"builtin"`.
   c. For each active plugin, iterates `p.Manifest.Provides.Tools`.
   d. Skips tools whose names are in `seen` (collision deduplication).
   e. Appends non-colliding plugin tools to the slice.
4. Returns the merged tool slice.

The `read_skill` tool will be added as a builtin (step 2) only when `c.pluginStore != nil`, so it conditionally appears alongside the plugin tools it serves.

---

## 4. How `codeagent.Provider` Invokes CLIs

File: `boabot/internal/infrastructure/codeagent/provider.go`

### Claude dialect (`DialectClaude`)

Args: `["--output-format=stream-json", "--dangerously-skip-permissions", "-p", instruction]`

Output parsing — streaming JSON, line by line via `bufio.Scanner`:
- Each line is passed to `extractText(line string) (string, bool)`.
- `extractText` unmarshals the line into `streamEvent{Type, Delta, Result}`.
- If `Type == "content_block_delta"` and `Delta.Type == "text_delta"`: returns `Delta.Text`.
- If `Type == "result"` and `Result != ""`: returns `Result`.
- All other event types: returns `""` with `ok=true` (non-error skip).
- Unmarshal failures return `"", false` (silently skipped).
- Accumulated content is joined without separators.

**Reuse plan for FR-4:** Extract `extractText`, `streamEvent`, `deltaField` into `internal/infrastructure/codeagent/stream.go` and export `ParseStreamLine(line string) (string, bool)`. The existing `provider.go` calls the extracted function. `run_claude_code` also calls it via the `CLIAgentRunner` with post-processing.

### Codex dialect (`DialectCodex`)

Args: `["-q", "--approval-mode=full-auto", instruction]`

Output parsing: plain-text, line by line via `bufio.Scanner`. Each non-empty line is appended to the accumulated string with a newline. No JSON parsing.

**Reuse plan for FR-5:** `CLIAgentRunner` accumulates lines; the Codex tool passes through plain lines directly.

### `WaitDelay`

Both dialects set `cmd.WaitDelay = 500 * time.Millisecond` on the `exec.Cmd`. This ensures that stdout/stderr pipes are force-closed 500ms after context cancellation, preventing goroutine leaks when child processes outlive the parent.

---

## 5. How `progressFn` Is Called During Task Execution

File: `boabot/internal/application/team/team_manager.go`, function `startBot`, around lines 847–864.

```go
worker.WithProgressHandler(func(taskID, line string) {
    task, getErr := sharedTasks.Get(context.Background(), taskID)
    if getErr != nil {
        return
    }
    task.Output += line + "\n"
    _, _ = sharedTasks.Update(context.Background(), task)

    if task.Source == domain.DirectTaskSourceBoard && tm.sharedBoard != nil {
        items, listErr := tm.sharedBoard.List(...)
        if listErr == nil && len(items) > 0 {
            item := items[0]
            item.LastResult += line + "\n"
            _, _ = tm.sharedBoard.Update(context.Background(), item)
        }
    }
})
```

The `progressFn` in `CLIAgentRunner` must match the signature expected by the task execution context. When a bot calls a CLI tool, it receives the runner's `progress func(line string)` which should ultimately call the task-level progress handler to stream output to the UI.

The wiring: `ExecuteTaskUseCase` exposes `WithProgressHandler(fn func(taskID, line string))`. Inside the tool call, the MCP client receives a context that includes the task ID. The CLI tool implementation will receive a `progress(line string)` callback from the MCP client dispatch layer.

**Key finding:** The MCP `CallTool` API does not currently pass a progress callback. To wire `progress` from the MCP tool call into the bot's progress handler, the `Client.CallTool` will need to pass the progress function, or the CLI runner will log progress to `slog` which is picked up by the existing logging pipeline. The cleanest approach: add a `progressFn` field to the `Client` struct, set during construction, and pass it to CLI tool invocations. Alternatively, context-carry the task ID and have the MCP client look up the progress handler. This design choice must be resolved in the architecture phase.

---

## 6. How AskChannel / drainAsks Works

File: `boabot/internal/application/team/team_manager.go`, lines ~739 and ~879–904.

The `teamAskRouter` manages per-bot buffered channels (`chan domain.AskRequest`, buffer 10):

```go
type teamAskRouter struct {
    mu  sync.RWMutex
    chs map[string]chan domain.AskRequest
}
```

`Enqueue(botName, req)` does a non-blocking send to the channel (select with default). Returns `false` if no channel exists or channel is full.

The bot's `Execute` loop reads from this channel between tool calls via `worker.WithAskChannel(...)`. This is the model for how `stdin <-chan string` in `CLIAgentRunner` should behave: a non-blocking channel read with a select statement that includes a `default` arm and `ctx.Done()` arm.

**Pattern for CLIAgentRunner stdin goroutine:**
```go
go func() {
    for {
        select {
        case line, ok := <-stdin:
            if !ok {
                return
            }
            _, _ = stdinPipe.Write([]byte(line + "\n"))
        case <-ctx.Done():
            return
        }
    }
}()
```

This goroutine exits when stdin is closed or context is cancelled, and never blocks the main task loop.

---

## 7. Config Struct — Where to Add `cli_tools`

File: `boabot/internal/infrastructure/config/config.go`

Current `OrchestratorConfig`:
```go
type OrchestratorConfig struct {
    Enabled       bool
    APIPort       int
    JWTSecret     string
    AdminPassword string
    WorkDirs      []string
    RetentionDays int
    MaxConcurrent int
    Plugins       PluginsConfig
}
```

New additions:
```go
type OrchestratorConfig struct {
    // ... existing fields ...
    CLITools CLIToolsConfig `yaml:"cli_tools"`
}

type CLIToolsConfig struct {
    ClaudeCode  CLIToolConfig `yaml:"claude_code"`
    Codex       CLIToolConfig `yaml:"codex"`
    OpenAICodex CLIToolConfig `yaml:"openai_codex"`
    OpenCode    CLIToolConfig `yaml:"opencode"`
}

type CLIToolConfig struct {
    Enabled    bool   `yaml:"enabled"`
    BinaryPath string `yaml:"binary_path"`
}
```

The `config.Load` function uses `dec.KnownFields(true)`, so these new struct fields must be added to avoid YAML decode errors on configs that include them. Configs that omit `cli_tools` will get zero-value structs (all disabled, empty binary paths).

**Binary path defaults:** If `BinaryPath == ""`, default to the CLI tool's canonical name (`"claude"`, `"codex"`, `"openai-codex"`, `"opencode"`). This defaulting happens at the `ListTools` / `CallTool` level, not in the config loader.

---

## 8. Plugin Install Directory Structure

From `boabot/internal/infrastructure/local/plugin/` (inferred from usage in `team_manager.go` and `client.go`):

```
<install_dir>/
  <plugin-name>/
    plugin.json         # or plugin.yaml manifest
    .claude-plugin/
      plugin.json       # Claude Code plugin metadata (this is the entrypoint field)
    commands/
      <skill-name>.md   # Markdown instructions for each skill
    run.sh              # Optional executable entrypoint (for non-Claude-Code plugins)
```

For Claude Code plugins (like `dev-flow`):
- `Manifest.Entrypoint` = `.claude-plugin/plugin.json` (a JSON metadata file)
- `Manifest.Provides.Tools` lists skills like `review-code`, `create-prd`, `implm-from-spec`
- The actual instructions are in `commands/<skill-name>.md`

The `read_skill` tool reads `<install_dir>/<plugin-name>/commands/<skill-name>.md`.

**Lookup:** Given skill name `"review-code"`, `read_skill` must:
1. Find which active plugin provides a tool named `"review-code"` (iterate `pluginStore.List`).
2. Use that plugin's `Name` to construct the path: `<installDir>/<plugin.Name>/commands/review-code.md`.

---

## 9. Go `exec.Cmd` Patterns Needed

### stdout/stderr pipes
```go
stdout, err := cmd.StdoutPipe()
// OR
var stderrBuf strings.Builder
cmd.Stderr = &stderrBuf
```

For the `CLIAgentRunner`, pipe stdout for line-by-line reading and capture stderr for error reporting:
```go
stdout, _ := cmd.StdoutPipe()
var stderrBuf strings.Builder
cmd.Stderr = &stderrBuf
```

### WaitDelay
```go
cmd.WaitDelay = 500 * time.Millisecond
```
Set on the cmd before `Start()`. This causes `Wait()` to forcibly close pipes if the process doesn't exit within 500ms after context cancellation. Already used in `codeagent/provider.go`.

### SIGTERM → SIGKILL pattern
```go
// Context cancel sends SIGTERM via exec.CommandContext.
// For explicit SIGKILL after grace period:
go func() {
    select {
    case <-time.After(5 * time.Second):
        _ = cmd.Process.Kill()
    case <-done: // process exited normally
    }
}()
```

`exec.CommandContext` sends SIGKILL immediately when the context is cancelled (on Linux). To implement a SIGTERM-first approach with a grace period, we must NOT use `exec.CommandContext` directly but instead use `exec.Command` with manual context watching:
```go
cmd := exec.Command(binary, args...)
// ... setup pipes ...
_ = cmd.Start()
done := make(chan struct{})
go func() { defer close(done); _ = cmd.Wait() }()

select {
case <-ctx.Done():
    _ = cmd.Process.Signal(syscall.SIGTERM)
    select {
    case <-time.After(5 * time.Second):
        _ = cmd.Process.Kill()
    case <-done:
    }
case <-done:
}
```

Alternatively, use `exec.CommandContext` with `cmd.WaitDelay` set to 5 seconds plus `cmd.Cancel` set to send SIGTERM instead of SIGKILL:
```go
cmd := exec.CommandContext(ctx, binary, args...)
cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
cmd.WaitDelay = 5 * time.Second
```
This is available from Go 1.20+. This is the recommended approach given the project uses Go 1.26.

### Goroutine leak prevention
Always drain stdout scanner goroutine even on context cancel. Use the `done` channel pattern: scanner goroutine signals completion. The main goroutine either reads all output or is cancelled; in either case, `cmd.Wait()` (or the done channel) ensures cleanup.

---

## Key Findings Summary

1. **Race location:** `tm.pluginStore` and `tm.pluginInstallDir` written in `startBot` goroutine (~line 597), read in other `startBot` goroutines (~line 712). Fix: pre-resolve in `Run()` before goroutine launch.

2. **Non-executable entrypoint:** Claude Code plugins set `Entrypoint: ".claude-plugin/plugin.json"`. `exec.Command` on a `.json` file returns `exec format error`. Detection: check if `filepath.Base(entrypoint) == "plugin.json"`.

3. **Streaming JSON parser:** `extractText` + `streamEvent` + `deltaField` in `codeagent/provider.go` — extract to `codeagent/stream.go` and export as `ParseStreamLine`.

4. **Progress wiring:** MCP `CallTool` does not currently pass a progress callback. Will need to add `progressFn func(line string)` to `Client` struct or pass via context.

5. **stdin goroutine model:** Follow the `teamAskRouter` non-blocking select pattern for the CLI runner's stdin forwarder.

6. **WaitDelay + SIGTERM:** Use Go 1.20+ `cmd.Cancel` + `cmd.WaitDelay` for clean SIGTERM-first graceful shutdown.

7. **Binary gating:** `exec.LookPath` at `ListTools` call time, not at startup. Already the right location since `ListTools` is called per-task.

8. **Config:** `OrchestratorConfig` needs `CLITools CLIToolsConfig` added; `dec.KnownFields(true)` requires all YAML fields to be in the struct.
