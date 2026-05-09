# Architecture: Bot Capability Access

## Overview

This feature touches four areas:

1. **Plugin store pre-resolution** — move from goroutine-written struct fields to pre-computed local variables in `TeamManager.Run`.
2. **`read_skill` MCP tool** — new builtin in `mcp/client.go`; `callPluginTool` routing update.
3. **`CLIAgentRunner` domain interface + `cliagent` infrastructure package** — clean architecture for subprocess CLI delegation.
4. **Config additions** — `CLIToolsConfig` on `OrchestratorConfig`.

---

## Package Layout Changes

### New package: `internal/infrastructure/cliagent/`

```
boabot/internal/infrastructure/cliagent/
├── runner.go         # SubprocessRunner implementing domain.CLIAgentRunner
└── runner_test.go    # Unit tests (exec fake / real subprocess patterns)
```

No external dependencies beyond Go stdlib (`os/exec`, `bufio`, `syscall`, `context`, `time`).

**Clean architecture:** `cliagent` is infrastructure. It imports `domain.CLIAgentRunner` and `domain.CLIAgentConfig`. It does not import any other infrastructure package.

### Modified: `internal/infrastructure/codeagent/`

```
boabot/internal/infrastructure/codeagent/
├── stream.go         # ParseStreamLine (extracted from provider.go), streamEvent, deltaField
├── provider.go       # Unchanged logic; calls ParseStreamLine instead of extractText
└── provider_test.go  # Existing tests; add test for ParseStreamLine
```

### Modified: `internal/infrastructure/local/mcp/client.go`

- Add `cliRunner domain.CLIAgentRunner` and `cliTools config.CLIToolsConfig` fields.
- Add `WithCLIRunner` and `WithCLITools` option functions.
- Add `read_skill` to `ListTools` output (when `pluginStore != nil`).
- Add four CLI tools to `ListTools` output (when each is enabled and binary resolves).
- Add `readSkill` method.
- Add `callCLITool` method dispatching by tool name.
- Update `callPluginTool` to detect JSON entrypoints and delegate to `readSkill`.
- Update `CallTool` switch to include `"read_skill"` and the four CLI tool names.

### Modified: `internal/infrastructure/config/config.go`

- Add `CLIToolConfig` struct.
- Add `CLIToolsConfig` struct.
- Add `CLITools CLIToolsConfig` field to `OrchestratorConfig`.

### Modified: `internal/application/team/team_manager.go`

- Move plugin store pre-resolution from `startBot` to `Run`.
- Pass plugin store values as parameters to `startBot` (new signature).
- Remove `tm.pluginStore` and `tm.pluginInstallDir` struct fields (or keep but never write from goroutines).
- Wire `cliRunner` and `cliTools` into MCP client construction.

### New domain file: `internal/domain/cliagent.go`

```
boabot/internal/domain/cliagent.go    # CLIAgentConfig, CLIAgentRunner
```

---

## Component Interaction Diagram

```
TeamManager.Run()
    ├── pre-resolve: pluginStore, pluginInstallDir (from orchestrator config)
    ├── pre-resolve: cliRunner = cliagent.New()
    ├── pre-resolve: cliTools = botCfg.Orchestrator.CLITools
    └── per bot goroutine: startBot(ctx, entry, orchName, pluginStore, installDir, cliRunner, cliTools)
            └── localmcp.NewClient(allowedDirs,
                    WithPluginStore(pluginStore),
                    WithInstallDir(installDir),
                    WithCLIRunner(cliRunner),
                    WithCLITools(cliTools))

mcp.Client.ListTools()
    ├── builtin tools (always)
    ├── complete_board_item (if boardStore != nil)
    ├── read_skill (if pluginStore != nil)
    ├── active plugin tools (from pluginStore.List)
    ├── run_claude_code (if cliTools.ClaudeCode.Enabled && LookPath(binary) succeeds)
    ├── run_codex      (if cliTools.Codex.Enabled && LookPath(binary) succeeds)
    ├── run_openai_codex (if cliTools.OpenAICodex.Enabled && LookPath(binary) succeeds)
    └── run_opencode   (if cliTools.OpenCode.Enabled && LookPath(binary) succeeds)

mcp.Client.CallTool(name, args)
    ├── callPluginTool() → if JSON entrypoint → readSkill()
    ├── "read_file"            → readFile()
    ├── "write_file"           → writeFile()
    ├── "create_dir"           → createDir()
    ├── "list_dir"             → listDir()
    ├── "run_shell"            → runShell()
    ├── "complete_board_item"  → completeBoardItem()
    ├── "read_skill"           → readSkill()
    ├── "run_claude_code"      → callCLITool("claude_code", args)
    ├── "run_codex"            → callCLITool("codex", args)
    ├── "run_openai_codex"     → callCLITool("openai_codex", args)
    └── "run_opencode"         → callCLITool("opencode", args)

callCLITool(toolID, args)
    └── cliRunner.Run(ctx, cfg, instruction, nil, progress)
            └── SubprocessRunner.Run()
                    ├── exec.LookPath(cfg.Binary) — fail fast if not found
                    ├── exec.Command(binary, cfg.Args..., instruction)
                    ├── cmd.Cancel = SIGTERM
                    ├── cmd.WaitDelay = 5s
                    ├── goroutine: bufio.Scanner on stdout → progress(line), accumulate
                    ├── goroutine: drain stdin channel → write to cmd stdin pipe
                    └── cmd.Wait() → return accumulated output

readSkill(ctx, name)
    └── pluginStore.List() → find active plugin with tool.Name == name
            └── os.ReadFile(<installDir>/<pluginName>/commands/<name>.md)
                    └── return Markdown content

callPluginTool(ctx, name, args)
    └── pluginStore.List() → find active plugin
            ├── if entrypoint ends with "plugin.json" → readSkill(ctx, name)
            └── else → exec.CommandContext(entrypoint) [existing path]
```

---

## Changes to `team_manager.go` — Plugin Store Pre-Resolution

### Current (racy) approach

`startBot` is called from goroutines and writes `tm.pluginStore` and `tm.pluginInstallDir` when it encounters the orchestrator config. Non-orchestrator goroutines may read these fields before the orchestrator goroutine writes them.

### New approach

In `Run()`, before the goroutine loop:

```go
// Pre-resolve plugin store before launching goroutines (eliminates data race).
var resolvedPluginStore domain.PluginStore
var resolvedInstallDir string
for _, e := range teamCfg.Team {
    if !e.Enabled || !e.Orchestrator {
        continue
    }
    orchCfgPath := filepath.Join(tm.cfg.BotsDir, e.Type, "config.yaml")
    orchCfg, err := config.Load(orchCfgPath)
    if err != nil {
        slog.Warn("cannot load orchestrator config for plugin pre-resolution", "err", err)
        break
    }
    installDir := orchCfg.Orchestrator.Plugins.InstallDir
    if installDir == "" {
        break
    }
    memPath := filepath.Join(tm.cfg.MemoryRoot, e.Name)
    if !filepath.IsAbs(installDir) {
        installDir = filepath.Join(memPath, installDir)
    }
    if ps, psErr := localplugin.NewLocalPluginStore(installDir); psErr == nil {
        resolvedPluginStore = ps
        resolvedInstallDir = installDir
    }
    break
}

// Also pre-resolve CLI runner and tools config.
cliRunner := cliagent.New()
var cliTools config.CLIToolsConfig
// ... load from orchestrator config if available ...
```

The goroutine closure captures `resolvedPluginStore` and `resolvedInstallDir` as immutable local variables. No struct fields are written from goroutines.

### `startBot` signature change

```go
func (tm *TeamManager) startBot(
    ctx context.Context,
    entry BotEntry,
    orchestratorName string,
    pluginStore domain.PluginStore,   // pre-resolved, may be nil
    installDir string,                  // pre-resolved, may be ""
    cliRunner domain.CLIAgentRunner,
    cliTools config.CLIToolsConfig,
) error
```

The `botRunner` function type in the struct must be updated to match. `export_test.go` must be updated accordingly.

---

## Thread Safety Considerations

### Plugin store reads
`pluginStore.List()` is called from `ListTools` and `CallTool`, both of which run in a single bot goroutine (bots process tasks sequentially). The `LocalPluginStore` is created once and passed immutably to all bots — it must be safe for concurrent reads. If `LocalPluginStore` uses a mutex for its own state, this is already handled. If not, multiple bots calling `List()` concurrently is safe if `List()` is read-only (filesystem reads, no shared mutable state). This should be verified during implementation.

### `CLIAgentRunner`
`SubprocessRunner` has no shared mutable state. Each `Run()` call is independent. Safe for concurrent use from multiple bots.

### `exec.LookPath` in `ListTools`
`exec.LookPath` is safe for concurrent calls (reads the PATH environment variable, no shared mutable state).

### MCP client struct fields (`cliRunner`, `cliTools`)
These are set once at construction time and only read thereafter. No synchronisation needed.

---

## Clean Architecture Compliance

| Layer | Package | Imports |
|---|---|---|
| Domain | `internal/domain/cliagent.go` | `context`, `time` only |
| Infrastructure | `internal/infrastructure/cliagent/` | `domain`, stdlib (`os/exec`, `bufio`, `syscall`) |
| Infrastructure | `internal/infrastructure/local/mcp/` | `domain`, `config`, `cliagent` |
| Infrastructure | `internal/infrastructure/codeagent/stream.go` | `encoding/json` only |
| Application | `internal/application/team/team_manager.go` | `domain`, `config`, `cliagent` (for construction only) |

**Prohibited imports:** `os/exec` must not appear in `internal/domain/` or `internal/application/` packages. The `cliagent` infrastructure package is the only place that imports `os/exec`.

---

## Binary Gating in `ListTools`

For each enabled CLI tool, `ListTools` checks binary availability at call time:

```go
func resolveBinary(cfg config.CLIToolConfig, defaultName string) (string, bool) {
    if !cfg.Enabled {
        return "", false
    }
    bin := cfg.BinaryPath
    if bin == "" {
        bin = defaultName
    }
    if filepath.IsAbs(bin) {
        _, err := os.Stat(bin)
        return bin, err == nil
    }
    resolved, err := exec.LookPath(bin)
    return resolved, err == nil
}
```

Tools that fail resolution are silently omitted from `ListTools`. This is a normal condition and must not log errors.

---

## Progress Function Wiring

The MCP `CallTool` API (`CallTool(ctx, name, args)`) does not currently carry a progress callback. To route CLI tool progress to the operator UI, the `Client` struct will hold a `progressFn func(line string)` field:

```go
type Client struct {
    // ...
    progressFn func(line string) // optional; called for each output line from CLI tools
}

func WithProgressFn(fn func(line string)) func(*Client) {
    return func(c *Client) { c.progressFn = fn }
}
```

In `team_manager.go`, during MCP client construction, wire the progress handler:

```go
taskProgressFn := func(line string) {
    // call worker's progress handler with taskID
    // taskID is not available at construction time — use a closure that
    // captures the current task ID from a shared atomic or from context.
}
mcpOpts = append(mcpOpts, localmcp.WithProgressFn(taskProgressFn))
```

**Alternative (simpler):** The bot's `ExecuteTaskUseCase` calls `CallTool` and can wrap it to inject progress. This avoids threading `progressFn` through the MCP client. However, the cleanest approach is the `WithProgressFn` option, which keeps the MCP client self-contained.

**Decision:** Use `WithProgressFn` on the MCP client. The task ID is carried in context (or the progress fn is set per-task). Architecture Phase will confirm the exact threading mechanism.

---

## `read_skill` Implementation in `mcp/client.go`

```go
func (c *Client) readSkill(ctx context.Context, args map[string]any) (domain.MCPToolResult, error) {
    name, _ := args["name"].(string)
    if name == "" {
        return errResult("read_skill: missing required argument \"name\""), nil
    }
    if c.pluginStore == nil {
        return errResult("read_skill: plugin store not available"), nil
    }
    plugins, err := c.pluginStore.List(ctx)
    if err != nil {
        return errResult(fmt.Sprintf("read_skill: list plugins: %v", err)), nil
    }
    for _, p := range plugins {
        if p.Status != domain.PluginStatusActive {
            continue
        }
        for _, t := range p.Manifest.Provides.Tools {
            if t.Name != name {
                continue
            }
            mdPath := filepath.Join(c.installDir, p.Name, "commands", name+".md")
            data, readErr := os.ReadFile(mdPath)
            if readErr != nil {
                return errResult(fmt.Sprintf("read_skill: read %s: %v", mdPath, readErr)), nil
            }
            return okResult(string(data)), nil
        }
    }
    return errResult(fmt.Sprintf("skill %q not found in any active plugin", name)), nil
}
```

The `callPluginTool` update to detect JSON entrypoints:

```go
// Check if entrypoint is a Claude Code plugin JSON (not executable).
if strings.HasSuffix(p.Manifest.Entrypoint, "plugin.json") {
    // Delegate to read_skill resolution.
    return c.readSkill(ctx, map[string]any{"name": name})
}
```
