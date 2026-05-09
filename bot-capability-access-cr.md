# Change Request: Bot Capability Access — Skills, Plugins, and CLI Tools

## Summary

Bots currently cannot access installed plugin skills or delegate work to external CLI coding
agents (Claude Code, Codex, OpenAI Codex, OpenCode). This CR fixes a wiring bug that prevents
plugin tools from appearing in non-orchestrator bots' tool lists, adds a `read_skill` tool so
bots can load and follow installed skill instructions, and introduces a long-running CLI tool
subsystem that lets any bot spawn a coding-agent CLI as a monitored subprocess and interact with
it over STDIO for the duration of the task.

---

## Background

### Plugin system (installed but not usable)

The BaoBot plugin system can install Claude Code plugins from registries. The `dev-flow` plugin
(skills: `review-code`, `create-prd`, `implm-from-spec`, etc.) is installed and marked active.
Two bugs prevent bots from using it:

**Bug 1 — plugin store wiring race**: `tm.pluginStore` is set inside the orchestrator bot's
`startBot` goroutine. All other bots start concurrently; those that reach the plugin store check
before the orchestrator sets it receive `nil` and see no plugin tools in `ListTools`. This is a
data race on a shared field.

**Bug 2 — incompatible entrypoint**: When a plugin tool is called, `callPluginTool` tries to
`exec.Command` the plugin entrypoint. Claude Code plugins use `.claude-plugin/plugin.json` as
their entrypoint — a JSON metadata file, not an executable. Attempting to run it produces an exec
format error. BaoBot's plugin execution model (stdin/stdout JSON subprocess) is incompatible with
the Claude Code plugin model (Markdown instruction files read by the Claude Code harness).

The correct fix for Bug 2 is not to add an executable entrypoint but to expose a `read_skill`
MCP tool. Bots read the skill's Markdown instructions, then execute the described steps using
their existing built-in tools (`run_shell`, `read_file`, `write_file`, etc.). This matches how
Claude Code uses skills internally and requires no subprocess for the skill itself.

### CLI tools (providers, not callable tools)

Two external coding CLI tools — Claude Code CLI (`claude`) and OpenAI Codex CLI (`codex`) — are
implemented as `domain.ModelProvider` adapters in `internal/infrastructure/codeagent/provider.go`.
This means a bot can be *statically configured* to use one of these CLIs as its entire inference
engine, but no currently deployed bot is configured that way, and no bot can *dynamically*
delegate a subtask to a CLI tool mid-execution.

The user's intent is the opposite: bots (running their own model) should be able to call CLI
tools as actions — spawn a CLI coding agent for a specific piece of work, stream its STDIO
output, optionally push input if the CLI asks for something, and receive the full result. This is
a peer-delegation pattern, not a model replacement.

Two additional CLI tools have been requested with no existing implementation:
- **OpenAI Codex CLI** (`openai-codex` binary, github.com/openai/codex — distinct from the
  existing `codex` provider if they differ)
- **OpenCode** (`opencode` binary, opencode.ai)

---

## Items

### Item 1 — Fix plugin store wiring race

**File**: `internal/application/team/team_manager.go`

`tm.pluginStore` and `tm.pluginInstallDir` are written by the orchestrator bot's `startBot` call
and read by every other bot's `startBot` call. All run in parallel goroutines with no
synchronisation. Fix: resolve the plugin store before spawning bot goroutines, in the `Run`
method, using only the orchestrator bot's config. Wire the resolved store into every bot's MCP
client at goroutine start. Add a mutex or initialise once before the goroutine loop.

**Acceptance criteria**:
- All bots list active plugin tools regardless of goroutine start order.
- Race detector (`go test -race`) passes on all team package tests.
- No regression in orchestrator-only plugin wiring.

---

### Item 2 — `read_skill` built-in MCP tool

**File**: `internal/infrastructure/local/mcp/client.go` (and domain/tool definition)

Add a new built-in tool `read_skill` to the local MCP client:

```
read_skill(name: string) → string
```

- Looks up the named skill across all active installed plugins by matching the tool name in
  `plugin.Manifest.Provides.Tools`.
- Reads the corresponding `commands/<name>.md` file from the plugin's install directory.
- Returns the Markdown content as the tool result.
- Returns a descriptive error if the skill is not found or the plugin is not active.

The tool description (schema) must clearly explain to the bot that it should read the skill
instructions and then carry out the described steps using its own built-in tools, not look for an
external executor.

Update `callPluginTool` in the MCP client so that if the requested tool name matches an active
plugin tool whose entrypoint is a `.claude-plugin/plugin.json` manifest (non-executable), it
routes to `read_skill` behaviour rather than attempting subprocess execution.

**Acceptance criteria**:
- Bot calls `read_skill("review-code")`, receives the full `commands/review-code.md` content.
- Bot calls `read_skill("nonexistent")`, receives a clear error string.
- Calling a plugin tool whose entrypoint is non-executable returns a useful error rather than
  an exec format error.
- Coverage maintained at ≥90% for domain and application packages.

---

### Item 3 — Long-running CLI tool infrastructure

**New package**: `internal/infrastructure/cliagent/` (or extend `codeagent/` under a new surface)

Introduce a `CLIAgent` type and a `RunCLITool` MCP wrapper that supports long-running subprocess
execution with STDIO monitoring. This is the shared infrastructure for all four CLI tools.

**Execution model**:
- Spawn the CLI binary as a subprocess with `exec.CommandContext`.
- Pipe stdout and stderr. Read line-by-line (or chunk-by-chunk) as the process runs.
- Accumulated output is reported progressively via the existing `progressFn` callback so the
  operator and UI can see real-time output.
- A write channel on stdin allows the caller to push input lines to the subprocess if it blocks
  waiting for a response. This enables mid-run interaction (e.g., answering a CLI confirmation
  prompt).
- On process exit, the full accumulated stdout is returned as the MCP tool result.
- Timeout is configurable per-tool with a generous default (30 minutes). On timeout, SIGTERM
  then SIGKILL after a grace period.

**Domain interface** (in `internal/domain/`):

```go
type CLIAgentConfig struct {
    Binary   string
    WorkDir  string
    Model    string // empty → omit model flag
    Args     []string
    Timeout  time.Duration
}

type CLIAgentRunner interface {
    Run(ctx context.Context, cfg CLIAgentConfig, instruction string,
        stdin <-chan string, progress func(line string)) (string, error)
}
```

The `stdin` channel is optional (nil = no interactive input). `progress` is called for each line
of output. The return value is the complete output.

**Acceptance criteria**:
- Unit tests cover: normal completion, timeout, SIGTERM, stdin write, progress callback.
- Long-running subprocesses do not block the bot's context cancellation.
- No AWS or external dependencies in the domain or application layers.

---

### Item 4 — Claude Code CLI tool (`run_claude_code`)

**MCP tool name**: `run_claude_code`

Wraps the `claude` binary (Claude Code CLI). The existing streaming JSON parser in
`codeagent/provider.go` (parsing `content_block_delta` and `result` events) should be extracted
and reused for STDIO interpretation.

**CLI invocation**:
```
claude --output-format=stream-json --dangerously-skip-permissions [--model <model>] -p <instruction>
```

**Model flag**: `--model <model>` included when `model` is provided.

**Tool schema — `input`**:
| Field | Type | Required | Description |
|---|---|---|---|
| `instruction` | string | yes | Task for Claude Code to perform |
| `work_dir` | string | yes | Working directory |
| `model` | string | no | Claude model ID (e.g. `claude-opus-4-7`) |

**Tool description** (shown to the bot before it calls):
> Run a task using the Claude Code CLI agent. Claude Code has full access to the filesystem and
> shell in the given work directory. It can read and write files, run commands, use git, and
> perform multi-step coding tasks autonomously. Best for complex implementation, refactoring, or
> anything that benefits from Claude's full agentic loop. Specify a model to control cost/quality.
> Returns the complete output when the task finishes.

**Binary path**: configurable via `orchestrator.cli_tools.claude_code.binary_path` in config;
defaults to `"claude"`.

**Acceptance criteria**:
- `run_claude_code` appears in `ListTools` when the `claude` binary is present on PATH or
  configured.
- Tool result contains the text output from Claude Code's streaming JSON events.
- `--model` flag is included when `model` is non-empty, omitted otherwise.
- Progress lines appear in the operator UI as the subprocess runs.

---

### Item 5 — Codex CLI tool (`run_codex`)

**MCP tool name**: `run_codex`

Wraps the `codex` binary (OpenAI Codex CLI, existing plain-text STDIO reader in `codeagent/`
reused).

**CLI invocation**:
```
codex -q --approval-mode=full-auto [--model <model>] <instruction>
```

**Model flag**: `--model <model>` when `model` is provided. Verify against Codex CLI docs that
`--model` is the correct flag; adjust if not.

**Tool schema — `input`**:
| Field | Type | Required | Description |
|---|---|---|---|
| `instruction` | string | yes | Task for Codex to perform |
| `work_dir` | string | yes | Working directory |
| `model` | string | no | Model ID (e.g. `o4-mini`) |

**Tool description**:
> Run a task using the OpenAI Codex CLI agent. Codex has filesystem and shell access in the given
> work directory and runs in fully automatic mode. Best for implementation tasks using OpenAI
> models. Returns the complete output when the task finishes.

**Binary path**: configurable via `orchestrator.cli_tools.codex.binary_path`; defaults to
`"codex"`.

**Acceptance criteria**: same pattern as Item 4.

---

### Item 6 — OpenAI Codex CLI tool (`run_openai_codex`)

**MCP tool name**: `run_openai_codex`

Wraps the `openai-codex` binary (github.com/openai/codex open-source CLI — confirm binary name
and flags from the project README as part of implementation research).

**Implementation note**: If the binary name and flags are identical to the `codex` binary in
Item 5, consolidate into a single dialect/tool rather than duplicating. If they differ, treat
as a distinct dialect with its own flag mapping.

**Model flag**: research the correct flag from the CLI's help output or README.

**Tool description**:
> Run a task using the OpenAI Codex open-source CLI. [Fill in from research: capabilities,
> modes, best-use cases.] Returns complete output on finish.

**Binary path**: configurable via `orchestrator.cli_tools.openai_codex.binary_path`; defaults to
`"openai-codex"`.

**Acceptance criteria**: same pattern as Items 4–5; flags verified against the actual CLI.

---

### Item 7 — OpenCode CLI tool (`run_opencode`)

**MCP tool name**: `run_opencode`

Wraps the `opencode` binary (opencode.ai). Research the CLI's invocation syntax, model flag,
output format, and non-interactive mode flag as the first step of implementation.

**Model flag**: research the correct flag; opencode likely supports `--model` or similar.

**Tool description**:
> Run a task using the OpenCode CLI agent (opencode.ai). [Fill in from research: supported
> providers, capabilities, non-interactive mode, best-use cases.] Returns complete output on
> finish.

**Binary path**: configurable via `orchestrator.cli_tools.opencode.binary_path`; defaults to
`"opencode"`.

**Acceptance criteria**: same pattern as Items 4–6; flags verified against the actual CLI.

---

## Config additions

All CLI tools are opt-in. Add to the orchestrator config struct and YAML:

```yaml
orchestrator:
  cli_tools:
    claude_code:
      enabled: true           # default: false; set true when binary is present
      binary_path: claude     # optional override
    codex:
      enabled: true
      binary_path: codex
    openai_codex:
      enabled: false
      binary_path: openai-codex
    opencode:
      enabled: false
      binary_path: opencode
```

A CLI tool only appears in `ListTools` when `enabled: true` AND the binary resolves on PATH
(or `binary_path` is set to an absolute path). Bots on teams where a CLI tool is not installed
simply do not see the tool.

---

## Architecture notes

### What is NOT changing

- The `codeagent.Provider` model provider adapters (`claude_code`, `codex` provider types in
  `provider_factory.go`) remain unchanged. Operators can still configure a bot to use a CLI as
  its entire inference engine if desired.
- The plugin install/approve/reject lifecycle is unchanged.
- MCP tool call/result semantics are unchanged. Long-running tools block the call until the
  subprocess exits, returning the full output. Progressive output is surfaced via the existing
  `progressFn` path (which updates the task output in the UI in real time).

### Stdin interaction model

The `stdin <-chan string` field on `CLIAgentRunner` allows the BaoBot task loop to forward lines
to the subprocess. In practice this hooks into the existing `drainAsks` mechanism: if the bot
detects a CLI tool call is in progress and a mid-task user question arrives, the answer can be
written to the subprocess stdin channel. The implementation should not block the main task loop
if no input arrives; the channel should be non-blocking with the subprocess allowed to timeout
or self-resolve.

### Clean architecture compliance

- Domain interface (`CLIAgentRunner`, config types) lives in `internal/domain/`.
- Subprocess implementation lives in `internal/infrastructure/cliagent/`.
- MCP client wiring lives in `internal/infrastructure/local/mcp/client.go`.
- No infrastructure imports in domain or application packages.

---

## Out of scope

- Changing how Claude Code plugins are packaged or their registry format.
- Adding executable entrypoints to existing Claude Code plugins.
- Auto-detecting installed CLI tools (explicit config opt-in only).
- Streaming partial results back to the model mid-tool-call (current MCP call/result contract
  is synchronous; progress goes via `progressFn`, not the tool result).

---

## Acceptance criteria (overall)

1. Reviewer bot (and all non-orchestrator bots) see active plugin tools in their tool list
   regardless of startup order; race detector passes.
2. Any bot can call `read_skill("review-code")` and receive the full skill instructions.
3. Any bot can call `run_claude_code`, `run_codex`, `run_openai_codex`, or `run_opencode`
   when the corresponding tool is enabled and the binary is available.
4. Each CLI tool's schema description accurately describes its capabilities.
5. `--model` is passed to the CLI when the field is non-empty and the CLI supports it.
6. Progress output from CLI subprocesses appears in the orchestrator UI task output tab in
   real time.
7. All new code follows TDD (failing test before production code).
8. Coverage ≥90% on domain and application packages; no regression on existing coverage.
9. `go vet`, `golangci-lint`, and `go test -race ./...` pass in the `boabot` module.
10. `docs/technical-details.md`, `docs/product-details.md`, and
    `docs/architectural-decision-record.md` updated to reflect the new capability surface.
