# Tasks: Bot Capability Access

## Phase 1: Fix Plugin Store Race (FR-1)

- [ ] **P1-T1 (RED):** Write a failing race-detector test in `team/` that starts multiple bots concurrently and asserts all receive plugin tools. Verify `go test -race` catches the current bug. (est: 1h)
- [ ] **P1-T2 (RED):** Update `botRunner` function type in `TeamManager` struct to accept `pluginStore domain.PluginStore` and `installDir string` parameters; update `export_test.go` accordingly. Compilation fails until implementation. (est: 30m)
- [ ] **P1-T3 (GREEN):** Add plugin store pre-resolution block in `TeamManager.Run()` before the goroutine loop. Load orchestrator config, build `LocalPluginStore`, capture in local variables. (est: 1.5h)
- [ ] **P1-T4 (GREEN):** Update `startBot` signature to accept pre-resolved store params. Remove struct-field writes `tm.pluginStore = ps` and `tm.pluginInstallDir = pluginInstallDir` from inside `startBot`. Update all call sites. (est: 1h)
- [ ] **P1-T5 (REFACTOR):** Remove `pluginStore` and `pluginInstallDir` fields from `TeamManager` struct if no longer needed. Run `go vet`, confirm no unused fields. (est: 30m)
- [ ] **P1-T6 (VERIFY):** Run `go test -race ./...` in `boabot/`; confirm race detector is clean and P1-T1 test passes. (est: 15m)

## Phase 2: `read_skill` Tool (FR-2)

- [ ] **P2-T1 (RED):** Write failing test in `mcp/` for `read_skill` returning Markdown content for a known skill from a fixture install directory. (est: 45m)
- [ ] **P2-T2 (RED):** Write failing test for `read_skill` returning error string for unknown skill name. (est: 15m)
- [ ] **P2-T3 (RED):** Write failing test for `callPluginTool` with `plugin.json` entrypoint: assert it does NOT attempt exec and returns skill Markdown. (est: 30m)
- [ ] **P2-T4 (GREEN):** Add `readSkill(ctx, args)` method to `mcp.Client`. Implement lookup via `pluginStore.List` + `os.ReadFile`. (est: 1h)
- [ ] **P2-T5 (GREEN):** Add `read_skill` tool definition to `ListTools` (conditional on `pluginStore != nil`). Add `"read_skill"` case to `CallTool`. (est: 30m)
- [ ] **P2-T6 (GREEN):** Update `callPluginTool` to check `strings.HasSuffix(p.Manifest.Entrypoint, "plugin.json")`; if true, call `readSkill` instead of `exec.Command`. (est: 30m)
- [ ] **P2-T7 (REFACTOR):** Review `readSkill` and `callPluginTool` for clarity and consistency with existing error handling patterns. (est: 20m)
- [ ] **P2-T8 (VERIFY):** Run coverage check; confirm ≥90% on `internal/domain/...` and `internal/application/...`. (est: 15m)

## Phase 3: CLIAgentRunner Domain Interface + Infrastructure (FR-3)

- [ ] **P3-T1:** Create `internal/domain/cliagent.go` with `CLIAgentConfig` struct and `CLIAgentRunner` interface. No production logic, no test yet. (est: 20m)
- [ ] **P3-T2:** Generate or hand-write `internal/domain/mocks/MockCLIAgentRunner`. (est: 20m)
- [ ] **P3-T3 (RED):** Write failing test for `SubprocessRunner.Run` — normal completion accumulates all stdout lines. Use a short real subprocess (e.g. `echo`). (est: 45m)
- [ ] **P3-T4 (RED):** Write failing test — timeout triggers SIGTERM; after WaitDelay SIGKILL; subprocess halts. (est: 45m)
- [ ] **P3-T5 (RED):** Write failing test — context cancellation sends SIGTERM. (est: 30m)
- [ ] **P3-T6 (RED):** Write failing test — stdin channel input is forwarded to subprocess stdin. (est: 45m)
- [ ] **P3-T7 (RED):** Write failing test — progress callback invoked for each non-empty output line. (est: 30m)
- [ ] **P3-T8 (RED):** Write failing test — nil stdin channel: subprocess completes normally without blocking. (est: 20m)
- [ ] **P3-T9 (RED):** Write failing test — binary not found returns a clear error. (est: 15m)
- [ ] **P3-T10 (GREEN):** Implement `internal/infrastructure/cliagent/runner.go` — `SubprocessRunner` with all required behaviour. (est: 3h)
- [ ] **P3-T11 (REFACTOR):** Extract shared helper functions; ensure goroutine leak prevention; add `t.Cleanup` in tests to wait for goroutine exit. (est: 1h)
- [ ] **P3-T12 (VERIFY):** `go test -race ./internal/infrastructure/cliagent/...` — all tests pass, race clean. (est: 15m)

## Phase 4: Extract Streaming JSON Parser (prerequisite for FR-4)

- [ ] **P4-T1 (RED):** Write failing tests in `codeagent/stream_test.go` for `ParseStreamLine` covering: `content_block_delta` event, `result` event, unknown event type (returns `"", true`), malformed JSON (returns `"", false`). (est: 30m)
- [ ] **P4-T2 (GREEN):** Create `internal/infrastructure/codeagent/stream.go` with `ParseStreamLine`, `streamEvent`, `deltaField`. (est: 20m)
- [ ] **P4-T3 (GREEN):** Update `provider.go` to call `ParseStreamLine` instead of `extractText`; remove `extractText`, `streamEvent`, `deltaField` from `provider.go`. (est: 20m)
- [ ] **P4-T4 (VERIFY):** Run existing `provider_test.go` tests — no regression. (est: 10m)

## Phase 5: `run_claude_code` Tool (FR-4)

- [ ] **P5-T1:** Add `CLIToolConfig` and `CLIToolsConfig` structs to `config/config.go`. Add `CLITools CLIToolsConfig` to `OrchestratorConfig`. (est: 20m)
- [ ] **P5-T2 (RED):** Write failing config test: parse YAML with `cli_tools` block; verify all fields loaded. (est: 20m)
- [ ] **P5-T3 (RED):** Write failing config test: parse YAML without `cli_tools` block; verify zero defaults. (est: 15m)
- [ ] **P5-T4 (GREEN):** Config struct changes satisfy tests. (est: 10m)
- [ ] **P5-T5:** Add `cliRunner domain.CLIAgentRunner`, `cliTools config.CLIToolsConfig`, `progressFn func(line string)` fields to `mcp.Client`. Add `WithCLIRunner`, `WithCLITools`, `WithProgressFn` option functions. (est: 30m)
- [ ] **P5-T6 (RED):** Write failing test: `ListTools` includes `run_claude_code` when `ClaudeCode.Enabled=true` and a fake binary exists on PATH. (est: 30m)
- [ ] **P5-T7 (RED):** Write failing test: `ListTools` excludes `run_claude_code` when disabled. (est: 15m)
- [ ] **P5-T8 (RED):** Write failing test: `ListTools` excludes `run_claude_code` when enabled but binary not found. (est: 15m)
- [ ] **P5-T9 (RED):** Write failing test: `CallTool("run_claude_code", ...)` invokes `cliRunner.Run` with correct args (including `--output-format=stream-json`, `--dangerously-skip-permissions`, `-p`). Use `MockCLIAgentRunner`. (est: 45m)
- [ ] **P5-T10 (RED):** Write failing test: `--model <value>` included in args when `model` field non-empty. (est: 20m)
- [ ] **P5-T11 (RED):** Write failing test: `--model` omitted when `model` field empty. (est: 15m)
- [ ] **P5-T12 (RED):** Write failing test: progress callback invoked for each line from runner. (est: 20m)
- [ ] **P5-T13 (GREEN):** Implement `run_claude_code` in `ListTools`, `resolveBinary` helper, `callCLITool`, `CallTool` case, stream-JSON post-processing. (est: 2h)
- [ ] **P5-T14 (GREEN):** Wire `cliRunner`, `cliTools`, `progressFn` in `team_manager.go` MCP client construction. (est: 45m)
- [ ] **P5-T15 (REFACTOR):** Clean up binary resolution helper; ensure it's tested independently. (est: 20m)

## Phase 6: `run_codex` Tool (FR-5)

- [ ] **P6-T1 (RED):** Write failing tests: `ListTools` includes/excludes `run_codex` per enabled/binary-available rules. (est: 20m)
- [ ] **P6-T2 (RED):** Write failing test: `CallTool("run_codex", ...)` invokes `cliRunner.Run` with correct args (`-q`, `--approval-mode=full-auto`, optional model, instruction). (est: 30m)
- [ ] **P6-T3 (GREEN):** Implement `run_codex` tool in `client.go`. (est: 1h)
- [ ] **P6-T4:** Verify `--model` flag against actual Codex CLI; update args in test fixtures if flag differs from `--model`. Document in `implementation-notes.md`. (est: 30m)

## Phase 7: Research + Implement `run_openai_codex` (FR-6)

- [ ] **P7-T1 (RESEARCH):** Run `openai-codex --help` or inspect README. Record binary name, non-interactive flag, model flag, output format in `implementation-notes.md`. (est: 30m)
- [ ] **P7-T2 (RED):** Write failing tests for `run_openai_codex` (same pattern as Phase 6). (est: 30m)
- [ ] **P7-T3 (GREEN):** Implement `run_openai_codex` in `client.go`. Update tool description with researched capabilities. (est: 1h)
- [ ] **P7-T4 (VERIFY):** No TBD placeholders in tool description. (est: 10m)

## Phase 8: Research + Implement `run_opencode` (FR-7)

- [ ] **P8-T1 (RESEARCH):** Check opencode.ai docs / `opencode --help`. Record non-interactive mode, model flag, output format in `implementation-notes.md`. (est: 30m)
- [ ] **P8-T2 (RED):** Write failing tests for `run_opencode` (same pattern). (est: 30m)
- [ ] **P8-T3 (GREEN):** Implement `run_opencode` in `client.go`. Update tool description. (est: 1h)
- [ ] **P8-T4 (VERIFY):** No TBD placeholders in tool description. (est: 10m)

## Phase 9: Config Validation + Wiring Review

- [ ] **P9-T1:** Run `go build ./...` in `boabot/` — confirm compilation clean. (est: 10m)
- [ ] **P9-T2:** Run `go vet ./...` — zero warnings. Fix any issues. (est: 15m)
- [ ] **P9-T3:** Run `golangci-lint run` — zero findings. Fix any issues. (est: 30m)

## Phase 10: Full Suite Pass + Coverage + Documentation

- [ ] **P10-T1:** `go test -race -coverprofile=coverage.out ./...` in `boabot/`. (est: 10m)
- [ ] **P10-T2:** `go tool cover -func=coverage.out` — verify `internal/domain/...` and `internal/application/...` ≥90%. Add tests if needed to reach threshold. (est: 1h)
- [ ] **P10-T3:** Update `boabot/docs/technical-details.md` — new packages, `CLIAgentRunner`, plugin store race fix. (est: 45m)
- [ ] **P10-T4:** Update `boabot/docs/product-details.md` — `read_skill`, CLI tool capabilities. (est: 30m)
- [ ] **P10-T5:** Update `boabot/docs/architectural-decision-record.md` — `read_skill` over executable entrypoints; `CLIAgentRunner` separate from `codeagent.Provider`. (est: 30m)
- [ ] **P10-T6:** Final `go test -race ./...` — clean pass. (est: 10m)
