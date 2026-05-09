# Data Dictionary: Bot Capability Access — Review Fixes

## Overview

No new domain types or value objects are introduced by these review fixes. All changes are either:
- Function signature adjustments (passing `ctx` through, returning an error from `resolvePath`)
- Logic additions within existing functions (mode bit checks)
- Test code only
- Comment/documentation updates

---

## Affected Type Usages

### `domain.CLIAgentConfig` (`boabot/internal/domain/`)

No field changes. The existing `WorkDir string` field is populated correctly after the fix — its value will be a cleaned, `allowedDirs`-validated absolute path rather than a raw user-supplied string.

### `domain.MCPToolResult` (`boabot/internal/domain/mcp.go`)

No changes. The existing `IsError bool` and `Content []MCPContent` fields are used as-is to return error results.

### `domain.PluginStore` interface (`boabot/internal/domain/`)

No interface change. The `List(ctx context.Context)` signature already exists; only the caller in `ListTools` needs to pass the correct context.

### `domain.MCPClient` interface (`boabot/internal/domain/mcp.go`)

The interface declaration must declare `ListTools(ctx context.Context) ([]MCPTool, error)`. If the current declaration uses `_`, it must be updated to a named parameter. Verify before implementing FR-003.

### `os.FileInfo` (stdlib)

Used in two places after the fix:
- `callPluginTool`: `info, err := os.Stat(entrypoint)` — then check `info.Mode()&0o100`.
- `resolveBinary`: `info, err := os.Stat(bin)` — then check `info.Mode()&0o100`.

Mode bit `0o100` is the owner-execute bit on Unix (`S_IXUSR`). This is consistent with how `exec.LookPath` validates executability on Unix systems.

---

## No Schema Changes

The MCP tool input schemas (returned by `cliToolSchema()`) are unchanged. `work_dir` is already declared as `"required"` in the schema — the fix enforces what the schema already states.
