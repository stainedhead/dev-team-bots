# Research: Bot Capability Access — Review Fixes

## Source Files Examined

### `boabot/internal/infrastructure/local/mcp/client.go`

**FR-001 / FR-009 — `callCLITool` missing `resolvePath` for `work_dir`**

Lines 518–519:
```go
workDir, _ := args["work_dir"].(string)
```
`workDir` is assigned directly; no call to `c.resolvePath`. Compare with `runShell` (line 461) which calls `c.resolvePath(args, "working_dir")` before constructing the command.

The fix is a one-line change: replace the bare type assertion with `c.resolvePath(args, "work_dir")`. The `resolvePath` helper at line 647 already handles empty strings, absolute path validation, and `isAllowed` enforcement — no new logic is needed.

**FR-003 — `ListTools` drops context**

Line 90: `func (c *Client) ListTools(_ context.Context)`
Line 186: `c.pluginStore.List(context.Background())`

The domain interface (`domain.MCPClient`) likely already declares `ListTools(ctx context.Context)`. The fix is to thread `ctx` through. No logic change beyond the signature and one call-site.

**FR-004 — `callPluginTool` uses `os.Stat` for existence, not executability**

Lines 323–325:
```go
if _, statErr := os.Stat(entrypoint); os.IsNotExist(statErr) {
    return errResult(...), true, nil
}
```
The stat result is discarded with `_`. The fix is to capture the `os.FileInfo` and check `info.Mode()&0o100 == 0` after the not-exists check.

**FR-008 — `isPluginJSONEntrypoint` comment**

Lines 394–399:
```go
// isPluginJSONEntrypoint returns true when the entrypoint path is a Claude Code
// plugin.json file (non-executable). This is detected by checking the base
// filename, which avoids conflating "notplugin.json" with the real manifest.
func isPluginJSONEntrypoint(entrypoint string) bool {
    return filepath.Base(entrypoint) == "plugin.json"
}
```
The comment says "detected by checking the base filename" but does not explicitly state this is an exact match. A small clarification resolves FR-008.

**FR-011 — `progressFn` read without synchronisation**

Line 36: `progressFn func(line string)` field
Line 573: `c.cliRunner.Run(ctx, cfg, instruction, nil, c.progressFn)`
`AllowDir` at line 678 has the comment "Safe to call because bots process tasks sequentially." The same invariant applies to `progressFn`.

**FR-013 — `resolveBinary` absolute-path branch uses `os.Stat` only**

Lines 610–615:
```go
if filepath.IsAbs(bin) {
    if _, err := os.Stat(bin); err != nil {
        return "", false
    }
    return bin, true
}
```
The `_` discards the `os.FileInfo`. Fix: capture it and check `info.Mode()&0o100 == 0`.

---

### `boabot/internal/application/team/team_manager.go`

**FR-005 — Race test coverage for `resolvedPluginStore`**

Lines 137–141 define the pre-resolved fields:
```go
resolvedPluginStore domain.PluginStore
resolvedInstallDir  string
```

Lines 249–275 show these are written in `Run()` before goroutines start. The race the test is meant to cover: goroutine A writing these fields vs goroutine B reading them in `startBot` (lines 751–754).

The existing test replaces `tm.botRunner` with a stub that never reads these fields, so the race detector has no opportunity to fire. The fix requires the stub to read `tm.resolvedPluginStore` inside the goroutine body.

`export_test.go` is the standard Go mechanism for exposing unexported fields/methods to tests in the same package. Check if it already exports `resolvedPluginStore`; add an accessor if not.

---

### `boabot/internal/infrastructure/cliagent/runner.go`

**FR-006 — Context cancellation propagation test (MCP client level)**

`Run` (line 35) receives `ctx context.Context`. It creates `runCtx` with `context.WithTimeout(ctx, timeout)` at line 52, so cancellation of the caller's context propagates. The production code is correct; the gap is at the test level (no MCP client test exercises this path).

**FR-007 — `drainStdin` context-cancel-while-forwarding**

Lines 138–150:
```go
func drainStdin(ctx context.Context, stdinCh <-chan string, w io.WriteCloser) {
    defer func() { _ = w.Close() }()
    for {
        select {
        case line, ok := <-stdinCh:
            ...
        case <-ctx.Done():
            return
        }
    }
}
```
`drainStdin` correctly returns and closes `w` on `ctx.Done()`. This closes the subprocess's stdin, which will cause a `cat`-like subprocess to receive EOF and exit. The production code is correct; only a test is missing.

**FR-012 — stderr in error string**

Lines 126–130:
```go
stderr := strings.TrimSpace(stderrBuf.String())
if stderr != "" {
    return "", fmt.Errorf("cliagent: subprocess exited with error: %w; stderr: %s", waitErr, stderr)
}
return "", fmt.Errorf("cliagent: subprocess exited with error: %w", waitErr)
```
The format is already correct. Only a test verifying this path is missing.

---

## Summary of Code Changes Required

| Finding | File | Change Type |
|---------|------|-------------|
| FR-001/009 | `mcp/client.go:519` | Production code (1 line) |
| FR-002/010 | `docs/product-details.md:286` | Documentation |
| FR-003 | `mcp/client.go:90,186` | Production code (2 lines) |
| FR-004 | `mcp/client.go:324` | Production code (~4 lines) |
| FR-005 | `team/plugin_race_test.go`, `export_test.go` | Test code |
| FR-006 | `mcp/client_cli_tool_test.go` | Test code |
| FR-007 | `cliagent/runner_test.go` | Test code |
| FR-008 | `mcp/client.go:395` | Comment only |
| FR-011 | `mcp/client.go:36,573` | Comment only |
| FR-012 | `cliagent/runner_test.go` | Test code |
| FR-013 | `mcp/client.go:610` | Production code (~3 lines) |
| FR-014 | `specs/archive/.../status.md` | Documentation verification |
