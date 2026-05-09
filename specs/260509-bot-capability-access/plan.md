# Implementation Plan: Bot Capability Access

## Ordered Phases

---

### Phase 1: Fix Plugin Store Race (FR-1)

**Goal:** Eliminate the data race on `tm.pluginStore` and `tm.pluginInstallDir`.

**Changes:**
- `internal/application/team/team_manager.go`
  - Add plugin store pre-resolution block in `Run()` before the goroutine loop.
  - Load the orchestrator bot's config to extract `Plugins.InstallDir`.
  - Build `LocalPluginStore` and resolve the install directory into local variables.
  - Update `startBot` signature to accept `pluginStore domain.PluginStore` and `installDir string` as parameters.
  - Remove writes to `tm.pluginStore` and `tm.pluginInstallDir` from inside `startBot`.
  - Update the `botRunner` function type to match the new signature.
  - Update `export_test.go` to reflect new `startBot` signature.
- Tests:
  - Failing test first: concurrent `ListTools` calls must return plugin tools from all goroutines (race detector must catch the current bug).
  - After fix: `go test -race` passes.

**Acceptance:** All bots see plugin tools; race detector clean.

---

### Phase 2: `read_skill` Tool (FR-2)

**Goal:** Add `read_skill` as a builtin MCP tool and fix non-executable plugin entrypoint dispatch.

**Changes:**
- `internal/infrastructure/local/mcp/client.go`
  - Add `read_skill` to `ListTools` (when `pluginStore != nil`).
  - Add `readSkill(ctx, args)` method.
  - Update `callPluginTool` to detect `plugin.json` entrypoints and delegate to `readSkill`.
  - Add `"read_skill"` case to `CallTool` switch.
- Tests (`client_plugin_test.go` or new `client_skill_test.go`):
  - `read_skill` returns Markdown content for installed active skill.
  - `read_skill` returns error for unknown skill name.
  - `read_skill` returns error for disabled plugin's skill.
  - `callPluginTool` with `plugin.json` entrypoint delegates to `readSkill` (not exec).
  - `callPluginTool` with executable entrypoint still uses exec path (no regression).

**Acceptance:** All acceptance criteria in FR-2 pass.

---

### Phase 3: CLIAgentRunner Domain Interface + `cliagent` Infra Package (FR-3)

**Goal:** Define the domain interface and implement the subprocess runner with full test coverage.

**Changes:**
- `internal/domain/cliagent.go` (new)
  - `CLIAgentConfig` struct.
  - `CLIAgentRunner` interface.
- `internal/infrastructure/cliagent/runner.go` (new)
  - `SubprocessRunner` implementing `CLIAgentRunner`.
  - Uses `exec.Command` + `cmd.Cancel` (SIGTERM) + `cmd.WaitDelay` (5s).
  - `bufio.Scanner` on stdout, progress callback per line.
  - Stdin goroutine with non-blocking channel select.
  - `exec.LookPath` check before start.
- `internal/infrastructure/cliagent/runner_test.go` (new)
  - Normal completion with accumulated output.
  - Timeout triggers SIGTERM then SIGKILL.
  - Explicit context cancellation.
  - Stdin write forwarded to subprocess.
  - Progress callback called for each output line.
  - Nil stdin channel: subprocess completes without blocking.
  - Binary not found: returns clear error.
- `internal/domain/mocks/` — add `MockCLIAgentRunner`.

**Acceptance:** All unit tests pass with `-race`; coverage ≥90% in `cliagent` package.

---

### Phase 4: Extract Streaming JSON Parser (prerequisite for FR-4)

**Goal:** Make `ParseStreamLine` available to the new `run_claude_code` tool without duplication.

**Changes:**
- `internal/infrastructure/codeagent/stream.go` (new)
  - Export `ParseStreamLine(line string) (text string, ok bool)`.
  - Move `streamEvent` and `deltaField` types here.
- `internal/infrastructure/codeagent/provider.go`
  - Replace call to unexported `extractText` with call to `ParseStreamLine`.
  - Remove `streamEvent`, `deltaField`, `extractText` from this file.
- Tests:
  - Add tests for `ParseStreamLine` directly in `stream_test.go`.
  - Existing `provider_test.go` tests must still pass (no regression).

**Acceptance:** `provider_test.go` passes; `stream_test.go` covers `ParseStreamLine` edge cases.

---

### Phase 5: `run_claude_code` Tool (FR-4)

**Goal:** Expose the Claude Code CLI as an MCP tool.

**Changes:**
- `internal/infrastructure/local/mcp/client.go`
  - Add `cliRunner domain.CLIAgentRunner` and `cliTools config.CLIToolsConfig` fields.
  - Add `WithCLIRunner` and `WithCLITools` option functions.
  - Add `progressFn func(line string)` field and `WithProgressFn` option.
  - Add `run_claude_code` to `ListTools` (gated on binary availability).
  - Add `callCLITool` method.
  - Add `"run_claude_code"` case to `CallTool` switch.
  - `run_claude_code` args: `["--output-format=stream-json", "--dangerously-skip-permissions"]` + optional `["--model", model]` + `["-p", instruction]`.
  - Post-process output lines through `ParseStreamLine`.
- `internal/application/team/team_manager.go`
  - Pre-resolve `cliRunner = cliagent.New()` in `Run()`.
  - Pass `cliRunner` and `cliTools` to `startBot`.
  - Wire `WithCLIRunner`, `WithCLITools`, `WithProgressFn` into MCP client construction.
- `internal/infrastructure/config/config.go`
  - Add `CLIToolConfig`, `CLIToolsConfig` structs.
  - Add `CLITools CLIToolsConfig` to `OrchestratorConfig`.
- Tests:
  - `ListTools` includes `run_claude_code` when enabled and binary resolves (mock `exec.LookPath` or use a test binary).
  - `ListTools` excludes `run_claude_code` when disabled.
  - `ListTools` excludes `run_claude_code` when binary not found.
  - `CallTool("run_claude_code", ...)` calls `cliRunner.Run` with correct args.
  - Model arg included when `model` field is non-empty.
  - Model arg omitted when `model` field is empty.
  - Progress callback invoked for each output line.

**Acceptance:** All FR-4 acceptance criteria pass.

---

### Phase 6: `run_codex` Tool (FR-5)

**Goal:** Expose the Codex CLI as an MCP tool.

**Changes:**
- `internal/infrastructure/local/mcp/client.go`
  - Add `run_codex` to `ListTools`.
  - Add `"run_codex"` case to `CallTool` switch.
  - Codex args: `["-q", "--approval-mode=full-auto"]` + optional `["--model", model]` + `[instruction]`.
  - Plain-text output (no JSON parsing).
- Tests: same pattern as Phase 5.

**Note:** Verify `--model` flag against actual Codex CLI. Adjust if the flag differs.

**Acceptance:** All FR-5 acceptance criteria pass.

---

### Phase 7: Research + Implement `run_openai_codex` (FR-6)

**Goal:** Research the `openai-codex` binary's CLI interface and implement the tool.

**Steps:**
1. Run `openai-codex --help` to determine: binary name, non-interactive mode flag, model flag, output format.
2. If identical to `codex` (FR-5), reuse the same dialect/args with the different binary name.
3. If different: implement a distinct dialect.
4. Record findings in `implementation-notes.md` before writing any tests.
5. Write failing test, implement, refactor.

**Changes:**
- `internal/infrastructure/local/mcp/client.go`: add `run_openai_codex` tool.
- Update tool description with researched capabilities (remove TBD placeholder).

**Acceptance:** FR-6 acceptance criteria pass; no TBD in shipped description.

---

### Phase 8: Research + Implement `run_opencode` (FR-7)

**Goal:** Research the `opencode` binary's CLI interface and implement the tool.

**Steps:**
1. Run `opencode --help` / check opencode.ai documentation.
2. Determine: non-interactive mode, model flag, output format.
3. Record findings in `implementation-notes.md`.
4. Write failing test, implement, refactor.

**Changes:**
- `internal/infrastructure/local/mcp/client.go`: add `run_opencode` tool.
- Update tool description with researched capabilities.

**Acceptance:** FR-7 acceptance criteria pass; no TBD in shipped description.

---

### Phase 9: Config Additions Validation (FR-8)

**Goal:** Ensure config struct changes are complete, tested, and backward-compatible.

**Changes:**
- Config unit tests:
  - Parse a config YAML with `cli_tools` block — all fields set correctly.
  - Parse a config YAML without `cli_tools` block — safe zero defaults.
  - Parse a config YAML with `enabled: true` and empty `binary_path` — defaults applied correctly at binary resolution.
- Run `go vet ./...` and `golangci-lint run` to verify no `dec.KnownFields` issues.

**Acceptance:** FR-8 acceptance criteria pass.

---

### Phase 10: Full Test Suite Pass, Coverage Check, Lint

**Goal:** All checks green; coverage maintained.

**Steps:**
1. `go test -race -coverprofile=coverage.out ./...` in `boabot/`.
2. `go tool cover -func=coverage.out` — verify `internal/domain/...` and `internal/application/...` are ≥90%.
3. `go fmt ./...` — zero diff.
4. `go vet ./...` — zero warnings.
5. `golangci-lint run` — zero findings.
6. Update `boabot/docs/technical-details.md`.
7. Update `boabot/docs/product-details.md`.
8. Update `boabot/docs/architectural-decision-record.md`.

**Acceptance:** All NFR-2 and NFR-3 criteria met; documentation updated.
