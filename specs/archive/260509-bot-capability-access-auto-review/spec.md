# Review Fixes: Bot Capability Access

**Source PRD:** bot-capability-access-auto-review-PRD.md
**Branch:** feat/bot-capability-access
**Date:** 2026-05-09

## Overview

This spec tracks implementation of all findings from the auto-review of the bot-capability-access feature. Findings are grouped by priority. Each requirement has acceptance criteria from the review PRD and a reference to the affected file.

---

## P0 Findings (must fix before merge)

### REQ-001: Validate `work_dir` against `allowedDirs` in `callCLITool`

**Source:** FR-001
**File:** `boabot/internal/infrastructure/local/mcp/client.go:519`

`callCLITool` reads `work_dir` from tool args and assigns it directly to `domain.CLIAgentConfig.WorkDir` without calling `resolvePath` or `isAllowed`. Every other path-accepting tool (`read_file`, `write_file`, `create_dir`, `list_dir`, `run_shell`) enforces `allowedDirs` via `resolvePath`. The four CLI delegation tools allow a bot to spawn a subprocess in any directory on the host, bypassing the sandbox.

**Acceptance Criteria:**
1. `callCLITool` must call `c.resolvePath(args, "work_dir")` before constructing `CLIAgentConfig.WorkDir`.
2. An empty or out-of-scope `work_dir` must return `domain.MCPToolResult{IsError: true}` — not silently pass the value to the subprocess.
3. A test must cover the case where `work_dir` is outside `allowedDirs` and verify the tool returns an error result with `IsError: true`.
4. An empty (missing) `work_dir` must also return an error result (naturally satisfied by `resolvePath`, which rejects empty strings).

---

## P1 Findings (should fix before merge)

### REQ-002: Fix `product-details.md` entry for `run_openai_codex` binary name

**Source:** FR-002
**File:** `boabot/docs/product-details.md:286`

The CLI Agent Tools table in `product-details.md` states `run_openai_codex` uses binary `codex` and calls it "Alias for `run_codex`". The code (`client.go:234`) uses the default binary `openai-codex`, and `user-docs/cli-agent-tools.md` correctly describes it as a distinct binary. Operators reading `product-details.md` will be misled.

**Acceptance Criteria:**
1. The `product-details.md` table entry for `run_openai_codex` must state the binary is `openai-codex` (not `codex`).
2. The description must not characterise `run_openai_codex` as an alias for `run_codex`.

---

### REQ-003: `ListTools` must honour the caller's `context.Context`

**Source:** FR-003
**File:** `boabot/internal/infrastructure/local/mcp/client.go:90,186`

`ListTools` is declared `func (c *Client) ListTools(_ context.Context)` — it drops the caller's context. Internally it calls `c.pluginStore.List(context.Background())` at line 186. If the caller provides a cancellable context (e.g. for a bot task being cancelled), the plugin store call cannot be interrupted. All I/O operations in the codebase honour the caller's context.

**Acceptance Criteria:**
1. The `ListTools` function signature must use `ctx context.Context` (not `_`).
2. The internal `pluginStore.List(...)` call must pass the received `ctx`.
3. An existing or new test must verify that a cancelled context causes `ListTools` to return promptly (or the pluginStore mock is called with the cancelled context).

---

### REQ-004: Check executable bit on plugin entrypoint before launching subprocess

**Source:** FR-004
**File:** `boabot/internal/infrastructure/local/mcp/client.go:324`

`callPluginTool` uses `os.Stat(entrypoint)` to confirm the file exists but does not check whether the file is executable. A non-executable entrypoint will fail with "permission denied" at OS level. A clearer error before subprocess launch improves diagnostics.

**Acceptance Criteria:**
1. After confirming the entrypoint file exists, the code must check that the file mode includes at least owner executable bit (`os.Stat` + `Mode()&0o100 != 0`) before invoking `exec.CommandContext`.
2. If the file is not executable, return `errResult(fmt.Sprintf("plugin %q entrypoint is not executable: %s", p.Name, entrypoint))`.
3. A test must cover the case where the entrypoint exists but lacks the executable bit, verifying `IsError: true` in the result.

---

### REQ-005: Race test must read `resolvedPluginStore` from bot goroutines

**Source:** FR-005
**File:** `boabot/internal/application/team/plugin_race_test.go`

`TestTeamManager_PluginStorePreResolved` uses a stub `botRunner` that records the bot name and returns immediately. The stub never reads `tm.resolvedPluginStore` or `tm.resolvedInstallDir`. The race detector can only catch races on memory locations that are actually accessed from multiple goroutines.

**Acceptance Criteria:**
1. The race test's fake `botRunner` must read at least `tm.resolvedPluginStore` (or an exported proxy via `export_test.go`) from within the goroutine body.
2. Alternatively, expose `resolvedPluginStore` via `export_test.go` and have the test read it concurrently from multiple goroutines while `Run` is in progress.
3. `go test -race ./internal/application/team/...` must pass with the race detector enabled on the updated test.

---

### REQ-006: Test that context cancellation propagates from `callCLITool` to `cliRunner.Run`

**Source:** FR-006
**File:** `boabot/internal/infrastructure/local/mcp/client_cli_tool_test.go`

The spec requires long-running subprocesses not to block context cancellation of the parent bot. There is no test verifying a cancelled context causes the CLI tool call to return promptly at the MCP client level.

**Acceptance Criteria:**
1. A test must pass a context that is cancelled after a short delay to `client.CallTool(ctx, "run_claude_code", ...)`.
2. The test must verify that `CallTool` returns before the mock runner's simulated long run completes (i.e. the cancellation is propagated to the runner).
3. The mock runner's `RunFn` should block on `ctx.Done()` to simulate a long-running subprocess.

---

## P2 Findings (nice to have; do not block merge)

### REQ-007: Test `drainStdin` context-cancel-while-stdin-forwarding path

**Source:** FR-007
**File:** `boabot/internal/infrastructure/cliagent/runner.go:138`

`drainStdin` correctly closes the write-end pipe on context cancellation. There is no test verifying that a subprocess blocking on stdin exits promptly when the context is cancelled.

**Acceptance Criteria:**
1. Add a test where a subprocess blocks reading from stdin indefinitely, the context is cancelled, and `Run` returns within a reasonable timeout (e.g. 5 seconds).
2. The returned error must describe context cancellation.

---

### REQ-008: Clarify `isPluginJSONEntrypoint` comment — exact base name, not suffix

**Source:** FR-008
**File:** `boabot/internal/infrastructure/local/mcp/client.go:397`

`isPluginJSONEntrypoint` uses exact base name match (`filepath.Base(entrypoint) == "plugin.json"`), but a comment in the spec says "filename suffix". No code change is needed; a comment clarification prevents future confusion.

**Acceptance Criteria:**
1. Add or update the comment in `isPluginJSONEntrypoint` to state explicitly that only an exact `plugin.json` base name is matched (not any filename ending in `plugin.json`).

---

### REQ-009: Empty `work_dir` must return a clear error in `callCLITool`

**Source:** FR-009
**File:** `boabot/internal/infrastructure/local/mcp/client.go:519`

If `work_dir` is absent from the tool call arguments, `workDir` is an empty string, and the subprocess inherits the process working directory. The tool schema marks `work_dir` as required, but the code does not enforce this.

**Acceptance Criteria:**
1. An empty `work_dir` must return `IsError: true` with a message indicating the argument is required.
2. This is naturally satisfied by the REQ-001 fix (calling `resolvePath`).

---

### REQ-010: Align `product-details.md` CLI Agent Tools table with `user-docs/cli-agent-tools.md`

**Source:** FR-010
**File:** `boabot/docs/product-details.md:286`, `boabot/user-docs/cli-agent-tools.md:11`

`product-details.md` omits the `--full-auto` flag detail that distinguishes `run_openai_codex` from `run_codex`. The user-docs file correctly describes all four tools with distinct descriptions.

**Acceptance Criteria:**
1. `product-details.md` CLI Agent Tools table must list distinct descriptions for `run_codex` and `run_openai_codex`, including different binary names and invocation flags.

---

### REQ-011: Document single-threaded access assumption for `progressFn`

**Source:** FR-011
**File:** `boabot/internal/infrastructure/local/mcp/client.go:573`

`c.progressFn` is read at line 573 without synchronisation. In intended usage each bot's MCP client is single-threaded, but there is no comment documenting this invariant.

**Acceptance Criteria:**
1. Add a comment to the `progressFn` field and/or the `callCLITool` method documenting the single-threaded access assumption.
2. No code change is required if the sequential access invariant is documented.

---

### REQ-012: Test that stderr content is included in runner error string on non-zero exit

**Source:** FR-012
**File:** `boabot/internal/infrastructure/cliagent/runner_test.go`

No test verifies that stderr output is attached to the error when a subprocess exits with a non-zero code. The format is `cliagent: subprocess exited with error: %w; stderr: %s`.

**Acceptance Criteria:**
1. Add a test where a script exits non-zero and writes to stderr; verify the returned error string contains the expected stderr text.

---

### REQ-013: Check executable bit on absolute-path binaries in `resolveBinary`

**Source:** FR-013
**File:** `boabot/internal/infrastructure/local/mcp/client.go:611`

`resolveBinary` uses `exec.LookPath` for relative binary names (which verifies executability) but uses `os.Stat` for absolute paths (existence only). A non-executable absolute-path binary resolves as available in `ListTools` but fails at subprocess launch.

**Acceptance Criteria:**
1. For absolute-path binaries, check that the file mode includes at least owner executable bit before returning `true`.
2. Return `("", false)` if the file exists but is not executable.
3. Add a test verifying that a non-executable absolute-path binary is not included in `ListTools`.

---

### REQ-014: Verify `specs/260509-bot-capability-access/status.md` shows 100% completion

**Source:** FR-014
**File:** `specs/260509-bot-capability-access/status.md` (archived)

The spec workflow requires `status.md` to show 100% completion before archiving.

**Acceptance Criteria:**
1. `status.md` in the archived spec must show all Phase 5 tasks as complete.
2. Phase 6 (Documentation) tasks must be marked complete.
3. No tasks left in "In Progress" or "Pending" that have been completed.
