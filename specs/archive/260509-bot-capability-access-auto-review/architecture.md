# Architecture: Bot Capability Access — Review Fixes

## Overview

No new components are introduced. These fixes are contained within existing files and layers. The clean architecture boundaries are unchanged.

---

## Files Changed

| File | Layer | Change |
|------|-------|--------|
| `boabot/internal/infrastructure/local/mcp/client.go` | Infrastructure | Production: `callCLITool` path validation, `ListTools` context, `callPluginTool` exec check, `resolveBinary` exec check, comments |
| `boabot/internal/infrastructure/cliagent/runner.go` | Infrastructure | Comment only (drainStdin already correct) |
| `boabot/internal/application/team/plugin_race_test.go` | Application (test) | Update race test to read `resolvedPluginStore` from goroutines |
| `boabot/internal/application/team/export_test.go` | Application (test) | Add `ResolvedPluginStore()` accessor if needed |
| `boabot/internal/infrastructure/local/mcp/client_cli_tool_test.go` | Infrastructure (test) | New tests: out-of-scope work_dir, context cancellation |
| `boabot/internal/infrastructure/local/mcp/client_plugin_test.go` | Infrastructure (test) | New test: non-executable entrypoint |
| `boabot/internal/infrastructure/cliagent/runner_test.go` | Infrastructure (test) | New tests: stdin-cancel, non-zero exit stderr |
| `boabot/docs/product-details.md` | Documentation | Fix `run_openai_codex` entry |

---

## No New Components

- No new interfaces.
- No new packages.
- No new infrastructure adapters.
- No changes to the domain layer.
- No changes to the dependency graph.

---

## Security Note

The FR-001 fix (REQ-001) is the only security-relevant change. It brings `callCLITool` into line with all other path-accepting tools, which already enforce `allowedDirs`. The sandboxing model is unchanged — the fix closes a gap in its consistent application.

The `resolvePath` function already exists and is already battle-tested by the tests for `read_file`, `write_file`, `create_dir`, `list_dir`, and `run_shell`. Reusing it for `callCLITool` ensures the same semantics apply.
