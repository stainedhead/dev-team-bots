# Tasks: Bot Capability Access — Review Fixes

Each task follows RED → GREEN → REFACTOR. All tests run with `go test -race ./...`.

---

## T-001 (REQ-001/REQ-009): Validate `work_dir` in `callCLITool` — P0

**RED — Write failing tests:**
- File: `boabot/internal/infrastructure/local/mcp/client_cli_tool_test.go`
- Function: `TestCallCLITool_WorkDirOutsideAllowedDirs`
  - Creates a client with `allowedDirs = ["/allowed"]`.
  - Calls `client.CallTool(ctx, "run_claude_code", map[string]any{"instruction": "x", "work_dir": "/not-allowed"})`.
  - Asserts `result.IsError == true`.
- Function: `TestCallCLITool_EmptyWorkDir`
  - Calls with `work_dir: ""` (or absent).
  - Asserts `result.IsError == true` and message contains "missing required argument".

**GREEN — Implement fix:**
- File: `boabot/internal/infrastructure/local/mcp/client.go`
- Location: `callCLITool`, around line 519.
- Change: Replace `workDir, _ := args["work_dir"].(string)` with:
  ```go
  workDir, err := c.resolvePath(args, "work_dir")
  if err != nil {
      return errResult(err.Error()), nil
  }
  ```

**REFACTOR:** Confirm all four CLI tool names (`run_claude_code`, `run_codex`, `run_openai_codex`, `run_opencode`) are covered by at least one out-of-scope test (parametrise if needed).

**Verify:** `go test -race ./internal/infrastructure/local/mcp/...`

---

## T-002 (REQ-002/REQ-010): Fix `product-details.md` for `run_openai_codex` — P1

**RED:** No unit test (documentation fix). Manual review before and after.

**GREEN — Implement fix:**
- File: `boabot/docs/product-details.md`
- Location: CLI Agent Tools table, `run_openai_codex` row (~line 286).
- Change binary column from `codex` to `openai-codex`.
- Change description to remove "Alias for `run_codex`"; replace with distinct description: "Runs the `openai-codex` open-source CLI agent in full-auto mode (`--full-auto`). Distinct binary from `run_codex`."
- While in the file: ensure all four CLI tools have distinct descriptions and accurate binary names (`claude`, `codex`, `openai-codex`, `opencode`).

**REFACTOR:** Cross-check against `boabot/user-docs/cli-agent-tools.md` to confirm parity.

**Verify:** Visual inspection; no automated test.

---

## T-003 (REQ-003): Honour caller context in `ListTools` — P1

**RED — Write failing test:**
- File: `boabot/internal/infrastructure/local/mcp/client_test.go` (or `client_list_tools_test.go`)
- Function: `TestListTools_PassesContextToPluginStore`
  - Creates a `mockPluginStore` whose `List` method records the context it received.
  - Creates a client with the mock store.
  - Creates a pre-cancelled context (`ctx, cancel := context.WithCancel(context.Background()); cancel()`).
  - Calls `client.ListTools(ctx)`.
  - Asserts the mock's recorded context has `Err() != nil`.

**GREEN — Implement fix:**
- File: `boabot/internal/infrastructure/local/mcp/client.go`
- Location: Line 90 (`ListTools` signature) and line 186 (`pluginStore.List` call).
- Change signature from `func (c *Client) ListTools(_ context.Context)` to `func (c *Client) ListTools(ctx context.Context)`.
- Change `c.pluginStore.List(context.Background())` to `c.pluginStore.List(ctx)`.

**REFACTOR:** Confirm the `MCPClient` interface in `domain/mcp.go` already declares `ListTools(ctx context.Context)`. If not, update it.

**Verify:** `go test -race ./internal/infrastructure/local/mcp/...`

---

## T-004 (REQ-004): Executable bit check on plugin entrypoint — P1

**RED — Write failing test:**
- File: `boabot/internal/infrastructure/local/mcp/client_plugin_test.go`
- Function: `TestCallPluginTool_EntrypointNotExecutable`
  - Creates a temp file (`os.CreateTemp`) without executable bit (mode `0o644`).
  - Registers a fake plugin in a mock `pluginStore` pointing to that file as entrypoint.
  - Calls `client.CallTool(ctx, "fake-tool", ...)`.
  - Asserts `result.IsError == true` and message contains "not executable".

**GREEN — Implement fix:**
- File: `boabot/internal/infrastructure/local/mcp/client.go`
- Location: After `os.Stat(entrypoint)` existence check (~line 324).
- Change: capture stat info, then add:
  ```go
  info, statErr := os.Stat(entrypoint)
  if os.IsNotExist(statErr) {
      return errResult(fmt.Sprintf("plugin %q entrypoint not found: %s", p.Name, entrypoint)), true, nil
  }
  if statErr != nil {
      return errResult(fmt.Sprintf("plugin %q stat entrypoint: %v", p.Name, statErr)), true, nil
  }
  if info.Mode()&0o100 == 0 {
      return errResult(fmt.Sprintf("plugin %q entrypoint is not executable: %s", p.Name, entrypoint)), true, nil
  }
  ```

**REFACTOR:** Ensure the existing "entrypoint not found" test still passes.

**Verify:** `go test -race ./internal/infrastructure/local/mcp/...`

---

## T-005 (REQ-005): Race test reads `resolvedPluginStore` from goroutines — P1

**RED — Write failing test (or fix existing test to be meaningful):**
- File: `boabot/internal/application/team/plugin_race_test.go`
- Update `TestTeamManager_PluginStorePreResolved`:
  - The fake `botRunner` assigned to `tm.botRunner` must read `tm.resolvedPluginStore` (accessed via `export_test.go` export) inside the goroutine body.
  - Example: `_ = tm.ResolvedPluginStore()` inside the stub function body.

**GREEN — Implement fix:**
- File: `boabot/internal/application/team/export_test.go`
- Add if not present:
  ```go
  func (tm *TeamManager) ResolvedPluginStore() domain.PluginStore {
      return tm.resolvedPluginStore
  }
  ```
- Update the fake `botRunner` in `plugin_race_test.go` to call `tm.ResolvedPluginStore()`.

**REFACTOR:** Confirm `go test -race` catches a race if the pre-resolution is removed (optional: temporarily revert and confirm detector fires).

**Verify:** `go test -race ./internal/application/team/...`

---

## T-006 (REQ-006): Test context cancellation propagates through `callCLITool` — P1

**RED — Write failing test:**
- File: `boabot/internal/infrastructure/local/mcp/client_cli_tool_test.go`
- Function: `TestCallCLITool_ContextCancelledDuringRun`
  - Creates a mock `CLIAgentRunner` whose `Run` blocks on `ctx.Done()`.
  - Creates a client with the mock runner and a valid `allowedDirs`.
  - Calls `client.CallTool(ctx, "run_claude_code", ...)` in a goroutine.
  - Cancels the context after a short delay (e.g. via `time.AfterFunc(50*time.Millisecond, cancel)`).
  - Asserts `CallTool` returns within 2 seconds.

**GREEN — Implement fix:**
- No production code change expected (the fix propagates naturally once T-001 passes and `ctx` is already threaded through `cliRunner.Run`).
- If there is a problem, the issue is that `callCLITool` creates a new context — check for any `context.Background()` misuse.

**REFACTOR:** Confirm the mock `CLIAgentRunner` is reusable for other tests.

**Verify:** `go test -race ./internal/infrastructure/local/mcp/...`

---

## T-007 (REQ-007): Test `drainStdin` cancel-while-forwarding — P2

**RED — Write failing test:**
- File: `boabot/internal/infrastructure/cliagent/runner_test.go`
- Function: `TestSubprocessRunner_ContextCancelledWhileForwardingStdin`
  - Subprocess: `cat` (blocks reading stdin).
  - Creates a context; starts `Run` in a goroutine with a stdin channel.
  - Cancels the context after 50ms.
  - Asserts `Run` returns within 5 seconds.
  - Asserts error contains context cancellation.

**GREEN:** No production code change expected (drainStdin already returns on `ctx.Done()`).

**Verify:** `go test -race -timeout 30s ./internal/infrastructure/cliagent/...`

---

## T-008 (REQ-008): Clarify `isPluginJSONEntrypoint` comment — P2

**RED:** No test (comment-only change).

**GREEN — Implement fix:**
- File: `boabot/internal/infrastructure/local/mcp/client.go`
- Location: `isPluginJSONEntrypoint` function (~line 395).
- Update comment to: "isPluginJSONEntrypoint returns true when the entrypoint base name is exactly 'plugin.json'. This is an exact match only — filenames such as 'myplugin.json' do not match."

**Verify:** `go vet ./...` passes.

---

## T-009 (REQ-011): Document single-threaded access for `progressFn` — P2

**RED:** No test (comment-only change).

**GREEN — Implement fix:**
- File: `boabot/internal/infrastructure/local/mcp/client.go`
- Location: `progressFn` field (~line 36) and/or `callCLITool` body (~line 573).
- Add comment: "progressFn is read without synchronisation; this is safe because bots process tasks sequentially — only one tool call is active at a time per Client instance."

**Verify:** `go vet ./...` passes.

---

## T-010 (REQ-012): Test stderr content in runner error on non-zero exit — P2

**RED — Write failing test:**
- File: `boabot/internal/infrastructure/cliagent/runner_test.go`
- Function: `TestSubprocessRunner_NonZeroExitIncludesStderr`
  - Script: `sh -c 'echo "fail detail" >&2; exit 1'`.
  - Calls `runner.Run(ctx, cfg, "", nil, nil)`.
  - Asserts error is non-nil.
  - Asserts `err.Error()` contains `"fail detail"`.

**GREEN:** No production code change expected (format `cliagent: subprocess exited with error: %w; stderr: %s` already includes stderr).

**Verify:** `go test -race ./internal/infrastructure/cliagent/...`

---

## T-011 (REQ-013): Check executable bit on absolute-path binaries in `resolveBinary` — P2

**RED — Write failing tests:**
- File: `boabot/internal/infrastructure/local/mcp/client_cli_tool_test.go`
- Function: `TestResolveBinary_AbsolutePathNotExecutable`
  - Creates a temp file with mode `0o644` (no execute bit).
  - Calls `resolveBinary(config.CLIToolConfig{Enabled: true, BinaryPath: tmpFile}, "default")`.
  - Asserts `(_, false)`.
- Function: `TestListTools_NonExecutableAbsoluteBinaryExcluded`
  - Creates a client configured with a non-executable absolute-path binary for `ClaudeCode`.
  - Calls `ListTools(ctx)`.
  - Asserts `run_claude_code` is not in the returned tool list.

**GREEN — Implement fix:**
- File: `boabot/internal/infrastructure/local/mcp/client.go`
- Location: `resolveBinary`, absolute-path branch (~line 610).
- Change:
  ```go
  if filepath.IsAbs(bin) {
      info, err := os.Stat(bin)
      if err != nil {
          return "", false
      }
      if info.Mode()&0o100 == 0 {
          return "", false
      }
      return bin, true
  }
  ```

**Verify:** `go test -race ./internal/infrastructure/local/mcp/...`

---

## T-012 (REQ-014): Verify archived spec status.md — P2

**RED:** No code test.

**GREEN — Verify:**
- Read `specs/archive/260509-bot-capability-access/status.md`.
- Confirm all Phase 5 and Phase 6 tasks are marked complete.
- Update any entries found incomplete.

**Verify:** Manual inspection.
