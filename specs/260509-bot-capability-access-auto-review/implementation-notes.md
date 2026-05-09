# Implementation Notes: Bot Capability Access — Review Fixes

## FR-001 / REQ-001: `callCLITool` `work_dir` validation

**Decision:** Use `c.resolvePath(args, "work_dir")` unchanged — do not duplicate logic.

`resolvePath` at `client.go:647` already:
- Rejects empty strings with `missing required argument "work_dir"`.
- Calls `filepath.Abs` to resolve relative paths.
- Calls `filepath.Clean` to normalise.
- Calls `c.isAllowed` to enforce `allowedDirs`.
- Returns an error for any failure.

The fix is a single replacement of:
```go
workDir, _ := args["work_dir"].(string)
```
with:
```go
workDir, err := c.resolvePath(args, "work_dir")
if err != nil {
    return errResult(err.Error()), nil
}
```

This simultaneously resolves FR-009 (empty `work_dir`) because `resolvePath` rejects empty strings before any path logic.

**Test coverage note:** The test for the out-of-scope case must use a real `allowedDirs` slice (not empty), otherwise `isAllowed` returns false for everything including valid paths. Use `allowedDirs: []string{t.TempDir()}` and pass a `work_dir` pointing to a different temp dir.

---

## FR-003 / REQ-003: Context propagation in `ListTools`

**Decision:** Thread the caller's `ctx` through to `pluginStore.List`.

The `domain.MCPClient` interface declaration should already use `ctx context.Context` as a named parameter. If it uses `_`, update it — this is an interface compliance fix, not a behaviour change.

The only observable difference: if the caller provides a cancelled context, `pluginStore.List` may return early (depending on the implementation). The existing `LocalPluginStore` reads from disk; whether it honours context cancellation depends on its implementation. The important thing is the contract is now correct.

---

## FR-004 / REQ-004: Executable bit check on plugin entrypoint

**Decision:** Check `info.Mode()&0o100 != 0` (owner execute bit) after confirming the file exists.

`exec.LookPath` on Unix already checks the execute bit for PATH lookups. For plugin entrypoints (absolute paths under the install dir), the code currently uses `os.Stat` without checking the mode. The fix adds a mode check immediately after the exists check to provide a clear error before subprocess launch.

The error message format follows the existing conventions: `plugin "X" entrypoint is not executable: Y`.

**Note on Windows:** `Mode()&0o100` always returns 0 on Windows (files are not marked executable via mode bits). If Windows support is ever needed, use `os.Executable` or attempt `exec.LookPath(entrypoint)`. For now, the Unix check is sufficient for the deployment target (Linux ECS containers).

---

## FR-005 / REQ-005: Race test — reading `resolvedPluginStore` from goroutines

**Decision:** Expose `resolvedPluginStore` via `export_test.go` and read it inside the fake `botRunner`.

The fake `botRunner` in the race test is:
```go
tm.botRunner = func(ctx context.Context, entry BotEntry, orchName string) error {
    started <- entry.Name
    return nil
}
```

It must be changed to:
```go
tm.botRunner = func(ctx context.Context, entry BotEntry, orchName string) error {
    _ = tm.ResolvedPluginStore() // causes race detector to observe the field read
    started <- entry.Name
    return nil
}
```

Where `ResolvedPluginStore` is added to `export_test.go`:
```go
func (tm *TeamManager) ResolvedPluginStore() domain.PluginStore {
    return tm.resolvedPluginStore
}
```

The race detector fires on unsynchronised concurrent reads and writes to the same memory location. By reading `tm.resolvedPluginStore` from the goroutine body while another goroutine might be writing it (if the pre-resolution were absent), the test exercises the actual race path.

**Run the test with `-race`:** `go test -race -run TestTeamManager_PluginStorePreResolved ./internal/application/team/...`

---

## FR-006 / REQ-006: Context cancellation in `callCLITool`

**Decision:** No production code change. The fix is test-only.

`callCLITool` passes `ctx` directly to `c.cliRunner.Run`. `SubprocessRunner.Run` wraps it with `context.WithTimeout`. If the caller cancels `ctx`, `runCtx` is also cancelled. The production code is correct.

The mock runner in tests must implement blocking on `ctx.Done()` to make this observable:
```go
mockRunner.RunFn = func(ctx context.Context, cfg domain.CLIAgentConfig, instruction string, stdin <-chan string, progress func(string)) (string, error) {
    <-ctx.Done()
    return "", ctx.Err()
}
```

The test must cancel the context and assert that `CallTool` returns within a short timeout (use `time.AfterFunc` or a separate goroutine with `cancel()`).

---

## FR-013 / REQ-013: `resolveBinary` absolute-path executable bit

**Decision:** Check `info.Mode()&0o100 != 0` after `os.Stat` in the absolute-path branch.

`exec.LookPath` on Unix verifies the execute bit as part of PATH resolution. For absolute paths, the current code only checks existence. The fix brings the two branches into parity. This affects what `ListTools` reports — a non-executable binary at an absolute path will no longer appear in the tool list.

The fix does not affect the subprocess launch path in `cliagent/runner.go` (which calls `exec.LookPath(cfg.Binary)` again before starting), but it improves diagnostic accuracy at the tool-listing stage.
