# Spec: Bot Capability Access — Skills, Plugins, and CLI Tools

## Feature Name
Bot Capability Access

## Executive Summary

Bots in the BaoBot system cannot currently access installed plugin skills or delegate work to external CLI coding agents. This spec covers three closely related improvements:

1. Fix a data race that prevents plugin tools from reaching non-orchestrator bots (plugin store wiring race in `TeamManager.Run`).
2. Add a `read_skill` MCP tool that lets bots load and follow Claude Code plugin skill Markdown instructions.
3. Introduce a long-running CLI tool subsystem that allows any bot to spawn Claude Code, Codex, OpenAI Codex, or OpenCode as a monitored subprocess and interact with it over STDIO for the duration of a task.

---

## Goals

1. All bots — regardless of goroutine start order — see active plugin tools in their MCP tool lists with no data race.
2. Any bot can call `read_skill(<name>)` to obtain the full Markdown instructions for an installed plugin skill and then carry out those steps autonomously.
3. Any bot can call `run_claude_code`, `run_codex`, `run_openai_codex`, or `run_opencode` when the corresponding binary is configured and available, delegating a subtask to that CLI agent and receiving its complete output.
4. CLI tool availability is opt-in via config and binary presence; bots on teams where a tool is not installed simply do not see it in `ListTools`.
5. Long-running CLI subprocesses stream progress output to the operator UI in real time via the existing `progressFn` mechanism.
6. All new code is covered by tests to the project's 90% domain+application threshold; the race detector passes.

---

## Non-Goals

- Changing how Claude Code plugins are packaged or modifying the registry format.
- Adding executable entrypoints to existing Claude Code plugins.
- Auto-detecting installed CLI tools; explicit config opt-in is required.
- Streaming partial CLI output back to the model mid-tool-call (progress is surfaced via `progressFn`, not the tool result payload).
- Modifying the `codeagent.Provider` model provider adapters.
- Modifying the plugin install/approve/reject lifecycle.

---

## Functional Requirements

### FR-1 — Fix Plugin Store Wiring Race

Resolve `pluginStore` and `pluginInstallDir` once, before spawning any bot goroutines, in `TeamManager.Run`. Wire the resolved store into every bot's MCP client at goroutine start.

**Detail:**
- Scan loaded bot configs for the orchestrator entry (the one with `Orchestrator.Plugins.InstallDir` set).
- Build `LocalPluginStore` and resolve the install directory path in `Run()`, assigning to local variables rather than struct fields written from goroutines.
- Pass the resolved store into each `startBot` call (or close over local variables) so every goroutine reads an already-initialised value.
- Remove or protect the current struct-field writes inside `startBot` to eliminate the race.
- If no orchestrator config is present or the plugin dir is unset, store remains nil — existing behaviour.

**Acceptance criteria:**
- All bots list active plugin tools regardless of goroutine start order.
- `go test -race ./...` passes on all team-package tests.
- No regression in orchestrator-only plugin wiring or plugin lifecycle management.

---

### FR-2 — `read_skill` Built-in MCP Tool

Add a `read_skill(name: string) → string` built-in tool to the local MCP client. Update `callPluginTool` to route non-executable plugin entrypoints to `read_skill` behaviour instead of attempting subprocess execution.

**Tool schema:**
```json
{
  "name": "read_skill",
  "description": "Read the Markdown instruction file for an installed plugin skill. Returns the full content of commands/<name>.md from the plugin's install directory. After reading, carry out the described steps yourself using your built-in tools (run_shell, read_file, write_file, etc.) — do not look for an external executor. Returns an error string if the skill is not found or its plugin is not active.",
  "input_schema": {
    "type": "object",
    "required": ["name"],
    "properties": {
      "name": {
        "type": "string",
        "description": "The skill name (e.g. \"review-code\", \"create-prd\"). Must match a tool name listed in an active plugin's manifest."
      }
    }
  }
}
```

**Resolution logic:**
1. Iterate active plugins via `pluginStore.List`.
2. For each active plugin, search `plugin.Manifest.Provides.Tools` for a tool whose `Name` matches the requested skill name.
3. On match, read `<installDir>/<pluginName>/commands/<name>.md`.
4. Return the Markdown content as the tool result.
5. If no active plugin provides the named skill, return error string: `skill "<name>" not found in any active plugin`.
6. If the `commands/<name>.md` file cannot be read, return a descriptive file-read error.

**`callPluginTool` update:**
- After locating the plugin and entrypoint path, check whether the entrypoint is a `.claude-plugin/plugin.json` file (detected by filename suffix or `plugin.json` content sniff).
- If the entrypoint is non-executable JSON, delegate to the `read_skill` resolution path rather than calling `exec.Command`.

**Acceptance criteria:**
- `read_skill("review-code")` returns the full `commands/review-code.md` content from the installed `dev-flow` plugin.
- `read_skill("nonexistent")` returns `skill "nonexistent" not found in any active plugin`.
- Calling a plugin tool whose entrypoint is a non-executable `plugin.json` returns the Markdown instructions.
- Calling a plugin tool on a disabled plugin returns a descriptive error.
- Coverage maintained at ≥90% on domain and application packages.

---

### FR-3 — Long-Running CLI Tool Infrastructure (`CLIAgentRunner`)

Introduce a `CLIAgentRunner` domain interface and a subprocess implementation in a new infrastructure package.

**Domain interface** (in `internal/domain/`):
```go
type CLIAgentConfig struct {
    Binary  string
    WorkDir string
    Model   string
    Args    []string
    Timeout time.Duration
}

type CLIAgentRunner interface {
    Run(ctx context.Context, cfg CLIAgentConfig, instruction string,
        stdin <-chan string, progress func(line string)) (string, error)
}
```

**Subprocess implementation** (in `internal/infrastructure/cliagent/`):
- Spawn binary with `exec.CommandContext`.
- Pipe stdout/stderr. Read stdout line-by-line via `bufio.Scanner`.
- Call `progress(line)` for each non-empty line.
- Optional stdin goroutine: non-blocking channel reads, exits on close or context cancellation.
- Default timeout: 30 minutes. On timeout/cancel: SIGTERM → 5s grace → SIGKILL.
- Return accumulated stdout as result string.
- Check binary via `exec.LookPath` before starting.

**Acceptance criteria:**
- Unit tests cover normal completion, timeout, context cancellation, stdin forwarding, progress callback, nil stdin channel.
- Long-running subprocesses do not block context cancellation of the parent bot.
- No AWS or external infrastructure imports in domain or application layers.
- `go test -race ./...` passes.

---

### FR-4 — Claude Code CLI Tool (`run_claude_code`)

Expose the `claude` CLI as an MCP tool available to all bots.

**CLI invocation:**
```
claude --output-format=stream-json --dangerously-skip-permissions [--model <model>] -p <instruction>
```

**Output parsing:** Reuse streaming JSON parser from `codeagent/provider.go` (extracted to shared package `internal/infrastructure/codeagent/stream.go` as `ParseStreamLine`).

**Tool schema input fields:** `instruction` (required), `work_dir` (required), `model` (optional).

**Config path:** `orchestrator.cli_tools.claude_code.enabled` / `orchestrator.cli_tools.claude_code.binary_path`.

**Acceptance criteria:**
- Appears in `ListTools` when `enabled: true` and binary resolves.
- Absent from `ListTools` when `enabled: false` or binary not found.
- Tool result contains text extracted from Claude Code's streaming JSON events.
- `--model <model>` included when `model` is non-empty.
- Progress lines appear in operator UI in real time.

---

### FR-5 — Codex CLI Tool (`run_codex`)

**CLI invocation:**
```
codex -q --approval-mode=full-auto [--model <model>] <instruction>
```

**Output parsing:** Plain-text stdout — accumulate all non-empty lines.

**Config path:** `orchestrator.cli_tools.codex.enabled` / `orchestrator.cli_tools.codex.binary_path`.

**Acceptance criteria:** Same pattern as FR-4. Flags verified against the actual Codex CLI at implementation time.

---

### FR-6 — OpenAI Codex Open-Source CLI Tool (`run_openai_codex`)

**MCP tool name:** `run_openai_codex`

**Implementation note:** Research binary name, invocation syntax, model flag, output format, and non-interactive mode from the project README and CLI help output. If identical to FR-5 codex, consolidate; if different, treat as distinct dialect.

**Config path:** `orchestrator.cli_tools.openai_codex.enabled` / `orchestrator.cli_tools.openai_codex.binary_path` (defaults to `"openai-codex"`).

**Acceptance criteria:** Same pattern as FR-4 and FR-5. CLI flags verified before implementation.

---

### FR-7 — OpenCode CLI Tool (`run_opencode`)

**MCP tool name:** `run_opencode`

**Implementation note:** Research invocation syntax, model flag, output format, and non-interactive mode from opencode documentation before writing implementation code.

**Config path:** `orchestrator.cli_tools.opencode.enabled` / `orchestrator.cli_tools.opencode.binary_path` (defaults to `"opencode"`).

**Acceptance criteria:** Same pattern as FR-4 through FR-6.

---

### FR-8 — Config Additions

**YAML structure:**
```yaml
orchestrator:
  cli_tools:
    claude_code:
      enabled: false
      binary_path: claude
    codex:
      enabled: false
      binary_path: codex
    openai_codex:
      enabled: false
      binary_path: openai-codex
    opencode:
      enabled: false
      binary_path: opencode
```

**Availability rule:** A CLI tool appears in `ListTools` if and only if `enabled: true` AND the binary resolves via `exec.LookPath` or is an existing absolute path. Resolution checked at `ListTools` call time (not startup).

**Acceptance criteria:**
- Config struct parses new fields without error.
- Missing `cli_tools` block uses safe defaults (all disabled).
- `ListTools` omits tool when disabled or binary not found.
- `ListTools` includes tool when enabled and binary resolves.

---

### FR-9 — Capability Advertisement in Tool Schemas

All four CLI tool schema descriptions must be complete, accurate, and distinct. TBD sections from the PRD must be filled in from CLI research during implementation. No placeholder text in shipped code.

---

## Non-Functional Requirements

- **NFR-1 (TDD):** All production code preceded by a failing test.
- **NFR-2 (Coverage):** `internal/domain/...` and `internal/application/...` remain ≥90%.
- **NFR-3 (Static analysis):** `go fmt ./...`, `go vet ./...`, `golangci-lint run`, `go test -race ./...` pass.
- **NFR-4 (Clean Architecture):** Domain interface in `internal/domain/`, subprocess implementation in `internal/infrastructure/cliagent/`, wiring in `internal/infrastructure/local/mcp/client.go`. No infra imports in domain or application.
- **NFR-5 (Documentation):** `boabot/docs/technical-details.md`, `boabot/docs/product-details.md`, `boabot/docs/architectural-decision-record.md` updated.

---

## Overall Acceptance Criteria

1. All bots see active plugin tools in `ListTools` regardless of goroutine start order; `go test -race ./...` passes.
2. Any bot can call `read_skill("review-code")` and receive the full Markdown content.
3. Any bot can call `run_claude_code`, `run_codex`, `run_openai_codex`, or `run_opencode` when enabled and binary resolves.
4. Each CLI tool is absent from `ListTools` when disabled or binary not found.
5. Each CLI tool's schema description accurately and completely describes its capabilities (no TBD placeholders).
6. `--model <value>` (or dialect-specific flag) passed when `model` field is non-empty.
7. Progress output from CLI subprocesses appears in operator UI in real time.
8. All new code follows TDD; coverage remains ≥90%.
9. `go fmt ./...`, `go vet ./...`, `golangci-lint run`, `go test -race ./...` pass.
10. Documentation updated.
