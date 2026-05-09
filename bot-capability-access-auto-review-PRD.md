# Auto-Review PRD: Bot Capability Access

## Executive Summary

The implementation is structurally sound and well-tested, with clean architecture boundaries respected throughout. The plugin store data-race fix is correct, the `CLIAgentRunner` interface is clean, and test coverage is broad. Two issues require attention before merge: the `callCLITool` method does not validate `work_dir` against `allowedDirs` (a security gap present in every CLI delegation tool), and `product-details.md` contains an inaccurate entry for `run_openai_codex`. Several lower-priority improvements around context propagation, test gaps, and the plugin entrypoint stat check are also noted.

---

## Findings

### FR-001: `callCLITool` does not validate `work_dir` against `allowedDirs`

**Priority**: P0
**Location**: `boabot/internal/infrastructure/local/mcp/client.go:519`
**Description**: `callCLITool` reads `work_dir` from the tool args but assigns it directly to `domain.CLIAgentConfig.WorkDir` without calling `resolvePath` or `isAllowed`. Every other tool that accepts a path argument (`read_file`, `write_file`, `create_dir`, `list_dir`, `run_shell`) calls `resolvePath`, which enforces `allowedDirs`. The four CLI delegation tools (`run_claude_code`, `run_codex`, `run_openai_codex`, `run_opencode`) allow a bot to spawn a subprocess in any directory on the host, bypassing the sandboxing that protects all other tools. This is inconsistent with the stated security model and the tool schema description, which says `work_dir` is "the absolute path of the working directory for the CLI agent subprocess" without stating it is unrestricted.
**Acceptance Criteria**:
- `callCLITool` must call `c.resolvePath(args, "work_dir")` (or equivalent) before constructing `CLIAgentConfig.WorkDir`.
- An empty or out-of-scope `work_dir` must return an error result, not silently pass the value to the subprocess.
- A test must cover the case where `work_dir` is outside `allowedDirs` and verify the tool returns an error result with `IsError: true`.

---

### FR-002: `product-details.md` misidentifies `run_openai_codex` binary

**Priority**: P1
**Location**: `boabot/docs/product-details.md:286`
**Description**: The CLI Agent Tools table in `product-details.md` states `run_openai_codex` uses binary `codex` and describes it as "Alias for `run_codex`; targets the same OpenAI Codex binary." This is factually wrong. The code (`client.go:234`) uses the default binary name `openai-codex`, and `user-docs/cli-agent-tools.md` correctly describes it as a distinct binary. Operators reading `product-details.md` will be misled into thinking `run_codex` and `run_openai_codex` share a binary.
**Acceptance Criteria**:
- The `product-details.md` table entry for `run_openai_codex` must state the binary is `openai-codex` (not `codex`).
- The description must not characterise it as an alias for `run_codex`.

---

### FR-003: `ListTools` ignores the passed `context.Context` when listing plugins

**Priority**: P1
**Location**: `boabot/internal/infrastructure/local/mcp/client.go:90,186`
**Description**: `ListTools` is declared as `func (c *Client) ListTools(_ context.Context)` — it drops the caller's context. Internally it calls `c.pluginStore.List(context.Background())` at line 186. If the caller provides a cancellable context (e.g. for a bot task that is being cancelled), the plugin store call cannot be interrupted. The convention in this codebase is for all I/O operations to honour the caller's context. Contrast with `readSkill`, `completeBoardItem`, and `callPluginTool`, which all pass `ctx` through.
**Acceptance Criteria**:
- The `ListTools` function signature must use `ctx context.Context` (not `_`).
- The internal `pluginStore.List(...)` call must pass the received `ctx`.
- An existing or new test should verify that a cancelled context causes `ListTools` to return promptly.

---

### FR-004: `callPluginTool` uses `os.Stat` for entrypoint existence check — not executability

**Priority**: P1
**Location**: `boabot/internal/infrastructure/local/mcp/client.go:324`
**Description**: After the `isPluginJSONEntrypoint` early-return path, the code checks `os.Stat(entrypoint)` to confirm the file exists before running it as a subprocess. However, it does not check whether the file is executable. If a plugin ships a non-executable entrypoint that is also not named `plugin.json`, the `exec.Command` call will fail with "permission denied" at the OS level. The spec (FR-2, "callPluginTool update") describes the detection heuristic as checking for a `plugin.json` entrypoint; the approach is correct for the Claude Code plugin case. However, the residual code path that reaches `exec.CommandContext` for non-JSON entrypoints has no executability gate — only a presence gate. Using `exec.LookPath` for relative entrypoints, or checking the executable bit via `os.Stat` and inspecting `Mode()`, would give a clearer error before the subprocess is launched.
**Acceptance Criteria**:
- After confirming the entrypoint file exists, the code must check that the file mode includes the executable bit (at least owner executable) before invoking `exec.CommandContext`.
- If the file is not executable, return an error result with a message such as `plugin "X" entrypoint is not executable: Y`.
- A test must cover the case where the entrypoint exists but lacks the executable bit.

---

### FR-005: Plugin race test does not actually read `resolvedPluginStore` from bot goroutines

**Priority**: P1
**Location**: `boabot/internal/application/team/plugin_race_test.go`
**Description**: `TestTeamManager_PluginStorePreResolved` replaces `botRunner` with a stub that records the bot name and returns immediately. The stub never reads `tm.resolvedPluginStore` or `tm.resolvedInstallDir`. The race detector can only catch races on memory locations that are actually accessed from multiple goroutines. Because the stub does not read these fields, the test provides no race-detection coverage for the core fix — it only verifies that three bots start. The original data race was: goroutine A writing `tm.resolvedPluginStore` concurrently with goroutine B reading it in `startBot`. The test needs the fake bot runner to read those fields to exercise the race path.
**Acceptance Criteria**:
- The race test's fake botRunner must read at least `tm.resolvedPluginStore` (or an exposed proxy value set by `startBot`) from within the goroutine body.
- Alternatively, expose `resolvedPluginStore` via `export_test.go` and have the test read it concurrently from multiple goroutines while `Run` is in progress.
- `go test -race ./internal/application/team/...` must pass with the race detector enabled on the updated test.

---

### FR-006: No test for `run_shell` context propagation vs `callCLITool` — highlights missing test for context-cancelled CLI tool

**Priority**: P1
**Location**: `boabot/internal/infrastructure/local/mcp/client_cli_tool_test.go`
**Description**: The spec (FR-3 Acceptance Criteria) requires "long-running subprocesses do not block context cancellation of the parent bot." There is no test that verifies a cancelled context causes the CLI tool call to return promptly. The runner tests cover cancellation in `cliagent/runner_test.go` (TestSubprocessRunner_ContextCancellation), but the MCP client tests do not verify that a cancelled `ctx` is forwarded from `callCLITool` → `cliRunner.Run`. Since the mock runner records calls but does not enforce cancellation, this path is untested at the integration level.
**Acceptance Criteria**:
- A test must pass a context that is cancelled after a short delay to `client.CallTool(ctx, "run_claude_code", ...)`.
- The test must verify that `CallTool` returns before the mock runner's simulated long run completes (i.e. the cancellation is propagated to the runner).
- The mock runner's `RunFn` should block on `ctx.Done()` to simulate a long-running subprocess.

---

### FR-007: `drainStdin` closing the write-end pipe on context cancellation may leave subprocess stdin EOF-driven wait

**Priority**: P2
**Location**: `boabot/internal/infrastructure/cliagent/runner.go:138`
**Description**: `drainStdin` defers `w.Close()` and returns early on `ctx.Done()`. This correctly closes the subprocess's stdin pipe when the context is cancelled, which is the right behaviour. However, there is no test verifying that a subprocess that blocks reading from stdin (rather than reading from stdout) exits promptly when the context is cancelled. The existing `TestSubprocessRunner_ContextCancellation` tests context cancellation with a `sleep` subprocess (no stdin), not a subprocess that blocks on stdin. The spec requires the nil-stdin case to be tested, which it is (`TestSubprocessRunner_NilStdinChannel`), but the context-cancelled-while-stdin-forwarding case is not tested.
**Acceptance Criteria**:
- Add a test where the subprocess blocks reading from stdin indefinitely, the context is cancelled, and `Run` returns within a reasonable timeout (e.g. 5 seconds).
- Verify the error returned describes context cancellation.

---

### FR-008: `isPluginJSONEntrypoint` detects only exact `plugin.json` base name — `callPluginTool` does not detect nested paths uniformly

**Priority**: P2
**Location**: `boabot/internal/infrastructure/local/mcp/client.go:397`
**Description**: `isPluginJSONEntrypoint` returns true only when `filepath.Base(entrypoint) == "plugin.json"`. The spec (FR-2) notes detection by "filename suffix or `plugin.json` content sniff", and the `product-details.md` confirms detection is by `filepath.Base(entrypoint) == "plugin.json"`. The test in `client_read_skill_test.go` uses entrypoint path `.claude-plugin/plugin.json`, which has base name `plugin.json` — this passes. However, the implementation comment says "avoids conflating 'notplugin.json' with the real manifest", which is correct. The function is consistent with both the spec and the docs. This is a documentation mismatch only: the spec says "detected by filename suffix" but the implementation uses exact base name match. No code change needed; the spec comment should be clarified.
**Acceptance Criteria**:
- Add a comment in `isPluginJSONEntrypoint` making explicit that only exact `plugin.json` base name is detected (not any filename ending in `plugin.json`).
- Alternatively, update the spec comment in FR-2 to say "exact base name `plugin.json`".

---

### FR-009: `callCLITool` does not validate an empty `work_dir` argument

**Priority**: P2
**Location**: `boabot/internal/infrastructure/local/mcp/client.go:519`
**Description**: If `work_dir` is absent from the tool call arguments, `workDir` will be an empty string (line 519), and `domain.CLIAgentConfig.WorkDir` will be empty. The subprocess will then inherit the process working directory rather than failing with a clear error. The tool schema marks `work_dir` as required, but the code does not enforce this. Note: this is distinct from FR-001 (path validation against allowedDirs) but shares the same code location and is resolved by the same fix (calling `resolvePath`).
**Acceptance Criteria**:
- An empty `work_dir` must return an error result with `IsError: true` and a message indicating the argument is required.
- This is naturally satisfied by the FR-001 fix if `resolvePath` is used.

---

### FR-010: `product-details.md` CLI Agent Tools table description for `run_openai_codex` says "Alias" — user-docs is accurate but docs are inconsistent

**Priority**: P2
**Location**: `boabot/docs/product-details.md:286`, `boabot/user-docs/cli-agent-tools.md:11`
**Description**: See FR-002 — this finding is the broader inconsistency. While FR-002 is P1 (the factual error), the table in `product-details.md` also omits the `--full-auto` flag detail that distinguishes `run_openai_codex` invocation from `run_codex`. The user-docs file correctly describes all four tools with distinct descriptions. Product-details.md should be brought to parity.
**Acceptance Criteria**:
- `product-details.md` CLI Agent Tools table must list distinct descriptions for `run_codex` and `run_openai_codex`, including the different binary names and invocation flags.

---

### FR-011: No test for concurrent `callCLITool` calls with shared `progressFn` — potential data race if called concurrently

**Priority**: P2
**Location**: `boabot/internal/infrastructure/local/mcp/client.go:573`
**Description**: The `c.progressFn` field is read at line 573 without any synchronisation: `c.cliRunner.Run(ctx, cfg, instruction, nil, c.progressFn)`. In the intended usage, each bot's MCP client is single-threaded (tasks execute sequentially), so this is safe. The code comment "Safe to call because bots process tasks sequentially" exists on `AllowDir` but not on the `progressFn` field. There is no documentation or assertion confirming single-threaded access. If the future allows parallel tool calls, this is a latent race.
**Acceptance Criteria**:
- Add a comment to the `progressFn` field and/or the `callCLITool` method documenting the single-threaded access assumption.
- No code change required if the sequential access invariant is documented.

---

### FR-012: `runner_test.go` does not test that stderr content is included in the error string on non-zero exit

**Priority**: P2
**Location**: `boabot/internal/infrastructure/cliagent/runner_test.go`
**Description**: The spec (FR-3) and the implementation both capture stderr and include it in the error message when the subprocess exits non-zero. `TestSubprocessRunner_BinaryNotFound` verifies "not found" in errors, but no test verifies that stderr output is attached when a subprocess exits with a non-zero code. The error format is: `cliagent: subprocess exited with error: %w; stderr: %s`.
**Acceptance Criteria**:
- Add a test where a script exits non-zero and writes to stderr; verify the returned error string contains the expected stderr text.

---

### FR-013: `resolveBinary` uses `os.Stat` for absolute paths — does not verify the executable bit

**Priority**: P2
**Location**: `boabot/internal/infrastructure/local/mcp/client.go:611`
**Description**: `resolveBinary` uses `exec.LookPath` for relative binary names (which verifies executability on most platforms) but uses `os.Stat` for absolute paths (which only verifies existence). A non-executable file at an absolute path will resolve as available in `ListTools` but fail at subprocess launch with "permission denied". Consistent executable-bit checking would improve the diagnostics at tool-list time.
**Acceptance Criteria**:
- For absolute path binaries, check that the file has at least owner executable bit set before returning `true`.
- Return `("", false)` if the file exists but is not executable.
- Add a test verifying that a non-executable absolute-path binary is not included in `ListTools`.

---

### FR-014: `specs/260509-bot-capability-access/status.md` may not reflect final implementation state

**Priority**: P2
**Location**: `specs/260509-bot-capability-access/status.md`
**Description**: The spec workflow requires `status.md` to be updated after each task completion and to show 100% completion before archiving. Verify this file accurately reflects all tasks as completed now that implementation and documentation steps are done.
**Acceptance Criteria**:
- `status.md` must show all Phase 5 tasks as complete.
- Phase 6 (Documentation) tasks must be marked complete or in-progress as appropriate.
- No tasks left in "In Progress" or "Pending" that have been completed.
