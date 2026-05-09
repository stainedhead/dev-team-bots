# Implementation Notes: Bot Capability Access

## Decisions and Gotchas to Record Before Implementation Begins

---

### 1. Plugin Race: Orchestrator Config Pre-Loading

**Problem:** `tm.pluginStore` and `tm.pluginInstallDir` are written inside `startBot`, which runs in goroutines. All bot goroutines start concurrently. Non-orchestrator bots can read `nil` from `tm.pluginStore` before the orchestrator goroutine writes it.

**Fix:** In `TeamManager.Run()`, before the goroutine loop, scan `teamCfg.Team` for the orchestrator entry (`e.Orchestrator && e.Enabled`). Load that bot's `config.yaml`, check `Plugins.InstallDir`. If set, call `localplugin.NewLocalPluginStore(installDir)` immediately and capture the result in a local variable.

**Key detail:** The orchestrator bot entry is identified by the `Orchestrator: true` flag in `team.yaml` (`BotEntry.Orchestrator`). The `TeamManager.Run()` method already finds `orchestratorName` by iterating this list — extend that block to also load the config.

**Pitfall to avoid:** Do not load the full orchestrator config just for the plugin store if it is expensive. The config load is a simple YAML file read — this is acceptable. If the file read fails (orchestrator bot has no config yet), log a warning and leave the store nil.

**Struct fields:** After the fix, `tm.pluginStore` and `tm.pluginInstallDir` struct fields should be removed entirely. They were only used to pass data from the orchestrator's `startBot` goroutine to other `startBot` goroutines — a pattern that was never safe. With pre-resolution, the data flows as parameters.

**`botRunner` type change:** The `botRunner` field in `TeamManager` has type `func(ctx context.Context, entry BotEntry, orchestratorName string) error`. This must be updated to include the new parameters. Check `export_test.go` — it assigns a replacement `botRunner` for testing; this must also be updated to match the new signature.

---

### 2. `read_skill`: Non-Executable Entrypoint Detection

**Detection strategy:** Check if `p.Manifest.Entrypoint` ends with `"plugin.json"` using `strings.HasSuffix(p.Manifest.Entrypoint, "plugin.json")`. This matches:
- `.claude-plugin/plugin.json`
- `plugin.json`
- Any path ending in `plugin.json`

An alternative is checking the file's MIME type or attempting `exec.LookPath`, but the suffix check is sufficient for Claude Code plugins and avoids filesystem stat calls.

**Fallback:** The existing exec path is NOT removed — it still handles plugins that do have real executable entrypoints (`run.sh` etc.). Only the `plugin.json` suffix triggers the `readSkill` path. The existing `TestMCPClient_CallTool_DispatchesToPluginEntrypoint` test (using `run.sh`) must continue to pass.

**`read_skill` in `ListTools`:** Only append the `read_skill` tool definition when `c.pluginStore != nil`. The tool should appear in the list even if no active plugins currently provide skills — a bot may call `read_skill` proactively to check, and will receive a clear error message rather than "unknown tool".

**Error messages:**
- Unknown skill: `skill "<name>" not found in any active plugin`
- File read error: `read_skill: read <path>: <os error>`
- Plugin store nil: `read_skill: plugin store not available`
- Missing `name` arg: `read_skill: missing required argument "name"`

---

### 3. `CLIAgentRunner` Stdin Channel: Non-Blocking Select

**Problem:** If the CLI subprocess never reads from its stdin (most CLI tools do not), a blocking write to `stdinPipe` will hang indefinitely.

**Solution:** The stdin goroutine uses non-blocking channel semantics:
```go
go func() {
    defer stdinPipe.Close()
    for {
        select {
        case line, ok := <-stdin:
            if !ok {
                return // channel closed
            }
            _, _ = io.WriteString(stdinPipe, line+"\n")
        case <-ctx.Done():
            return
        }
    }
}()
```

This goroutine blocks on the channel select, not on the pipe write. If the subprocess's stdin buffer fills and the pipe write blocks, the goroutine stalls — but this is only a problem if the subprocess is not reading stdin at all AND the caller is writing to the channel. In normal usage (no stdin needed), the channel is nil and no goroutine is started.

**When `stdin == nil`:** Do not start the stdin goroutine. Do not open `cmd.StdinPipe()`. Leave `cmd.Stdin` unset (subprocess stdin is `/dev/null` by default with `exec.Command`).

**Test case for nil stdin:** Use `echo hello` as the subprocess (no stdin needed). Pass `stdin: nil`. Assert the subprocess completes and returns `"hello\n"`. This test must pass without blocking.

---

### 4. `WaitDelay` on `exec.Cmd`: SIGTERM + 5s Grace Period

**Go 1.20+ approach:** Use `cmd.Cancel` and `cmd.WaitDelay`:
```go
cmd := exec.CommandContext(ctx, binary, args...)
cmd.Cancel = func() error {
    return cmd.Process.Signal(syscall.SIGTERM)
}
cmd.WaitDelay = 5 * time.Second
```

When `ctx` is cancelled:
1. `cmd.Cancel` is called — sends SIGTERM.
2. `cmd.WaitDelay` starts — gives the process 5 seconds to exit gracefully.
3. After 5 seconds, `exec` force-closes the I/O pipes and sends SIGKILL.

**Timeout implementation:** Set `cmd.WaitDelay = 5 * time.Second` for SIGTERM grace. For the overall timeout, wrap the passed context:
```go
timeout := cfg.Timeout
if timeout <= 0 {
    timeout = 30 * time.Minute
}
runCtx, cancel := context.WithTimeout(ctx, timeout)
defer cancel()
cmd := exec.CommandContext(runCtx, ...)
```

This means: overall timeout cancels context → SIGTERM → 5s grace → SIGKILL.

**On macOS vs Linux:** `syscall.SIGTERM` works on both. The `cmd.Cancel` approach is cross-platform for Go 1.20+.

---

### 5. Streaming JSON Parser Reuse: Extract to `stream.go`

**What to extract:**
- `streamEvent` struct
- `deltaField` struct
- `extractText(line string) (string, bool)` → becomes `ParseStreamLine(line string) (string, bool)`

**Export:** `ParseStreamLine` is exported (capital P) so the `mcp` package can import it from the `codeagent` package. The function signature does not change; only the name and export status change.

**Import path:** `internal/infrastructure/codeagent` is already imported by `team/provider_factory.go`. The `mcp/client.go` will need to import it as well. This is infrastructure → infrastructure, which is acceptable (both are in the infra layer).

**Alternative:** If the `mcp` package importing `codeagent` feels like coupling, the parse function can be moved to a more neutral shared package (`internal/infrastructure/jsonstream/` or similar). However, since both packages are infrastructure and the function is specific to Claude's stream format, keeping it in `codeagent/` is the right semantic home.

**`provider.go` after extraction:** The `Invoke` method calls `ParseStreamLine` instead of `extractText`. The unexported `extractText` function is deleted. No other changes to `provider.go`.

---

### 6. Binary Gating: `exec.LookPath` at `ListTools` Call Time

**Why at call time:** `ListTools` is called per-task (the worker calls it before each agent loop). Checking at startup would mean that installing a CLI binary after startup requires a restart. Checking at `ListTools` time makes the tool appear automatically once the binary is installed.

**Implementation:**
```go
func (c *Client) resolveBinary(cfg config.CLIToolConfig, defaultName string) (string, bool) {
    if !cfg.Enabled {
        return "", false
    }
    bin := cfg.BinaryPath
    if bin == "" {
        bin = defaultName
    }
    if filepath.IsAbs(bin) {
        if _, err := os.Stat(bin); err != nil {
            return "", false
        }
        return bin, true
    }
    resolved, err := exec.LookPath(bin)
    if err != nil {
        return "", false
    }
    return resolved, true
}
```

**No error logging:** `resolveBinary` returning `false` is a normal, non-error condition. Do not log warnings when a binary is not found — this fires on every `ListTools` call for every uninstalled tool.

---

### 7. Goroutine Leak Prevention

**Pattern:** Always drain the stdout scanner goroutine even on context cancel:
```go
scanDone := make(chan struct{})
go func() {
    defer close(scanDone)
    for scanner.Scan() {
        line := scanner.Text()
        if line != "" {
            progress(line)
            accumulated.WriteString(line + "\n")
        }
    }
}()

// Wait for subprocess to exit.
waitErr := cmd.Wait()
<-scanDone // ensure scanner goroutine exits after Wait closes the pipe
```

`cmd.Wait()` closes the stdout pipe, which causes `scanner.Scan()` to return false. The scanner goroutine then exits and closes `scanDone`. Always wait for `<-scanDone` after `cmd.Wait()`.

**`WaitDelay`:** With `cmd.WaitDelay = 5 * time.Second`, the pipes are force-closed 5 seconds after context cancellation even if the subprocess is still running. This unblocks the scanner goroutine in the worst case.

**Stdin goroutine:** Exits when the stdin channel is closed or ctx is done. The `defer stdinPipe.Close()` ensures the pipe is always closed, which signals the subprocess that stdin is done.

---

### 8. Progress Function Wiring: MCP Client Field Approach

**Decision:** Add `progressFn func(line string)` as a field on `mcp.Client`, set via `WithProgressFn` option at construction time.

**Why not context-carry:** The task ID is not available at `CallTool` time without plumbing it through the MCP interface, which would change the interface signature. The field approach keeps `domain.MCPClient` unchanged.

**Wiring in `team_manager.go`:**
```go
worker.WithProgressHandler(func(taskID, line string) {
    // existing progress logic
})
// Also wire into MCP client — but we need the taskID at call time.
```

**Problem:** The MCP client's `progressFn` does not have the `taskID`. The existing progress handler in `ExecuteTaskUseCase` knows the current task ID at call time. The two progress paths need to be bridged.

**Resolution:** The `ExecuteTaskUseCase` holds the current `taskID` during execution. It can wrap the MCP client's `CallTool` to inject progress. Or, the MCP client's `progressFn` is set to call a shared `slog.Info` which is separately captured by the task progress handler.

**Simplest correct approach:** The MCP client's `progressFn` is a raw line sink. In `team_manager.go`, set it to call the existing task progress infrastructure with the task ID captured via closure. The task ID can be stored in an atomic during `Execute` and read by the closure.

This design is to be finalised during implementation of Phase 5. Record the chosen approach in this file before writing tests.

---

### 9. `callPluginTool` Routing: Three Cases

After the fix, `callPluginTool` handles three distinct cases:

1. **Plugin not found / inactive:** Return `(zero, false, nil)` — not handled.
2. **Active plugin with JSON entrypoint (`plugin.json`):** Call `readSkill`. Return `(result, true, err)`.
3. **Active plugin with executable entrypoint:** Call `exec.Command`. Return `(result, true, err)`.

The existing test `TestMCPClient_CallTool_DispatchesToPluginEntrypoint` covers case 3. New tests cover case 2. A new test should also cover calling a plugin whose tool name does not match `name` (ensuring the loop continues correctly).

---

### 10. `openai-codex` and `opencode` Research Gate

**FR-6 and FR-7 are explicitly gated on CLI research.** Do not write tests or implementation code for `run_openai_codex` or `run_opencode` until the following are confirmed and recorded in this file:
- Binary name (exact executable name)
- Non-interactive mode flag (e.g. `--no-interactive`, `-q`, `--headless`)
- Model flag (e.g. `--model`, `-m`, `--provider`)
- Output format (plain text, JSON, or other)
- Whether `openai-codex` and the OpenAI Codex CLI from FR-5 (`codex`) are the same or different binaries

If `openai-codex` is identical to `codex` (same binary, different alias), consolidate to a single dialect with a different binary name and skip a separate implementation.

If `opencode` lacks a non-interactive mode, the requirement may need to be narrowed in the PRD before implementation. Flag this to the product owner.

---

### 11. `dec.KnownFields(true)` in Config Loader

The config loader uses `dec.KnownFields(true)`, which means any YAML field that does not have a corresponding Go struct field will cause a decode error. This is already the case for existing fields.

**Action required:** When adding `CLIToolsConfig` and `CLIToolConfig` to the config structs, ensure all YAML tag names exactly match the YAML keys in the PRD's config example:
- `cli_tools` → `CLITools CLIToolsConfig \`yaml:"cli_tools"\``
- `claude_code` → `ClaudeCode CLIToolConfig \`yaml:"claude_code"\``
- `codex` → `Codex CLIToolConfig \`yaml:"codex"\``
- `openai_codex` → `OpenAICodex CLIToolConfig \`yaml:"openai_codex"\``
- `opencode` → `OpenCode CLIToolConfig \`yaml:"opencode"\``
- `enabled` → `Enabled bool \`yaml:"enabled"\``
- `binary_path` → `BinaryPath string \`yaml:"binary_path"\``

Any YAML key not in this list will break existing config files. Do not add undocumented struct fields.

---

### 12. `MockCLIAgentRunner` for MCP Client Tests

The MCP client tests for CLI tools (Phase 5–8) need a mock `CLIAgentRunner` to avoid spawning real subprocesses. The mock should:
- Record the `cfg`, `instruction`, `stdin`, and `progress` arguments.
- Call `progress` with configurable lines.
- Return a configurable `(string, error)`.

Hand-write this mock in `internal/domain/mocks/mock_cli_agent_runner.go`:
```go
type MockCLIAgentRunner struct {
    RunFn func(ctx context.Context, cfg domain.CLIAgentConfig, instruction string,
               stdin <-chan string, progress func(line string)) (string, error)
}

func (m *MockCLIAgentRunner) Run(ctx context.Context, cfg domain.CLIAgentConfig,
    instruction string, stdin <-chan string, progress func(line string)) (string, error) {
    return m.RunFn(ctx, cfg, instruction, stdin, progress)
}
```

---

### 13. `isPluginJSONEntrypoint` Helper

To avoid duplicating the suffix-check logic, add a small private helper:
```go
func isPluginJSONEntrypoint(entrypoint string) bool {
    return filepath.Base(entrypoint) == "plugin.json"
}
```

This is more precise than `strings.HasSuffix` (avoids matching `notplugin.json`) and documents the intent.
