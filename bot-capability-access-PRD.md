# PRD: Bot Capability Access — Skills, Plugins, and CLI Tools

## Executive Summary

Bots in the BaoBot system cannot currently access installed plugin skills or delegate work to external CLI coding agents. This PRD covers three closely related improvements: fixing a data race that prevents plugin tools from reaching non-orchestrator bots, adding a `read_skill` MCP tool that lets bots load and follow Claude Code plugin skill instructions, and introducing a long-running CLI tool subsystem that allows any bot to spawn Claude Code, Codex, OpenAI Codex, or OpenCode as a monitored subprocess and interact with it over STDIO for the duration of a task.

---

## Problem Statement

### Plugin tools invisible to non-orchestrator bots

The `dev-flow` plugin (providing skills such as `review-code`, `create-prd`, and `implm-from-spec`) is installed and marked active, yet no bot can use it. Two bugs combine to produce this failure:

**Bug 1 — plugin store wiring race.** `TeamManager.pluginStore` and `TeamManager.pluginInstallDir` are written inside the orchestrator bot's `startBot` goroutine (lines ~597–598 of `team_manager.go`). All other bot goroutines start concurrently. A non-orchestrator bot that reaches the MCP-client wiring block (lines ~708–715) before the orchestrator goroutine assigns `tm.pluginStore` receives `nil` and its MCP client is built without plugin support. This is an unguarded data race on a shared struct field. Every bot except the orchestrator may see an empty plugin tool list depending on goroutine scheduling.

**Bug 2 — incompatible plugin entrypoint.** When a plugin tool is called, `callPluginTool` in `local/mcp/client.go` runs `exec.Command` on `plugin.Manifest.Entrypoint`. Claude Code plugins store a JSON metadata file (`/.claude-plugin/plugin.json`) as their entrypoint — not an executable. Executing a JSON file produces an exec format error. The BaoBot subprocess model (stdin/stdout JSON exchange) is fundamentally incompatible with the Claude Code plugin model, which uses Markdown instruction files consumed directly by the Claude Code harness.

The correct fix for Bug 2 is not to add a shim executable but to expose a `read_skill` MCP tool. A bot reads the skill's Markdown instructions and then executes the described steps using its own built-in tools (`run_shell`, `read_file`, `write_file`, etc.). This matches how Claude Code itself uses skills and requires no subprocess for the skill invocation.

### CLI tools are model providers, not callable actions

`codeagent/provider.go` implements `claude` and `codex` CLI dialects as `domain.ModelProvider` adapters. A bot can be statically configured to use one of these CLIs as its entire inference engine, but no deployed bot is configured that way, and — more importantly — no bot can dynamically delegate a subtask to a CLI agent mid-execution. The desired pattern is peer delegation: a bot running its own model calls a CLI tool, streams its output, and receives the result. This pattern is absent from the current codebase.

Two additional CLI agents have been requested with no existing implementation: the OpenAI Codex open-source CLI (`openai-codex` binary, github.com/openai/codex) and OpenCode (`opencode` binary, opencode.ai).

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
- Streaming partial CLI output back to the model mid-tool-call (the MCP call/result contract is synchronous; progress is surfaced via `progressFn`, not the tool result payload).
- Modifying the `codeagent.Provider` model provider adapters; those remain for operators who want to run a bot entirely on a CLI backend.
- Modifying the plugin install/approve/reject lifecycle.

---

## Functional Requirements

### FR-1 — Fix Plugin Store Wiring Race

**What:** Resolve `pluginStore` and `pluginInstallDir` once, before spawning any bot goroutines, in the `TeamManager.Run` method. Wire the resolved store into every bot's MCP client at goroutine start rather than writing to shared fields from within goroutines.

**Detail:**
- Scan the loaded bot configs for the orchestrator entry (the one with `Orchestrator.Plugins.InstallDir` set).
- Build the `LocalPluginStore` and resolve the install directory path in `Run()`, assigning to local variables rather than struct fields written from goroutines.
- Pass the resolved store into each `startBot` call (or close over local variables) so every goroutine reads an already-initialised value.
- Remove or protect the current struct-field writes inside `startBot` to eliminate the race.
- If no orchestrator config is present or the plugin dir is unset, store remains nil — existing behaviour.

**Acceptance criteria:**
- All bots list active plugin tools regardless of goroutine start order.
- `go test -race ./...` passes on all team-package tests, including tests that start multiple goroutines in parallel.
- No regression in orchestrator-only plugin wiring or plugin lifecycle management.

---

### FR-2 — `read_skill` Built-in MCP Tool

**What:** Add a `read_skill(name: string) → string` built-in tool to the local MCP client. Update `callPluginTool` to route non-executable plugin entrypoints to `read_skill` behaviour instead of attempting subprocess execution.

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
5. If no active plugin provides the named skill, return a clear error string: `skill "<name>" not found in any active plugin`.
6. If the `commands/<name>.md` file cannot be read, return a descriptive file-read error.

**`callPluginTool` update:**
- After locating the plugin and entrypoint path, check whether the entrypoint is a `.claude-plugin/plugin.json` file (detected by filename suffix or by `plugin.json` content sniff).
- If the entrypoint is non-executable JSON, delegate to the `read_skill` resolution path above rather than calling `exec.Command`.
- The `read_skill` tool is also callable directly by a bot as a built-in (appears in `ListTools`).

**Acceptance criteria:**
- `read_skill("review-code")` returns the full `commands/review-code.md` content from the installed `dev-flow` plugin.
- `read_skill("nonexistent")` returns the error string `skill "nonexistent" not found in any active plugin`.
- Calling a plugin tool whose entrypoint is a non-executable `plugin.json` returns the Markdown instructions (not an exec format error).
- Calling a plugin tool on a disabled plugin returns a descriptive error.
- Coverage maintained at ≥90% on domain and application packages.

---

### FR-3 — Long-Running CLI Tool Infrastructure (`CLIAgentRunner`)

**What:** Introduce a `CLIAgentRunner` domain interface and a subprocess implementation in a new infrastructure package. This is the shared foundation for all four CLI tools.

**Domain interface** (in `internal/domain/`):

```go
// CLIAgentConfig holds parameters for a single CLI agent invocation.
type CLIAgentConfig struct {
    Binary  string        // executable name or absolute path
    WorkDir string        // subprocess working directory
    Model   string        // model ID; empty means omit the model flag
    Args    []string      // dialect-specific arguments prepended before the instruction
    Timeout time.Duration // 0 means use the default (30 minutes)
}

// CLIAgentRunner executes a long-running CLI coding agent as a supervised subprocess.
type CLIAgentRunner interface {
    Run(ctx context.Context, cfg CLIAgentConfig, instruction string,
        stdin <-chan string, progress func(line string)) (string, error)
}
```

**Subprocess implementation** (in `internal/infrastructure/cliagent/`):

Execution model:
- Spawn the binary with `exec.CommandContext` using `cfg.Args` + instruction.
- Pipe stdout and stderr. Read stdout line-by-line via `bufio.Scanner`.
- Call `progress(line)` for each non-empty line. Accumulate all lines.
- If `stdin` channel is non-nil, a separate goroutine reads from it (non-blocking) and writes lines to the subprocess stdin pipe. If the channel is closed or the context is done, the goroutine exits. The goroutine must not block the main task loop if no input arrives.
- Default timeout is 30 minutes. If `cfg.Timeout > 0` it overrides the default.
- On timeout or context cancellation: send SIGTERM; wait up to 5 seconds for graceful exit; then send SIGKILL.
- Return the complete accumulated stdout as the result string.
- Binary availability: check via `exec.LookPath` before starting; return a clear error if not found.

**Acceptance criteria:**
- Unit tests cover: normal completion with accumulated output, timeout triggering SIGTERM then SIGKILL, explicit context cancellation, stdin write forwarded to subprocess, progress callback called for each output line.
- Long-running subprocesses do not block context cancellation of the parent bot.
- No AWS or other external infrastructure imports in the domain or application layers.
- `go test -race ./...` passes on all cliagent package tests.

---

### FR-4 — Claude Code CLI Tool (`run_claude_code`)

**What:** Expose the `claude` CLI as an MCP tool available to all bots.

**MCP tool name:** `run_claude_code`

**CLI invocation:**
```
claude --output-format=stream-json --dangerously-skip-permissions [--model <model>] -p <instruction>
```

**Output parsing:** Reuse the streaming JSON parser already present in `codeagent/provider.go` (`extractText` function and `streamEvent`/`deltaField` types). Extract these into a shared internal package or pass through `CLIAgentRunner` with a post-processing step. The `progress` callback receives each parsed text line; the final result is the concatenated text from all `content_block_delta` and `result` events.

**Tool schema input fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `instruction` | string | yes | Task for Claude Code to perform |
| `work_dir` | string | yes | Working directory for the subprocess |
| `model` | string | no | Claude model ID (e.g. `claude-opus-4-7`); omitted from CLI args when empty |

**Tool description (verbatim in schema):**
> Run a task using the Claude Code CLI agent. Claude Code has full access to the filesystem and shell in the given work directory. It can read and write files, run commands, use git, and perform multi-step coding tasks autonomously. Best for complex implementation, refactoring, or anything that benefits from Claude's full agentic loop. Specify a model to control cost/quality. Returns the complete output when the task finishes.

**Config path:** `orchestrator.cli_tools.claude_code.enabled` / `orchestrator.cli_tools.claude_code.binary_path` (defaults to `"claude"`).

**Acceptance criteria:**
- `run_claude_code` appears in `ListTools` when `enabled: true` and the `claude` binary resolves on PATH or via `binary_path`.
- `run_claude_code` is absent from `ListTools` when `enabled: false` or binary is not found.
- Tool result contains text extracted from Claude Code's streaming JSON events.
- `--model <model>` is included in the CLI args when `model` is non-empty; omitted otherwise.
- Progress lines appear in the operator UI task output in real time.

---

### FR-5 — Codex CLI Tool (`run_codex`)

**What:** Expose the `codex` CLI as an MCP tool.

**MCP tool name:** `run_codex`

**CLI invocation:**
```
codex -q --approval-mode=full-auto [--model <model>] <instruction>
```

**Output parsing:** Plain-text stdout — accumulate all non-empty lines directly (reuse the `DialectCodex` reader from `codeagent/provider.go`).

**Tool schema input fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `instruction` | string | yes | Task for Codex to perform |
| `work_dir` | string | yes | Working directory for the subprocess |
| `model` | string | no | Model ID (e.g. `o4-mini`); omitted when empty |

**Tool description (verbatim in schema):**
> Run a task using the OpenAI Codex CLI agent. Codex has filesystem and shell access in the given work directory and runs in fully automatic mode. Best for implementation tasks using OpenAI models. Returns the complete output when the task finishes.

**Model flag:** `--model <model>` — verify this is the correct flag against Codex CLI documentation at implementation time; adjust if not.

**Config path:** `orchestrator.cli_tools.codex.enabled` / `orchestrator.cli_tools.codex.binary_path` (defaults to `"codex"`).

**Acceptance criteria:** Same pattern as FR-4. Flags verified against the actual Codex CLI at implementation time.

---

### FR-6 — OpenAI Codex Open-Source CLI Tool (`run_openai_codex`)

**What:** Expose the `openai-codex` binary (github.com/openai/codex open-source project) as an MCP tool.

**MCP tool name:** `run_openai_codex`

**Implementation note:** At implementation time, research the binary name, invocation syntax, model flag, output format, and non-interactive mode from the project README and CLI help output. If the binary name and flags are identical to the `codex` binary in FR-5, consolidate into a single dialect rather than duplicating code. If they differ, treat as a distinct dialect.

**Tool schema input fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `instruction` | string | yes | Task to perform |
| `work_dir` | string | yes | Working directory |
| `model` | string | no | Model ID; omitted when empty |

**Tool description (verbatim in schema):**
> Run a task using the OpenAI Codex open-source CLI (github.com/openai/codex). [TBD: fill in capabilities, modes, and best-use cases from CLI research.] Returns complete output on finish.

**Config path:** `orchestrator.cli_tools.openai_codex.enabled` / `orchestrator.cli_tools.openai_codex.binary_path` (defaults to `"openai-codex"`).

**Acceptance criteria:** Same pattern as FR-4 and FR-5. CLI flags and output format verified against the actual binary before implementation is complete.

---

### FR-7 — OpenCode CLI Tool (`run_opencode`)

**What:** Expose the `opencode` binary (opencode.ai) as an MCP tool.

**MCP tool name:** `run_opencode`

**Implementation note:** Research the invocation syntax, model flag (likely `--model` or similar), output format, and non-interactive mode from the opencode documentation and CLI help output before writing implementation code.

**Tool schema input fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `instruction` | string | yes | Task to perform |
| `work_dir` | string | yes | Working directory |
| `model` | string | no | Model ID; omitted when empty |

**Tool description (verbatim in schema):**
> Run a task using the OpenCode CLI agent (opencode.ai). [TBD: fill in supported providers, capabilities, non-interactive mode, and best-use cases from CLI research.] Returns complete output on finish.

**Config path:** `orchestrator.cli_tools.opencode.enabled` / `orchestrator.cli_tools.opencode.binary_path` (defaults to `"opencode"`).

**Acceptance criteria:** Same pattern as FR-4 through FR-6. CLI flags and output format verified before implementation.

---

### FR-8 — Config Additions

**What:** Add `orchestrator.cli_tools` to the config struct and YAML schema.

**YAML structure:**
```yaml
orchestrator:
  cli_tools:
    claude_code:
      enabled: false          # default: false
      binary_path: claude     # optional override; defaults to "claude"
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

**Availability rule:** A CLI tool appears in `ListTools` if and only if:
1. Its `enabled` field is `true`, AND
2. The binary resolves: either `binary_path` is an absolute path to an existing file, or `exec.LookPath(binary_path)` succeeds.

Bots on teams where a CLI tool is not installed simply do not see the tool. Binary resolution is checked at `ListTools` call time (not at startup) so that a tool installed after startup becomes available without a restart.

**Acceptance criteria:**
- Config struct parses the new fields without error.
- Missing `cli_tools` block uses safe defaults (all disabled).
- `ListTools` omits a tool when `enabled: false`.
- `ListTools` omits a tool when `enabled: true` but the binary is not found.
- `ListTools` includes a tool when `enabled: true` and the binary resolves.

---

### FR-9 — Capability Advertisement in Tool Schemas

**What:** Each CLI tool's schema description must accurately convey to the bot what the tool does, what level of autonomy the CLI has, and when to prefer it.

**Requirement:** The descriptions defined in FR-4 through FR-7 are the required verbatim baseline. TBD sections must be filled in from CLI research during implementation. Descriptions must not be left as placeholders in shipped code.

**Acceptance criteria:**
- All four tool descriptions are complete and accurate in the shipped implementation.
- Tool descriptions are distinct and do not reuse identical text across tools.

---

## Non-Functional Requirements

### NFR-1 — Test-Driven Development

All production code for this feature must be preceded by a failing test. The red-green-refactor cycle is mandatory. This applies to bug fixes (FR-1, FR-2 entrypoint fix) as well as new features. No production code is merged without a corresponding failing test committed first.

### NFR-2 — Coverage

Coverage on `internal/domain/...` and `internal/application/...` packages (excluding `mocks/` subdirectories) must remain at or above 90% after all changes land. New packages introduced by this feature (`internal/infrastructure/cliagent/`, updates to `internal/infrastructure/local/mcp/`) must not reduce overall coverage. `cmd/`, `mocks/`, and `config/` packages are excluded from the threshold.

### NFR-3 — Static Analysis

All code must pass:
- `go fmt ./...` — zero diff
- `go vet ./...` — zero warnings
- `golangci-lint run` — zero findings (project `.golangci.yml` configuration)
- `go test -race ./...` — zero race reports

All checks run in the `boabot` module.

### NFR-4 — Clean Architecture

Domain interface (`CLIAgentRunner`, `CLIAgentConfig`) lives in `internal/domain/`. Subprocess implementation lives in `internal/infrastructure/cliagent/`. MCP client wiring lives in `internal/infrastructure/local/mcp/client.go`. No infrastructure imports (AWS SDK, `os/exec`, etc.) appear in `internal/domain/` or `internal/application/` packages.

### NFR-5 — Documentation

The following documentation files must be updated when this feature lands:
- `boabot/docs/technical-details.md` — new packages, `CLIAgentRunner` interface, plugin store race fix
- `boabot/docs/product-details.md` — `read_skill` and CLI tool capabilities visible to bots
- `boabot/docs/architectural-decision-record.md` — decision to use `read_skill` over executable plugin entrypoints; decision to introduce `CLIAgentRunner` domain interface separate from `codeagent.Provider`

---

## Architecture and Design Constraints

### CLIAgentRunner is a domain interface

`CLIAgentRunner` is defined in `internal/domain/` alongside other core interfaces. The subprocess implementation is infrastructure and lives in `internal/infrastructure/cliagent/`. The MCP client receives a `CLIAgentRunner` by injection — it does not instantiate the subprocess implementation directly. This preserves Clean Architecture boundaries and enables unit testing with mock runners.

### CLI tool availability check

`ListTools` calls `exec.LookPath(cfg.BinaryPath)` for each enabled CLI tool at call time. Tools that are not enabled or whose binary is not found are omitted silently. This check must not fail noisily — absence of a binary is a normal condition on teams where that tool is not installed.

### Long-running execution model

- Line-by-line STDIO reading via `bufio.Scanner` on stdout.
- `progress(line)` called for each non-empty line.
- Optional `stdin <-chan string`: a goroutine drains the channel and writes to subprocess stdin, exiting when the channel is closed or context is cancelled. The goroutine uses a non-blocking channel read with a select to avoid blocking the main task loop.
- Configurable timeout; default 30 minutes. On timeout: SIGTERM, 5-second grace period, then SIGKILL.
- WaitDelay set on `exec.Cmd` (500 ms recommended) to force-close I/O pipes after context cancellation.

### Streaming JSON parser reuse

The `extractText` function and associated event types in `codeagent/provider.go` parse Claude Code's `--output-format=stream-json` STDIO. For `run_claude_code`, this logic must be accessible from the new MCP tool implementation. Extract it into a shared internal package (e.g., `internal/infrastructure/codeagent/stream.go` exported as `ParseStreamLine`) rather than duplicating it. The `codeagent.Provider` remains unchanged and continues to use the same function.

### Plugin store race fix approach

Resolve the plugin store before the bot goroutine loop in `TeamManager.Run()`. Pass it into `startBot` as a parameter or capture it in a local variable before the `go func()` launch. Do not write to `tm.pluginStore` from inside a goroutine. If a mutex is used, acquire it before the goroutine loop and release after — the simplest correct approach is to eliminate the struct-field write from goroutines entirely.

### Model flag passthrough

`--model <value>` (or the dialect-specific equivalent flag) is included in the CLI argument list if and only if `CLIAgentConfig.Model` is non-empty. The `CLIAgentRunner` implementation must support this by including the model arg in `cfg.Args` when the MCP tool wiring builds the config, or by handling `Model` explicitly in the subprocess runner. The exact flag name per CLI must be verified at implementation time for dialects other than Claude Code and Codex.

### Existing `codeagent.Provider` unchanged

The `claude_code` and `codex` provider types in `provider_factory.go` remain available for operators who want a bot to use a CLI as its entire inference engine. This feature adds a peer-delegation path (MCP tool) on top of the existing model-replacement path. Both paths coexist independently.

---

## Dependencies and Risks

### R-1 — OpenAI Codex and OpenCode CLI flag research [TBD]

The invocation syntax, model flag name, output format (plain text vs structured), and non-interactive mode flag for `openai-codex` and `opencode` are not confirmed. These must be researched by inspecting the CLI's `--help` output and README before implementation code is written. If `openai-codex` and `codex` are identical binaries, consolidate to one dialect. If `opencode` lacks a non-interactive mode, this requirement may need to be narrowed.

**Mitigation:** Make FR-6 and FR-7 implementation tasks explicitly gated on a research step. Implementation notes must record the researched flags before tests are written.

### R-2 — Stdin interaction must not block the main task loop

The `stdin <-chan string` mechanism enables mid-run interaction with CLI subprocesses (e.g., answering a confirmation prompt). If implemented naively, a blocking channel read could stall the bot. The subprocess stdin goroutine must use non-blocking channel semantics (`select` with a `default` case or `ctx.Done()` select arm).

**Mitigation:** Unit test specifically for the case where no stdin input arrives and the subprocess completes normally without blocking.

### R-3 — Claude Code streaming JSON format stability

The `extractText` parser targets specific event types (`content_block_delta`, `result`). If Anthropic changes the streaming JSON schema, parsed output will degrade silently. This is an existing risk in `codeagent/provider.go` and is not introduced by this feature.

### R-4 — Binary availability at runtime on ECS

The `claude`, `codex`, `openai-codex`, and `opencode` binaries are not included in the BaoBot container image. Enabling CLI tools requires those binaries to be present in the execution environment. Operators must install them in the container image or via a side-car. The `enabled: false` default prevents misconfiguration.

**Mitigation:** Document the binary installation requirement in `boabot/user-docs/`. Config defaults to disabled for all CLI tools.

---

## Acceptance Criteria (Overall)

1. All bots (reviewer, developer, and any non-orchestrator bot) see active plugin tools in their `ListTools` output regardless of goroutine start order; `go test -race ./...` passes with no race reports.
2. Any bot can call `read_skill("review-code")` and receive the full Markdown content of `commands/review-code.md` from the installed `dev-flow` plugin.
3. Any bot can call `run_claude_code`, `run_codex`, `run_openai_codex`, or `run_opencode` when the corresponding tool is `enabled: true` and the binary resolves.
4. Each CLI tool is absent from `ListTools` when disabled or when its binary is not found.
5. Each CLI tool's schema description accurately and completely describes its capabilities (no TBD placeholders in shipped code).
6. `--model <value>` (or the dialect-specific flag) is passed to the CLI when the `model` field is non-empty and omitted otherwise.
7. Progress output from CLI subprocesses appears in the orchestrator UI task output tab in real time, routed through the existing `progressFn` mechanism.
8. All new code follows TDD: failing test precedes every production code change.
9. Coverage on `internal/domain/...` and `internal/application/...` remains at or above 90%; no regression on existing coverage.
10. `go fmt ./...`, `go vet ./...`, `golangci-lint run`, and `go test -race ./...` pass in the `boabot` module.
11. `boabot/docs/technical-details.md`, `boabot/docs/product-details.md`, and `boabot/docs/architectural-decision-record.md` are updated to reflect the new capability surface.
