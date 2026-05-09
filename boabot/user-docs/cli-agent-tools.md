# CLI Agent Delegation Tools

Bots can delegate coding tasks to external AI CLI agents — Claude Code, Codex, OpenAI Codex, or opencode — by calling one of four MCP tools. The bot provides a task instruction and a working directory; the CLI agent runs to completion and its output is returned as the tool result.

## When to Use Each Tool

| Tool | Binary | Best for |
|---|---|---|
| `run_claude_code` | `claude` | Tasks that benefit from Claude's reasoning: design review, complex refactoring, test authoring |
| `run_codex` | `codex` | OpenAI Codex CLI tasks: code completion, targeted edits in an existing codebase |
| `run_openai_codex` | `openai-codex` | Open-source OpenAI Codex CLI (different binary from `run_codex`) |
| `run_opencode` | `opencode` | opencode CLI tasks |

All four tools share the same interface. Choose based on which CLI agent is installed and which model/provider you want to use for the subtask.

## Enabling CLI Tools in Config

CLI tools are configured under `orchestrator.cli_tools` in `config.yaml`. By default, all tools are disabled:

```yaml
orchestrator:
  cli_tools:
    claude_code:
      enabled: true
      binary_path: ""          # empty → looks up "claude" on PATH
    codex:
      enabled: false
      binary_path: ""          # empty → looks up "codex" on PATH
    openai_codex:
      enabled: false
      binary_path: ""          # empty → looks up "openai-codex" on PATH
    opencode:
      enabled: false
      binary_path: ""          # empty → looks up "opencode" on PATH
```

Set `enabled: true` for each tool you want bots to use. Optionally set `binary_path` to an absolute path if the binary is not on `PATH` (for example, `binary_path: /usr/local/bin/claude`).

A tool appears in a bot's `ListTools` response if and only if:
1. `enabled: true` is set in config, **and**
2. The binary resolves (via `PATH` lookup or direct `os.Stat` for absolute paths).

Binary availability is checked at `ListTools` call time, not at startup. If you install a binary while the process is running, the tool appears automatically on the next `ListTools` call without a restart.

## Binary Requirements

| Tool | Binary name | Where to get it |
|---|---|---|
| `run_claude_code` | `claude` | [Claude Code docs](https://docs.anthropic.com/claude/docs/claude-code) — install via `npm install -g @anthropic-ai/claude-code` |
| `run_codex` | `codex` | [OpenAI Codex CLI](https://github.com/openai/codex) — install via `npm install -g @openai/codex` |
| `run_openai_codex` | `openai-codex` | Open-source Codex CLI; binary name differs from `codex` |
| `run_opencode` | `opencode` | [opencode](https://opencode.ai) — install per project docs |

The binary must be executable and reachable on `PATH`, or you must specify `binary_path` in config.

## Tool Input Schema

All four tools accept the same three inputs:

| Field | Type | Required | Description |
|---|---|---|---|
| `instruction` | string | yes | The task prompt passed to the CLI agent |
| `work_dir` | string | yes | Working directory for the subprocess |
| `model` | string | no | Model override; passed as `--model <value>` to the CLI |

### The `--model` Parameter

When `model` is non-empty, the value is passed to the CLI with its model flag:

- `run_claude_code`: `claude --model <value> ...`
- `run_codex`: `codex --model <value> ...`
- `run_openai_codex`: `openai-codex --model <value> ...`
- `run_opencode`: `opencode --model <value> ...`

When `model` is empty, each CLI uses its own default model. Valid model values depend on the CLI and your account. For `run_claude_code`, examples include `claude-opus-4-5`, `claude-sonnet-4-5`.

## How a Bot Uses These Tools

A bot uses CLI delegation tools exactly like any other MCP tool. Here is an example of a task description that would cause a tech-lead bot to delegate implementation to Claude Code:

> "Implement the `UserRepository` interface in `internal/infrastructure/db/user_repo.go`. Follow TDD: write failing tests first, then make them pass. Use the existing `WorkItemRepo` in the same package as a reference. The module is at `/workspace/boabot`."

The tech-lead bot would call:

```json
{
  "tool": "run_claude_code",
  "input": {
    "instruction": "Implement the UserRepository interface in internal/infrastructure/db/user_repo.go ...",
    "work_dir": "/workspace/boabot",
    "model": "claude-opus-4-5"
  }
}
```

The `claude` subprocess runs to completion. Each stdout line is forwarded in real time to the operator UI via the `progressFn` mechanism. When the subprocess exits, the accumulated assistant text is returned as the tool result, and the calling bot continues its own task.

## Execution Model

Each tool call spawns the CLI binary as a subprocess with:
- `instruction` appended as the final argument (for `run_claude_code`, passed via `-p`).
- `work_dir` set as the subprocess working directory.
- `--model <value>` injected before the instruction when `model` is non-empty.

Process lifecycle:
1. Context or timeout cancels → `SIGTERM` sent to the subprocess.
2. 5-second grace period for clean shutdown.
3. If still running after 5 seconds → `SIGKILL` via force-close of I/O pipes.

**Default timeout:** 30 minutes. This is a long-running tool by design — CLI agents can take many minutes to complete complex tasks. Do not use CLI delegation tools for tasks that must complete quickly; use the bot's built-in `run_shell` for short commands instead.

## Output Handling

**`run_claude_code`:** Claude Code emits events in `--output-format=stream-json` format. The harness parses each `content_block_delta` event and extracts the text delta. Internal scaffolding events (tool calls, system prompts, metadata) are filtered out. The returned string is the accumulated assistant text only.

**`run_codex`, `run_openai_codex`, `run_opencode`:** These CLIs emit plain-text stdout. All non-empty lines are accumulated and returned as-is.

In all cases, output is returned only after the subprocess exits. There is no mid-call streaming of partial results back to the calling bot — progress lines are forwarded to the operator UI only.

## Limitations

- **Tools appear only when binary is present.** If the binary is not on `PATH` and `binary_path` is not set, the tool is silently absent from `ListTools`. No error or warning is logged.
- **Long-running by design.** The 30-minute default timeout accommodates multi-step implementations. If the CLI hangs (e.g. waiting for interactive input), it will be killed after 30 minutes.
- **Output returned on completion only.** The calling bot cannot inspect partial output mid-execution.
- **No stdin interaction.** The `stdin` channel is supported at the infrastructure level but is not exposed through the MCP tool interface. CLI agents are invoked in non-interactive, full-auto modes that do not require stdin.
- **Orchestrator config section required.** The `cli_tools` block lives under `orchestrator:` even for non-orchestrator bots. Ensure the `orchestrator:` section exists in `config.yaml` before adding `cli_tools`.
