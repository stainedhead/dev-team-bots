# Plugin Skills

Some plugins provide "skills" — Markdown instruction files that describe multi-step workflows a bot can execute autonomously using its built-in tools. The `dev-flow` plugin is the primary example: it ships skills like `review-code`, `create-prd`, and `implm-from-spec`.

This page explains how plugin skills work, how a bot discovers and uses them, and how to troubleshoot common issues.

## What a Plugin Skill Is

A plugin skill is a Markdown file (`commands/<name>.md`) inside a plugin archive. When a plugin declares a tool in its `plugin.yaml` manifest and the plugin uses a `plugin.json` entrypoint (the Claude Code plugin format), the tool's "implementation" is that Markdown file.

The Markdown file contains step-by-step instructions: what to read, what to write, what API calls to make, how to handle edge cases. The bot reads these instructions and carries out each step using its own built-in tools — there is no separate executor.

**Example:** The `dev-flow` plugin ships `commands/review-code.md`. When a bot calls `read_skill("review-code")`, it receives the full Markdown and then performs the code review by calling `read_file`, `write_file`, `http_request`, and other built-in tools as directed.

## Installing Plugins with Skills

Plugin installation follows the same process as any other plugin. Skills are not a separate install step.

1. From the admin UI **Plugins & Skills** tab, browse the catalog for the registry that hosts your plugin (e.g. `stainedhead/shared-plugins`).
2. Click **Install** next to the plugin. If the registry is trusted, the plugin activates immediately. If untrusted, it lands in `staged` status and requires admin approval.
3. Once the plugin status is `active`, its skills are available to all bots on the next `ListTools` call.

From the CLI:

```bash
boabotctl plugin install dev-flow --version 1.0.0
```

The `read_skill` tool appears in every bot's `ListTools` response as soon as at least one plugin with a `plugin.json` entrypoint is active.

## How a Bot Discovers Available Skills

A bot does not need to enumerate skills upfront. The `read_skill` tool is always present when a plugin store is configured. The bot discovers skills by calling `read_skill` with the skill name it wants to use.

If the bot needs to know which skills are available before deciding which to call, it can use the standard MCP `ListTools` response — the tools listed there include one entry per skill from each active plugin. The tool name is the skill name. The tool description summarises what the skill does.

For example, if `dev-flow` is active, `ListTools` includes:

```json
{
  "name": "review-code",
  "description": "Run a structured code and design review on the current branch changes."
}
```

The bot can match its current task against available tool descriptions before deciding to call `read_skill`.

## How a Bot Uses a Skill

### Step 1 — Call `read_skill`

```json
{
  "tool": "read_skill",
  "input": {
    "name": "review-code"
  }
}
```

### Step 2 — Receive the Markdown instructions

The tool returns the full content of `commands/review-code.md`. This is a multi-step workflow description, typically including:
- What context to gather (branch diff, spec files, etc.)
- What criteria to apply
- How to format the output
- Where to write results

### Step 3 — Execute each step with built-in tools

The bot reads the instructions and calls its own tools step by step. For example, the `review-code` skill might direct the bot to:
1. Call `run_shell` to get the `git diff` of the current branch.
2. Call `read_file` to load the spec from `specs/`.
3. Perform the review analysis.
4. Call `write_file` to write the review artifact to the repo.

No external process is involved. The bot is the executor.

## Why Skills Are Not Executed as Subprocesses

Claude Code plugins use a `plugin.json` manifest as their entrypoint. This is a JSON file — not an executable. Attempting to run it as a subprocess would always fail with a "permission denied" or "exec format error".

Claude Code plugins are designed to be consumed by an AI agent, not a shell. Their format assumes the caller is an LLM that will read the Markdown and act on it. The boabot `read_skill` tool implements exactly this contract: the bot reads the Markdown and follows the instructions using its built-in tools.

This design means boabot is fully compatible with any plugin designed for Claude Code, without requiring changes to the plugin format.

## Troubleshooting

### Skill not found

```
skill "review-code" not found in any active plugin
```

**Causes and fixes:**
- The plugin is not installed. Install it from the admin UI or with `boabotctl plugin install`.
- The plugin is installed but in `staged` status. Approve it from the admin UI (`POST /api/v1/plugins/{id}/approve`) or with `boabotctl plugin approve`.
- The plugin is installed but `disabled`. Re-enable it from the admin UI or with `boabotctl plugin enable`.
- The skill name does not match. Skill names must exactly match the `name` field in `plugin.yaml`'s `provides.tools` list. Check the plugin manifest with `boabotctl plugin get <name>`.

### `read_skill` tool not in ListTools

The `read_skill` tool only appears when a plugin store is configured. Plugin support requires the orchestrator config section with `plugins.install_dir` set:

```yaml
orchestrator:
  enabled: true
  plugins:
    install_dir: ./plugins
    registries:
      - url: https://raw.githubusercontent.com/stainedhead/shared-plugins/main
        trusted: true
```

If you are running a non-orchestrator bot and want it to use skills, the orchestrator bot's plugin store is shared with all bots in the team. Ensure the orchestrator bot is enabled in `team.yaml` and its config has `plugins.install_dir` set.

### Plugin active but skill Markdown not found

```
read_skill: read /path/to/plugins/dev-flow/commands/review-code.md: no such file or directory
```

The plugin is active in the store but the Markdown file is missing from the install directory. This can happen if the archive was corrupt or the install was interrupted.

**Fix:** Reload the plugin to re-extract it:
```bash
boabotctl plugin reload dev-flow
```
Or remove and reinstall:
```bash
boabotctl plugin remove dev-flow
boabotctl plugin install dev-flow
```
