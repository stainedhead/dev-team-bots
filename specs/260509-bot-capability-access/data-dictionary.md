# Data Dictionary: Bot Capability Access

## New Domain Types

### `CLIAgentConfig` (new, in `internal/domain/`)

Holds parameters for a single CLI agent invocation.

```go
// CLIAgentConfig holds parameters for a single CLI agent invocation.
type CLIAgentConfig struct {
    // Binary is the executable name (e.g. "claude") or absolute path.
    // The caller is responsible for resolving defaults before passing.
    Binary string

    // WorkDir is the subprocess working directory.
    WorkDir string

    // Model is the model ID to pass to the CLI (e.g. "claude-opus-4-7").
    // Empty string means omit the model flag entirely.
    Model string

    // Args contains dialect-specific arguments prepended before the instruction.
    // For Claude Code: ["--output-format=stream-json", "--dangerously-skip-permissions", "-p"]
    // For Codex:       ["-q", "--approval-mode=full-auto"]
    // The CLIAgentRunner appends the instruction after Args.
    Args []string

    // Timeout limits total subprocess execution time.
    // Zero means use the implementation default (30 minutes).
    Timeout time.Duration
}
```

---

### `CLIAgentRunner` (new interface, in `internal/domain/`)

Executes a long-running CLI coding agent as a supervised subprocess.

```go
// CLIAgentRunner executes a long-running CLI coding agent as a supervised subprocess.
type CLIAgentRunner interface {
    // Run spawns the binary with cfg.Args + instruction, streams output line by
    // line (calling progress for each non-empty line), optionally forwards stdin
    // from the provided channel, and returns the complete accumulated output.
    //
    // stdin may be nil if no interactive input is expected. The channel must be
    // closed by the caller when no more input will be sent.
    //
    // Run blocks until the subprocess exits or ctx is cancelled.
    // On context cancellation or timeout: SIGTERM is sent first; if the process
    // does not exit within 5 seconds, SIGKILL is sent.
    //
    // Returns the accumulated stdout as the result string, or an error if the
    // binary is not found, the process fails to start, or the context is cancelled.
    Run(ctx context.Context, cfg CLIAgentConfig, instruction string,
        stdin <-chan string, progress func(line string)) (string, error)
}
```

---

### `CLIToolConfig` (new, in `internal/infrastructure/config/`)

Per-CLI-tool configuration block.

```go
// CLIToolConfig configures a single CLI tool integration.
type CLIToolConfig struct {
    // Enabled controls whether the tool is advertised in ListTools.
    // Default: false (opt-in required).
    Enabled bool `yaml:"enabled"`

    // BinaryPath is the executable name or absolute path.
    // Empty string uses the tool's canonical default name.
    BinaryPath string `yaml:"binary_path"`
}
```

---

### `CLIToolsConfig` (new, in `internal/infrastructure/config/`)

Orchestrator-level CLI tools configuration. Added as a field on `OrchestratorConfig`.

```go
// CLIToolsConfig groups configuration for all supported CLI tool integrations.
type CLIToolsConfig struct {
    ClaudeCode  CLIToolConfig `yaml:"claude_code"`
    Codex       CLIToolConfig `yaml:"codex"`
    OpenAICodex CLIToolConfig `yaml:"openai_codex"`
    OpenCode    CLIToolConfig `yaml:"opencode"`
}
```

**Binary defaults (applied at ListTools call time, not in config):**

| Field        | Default binary name |
|--------------|---------------------|
| `ClaudeCode` | `"claude"`          |
| `Codex`      | `"codex"`           |
| `OpenAICodex`| `"openai-codex"`    |
| `OpenCode`   | `"opencode"`        |

---

## Changes to Existing Config Types

### `OrchestratorConfig` (modified, in `internal/infrastructure/config/`)

Add `CLITools` field:

```go
type OrchestratorConfig struct {
    Enabled       bool          `yaml:"enabled"`
    APIPort       int           `yaml:"api_port"`
    JWTSecret     string        `yaml:"jwt_secret"`
    AdminPassword string        `yaml:"admin_password"`
    WorkDirs      []string      `yaml:"work_dirs"`
    RetentionDays int           `yaml:"retention_days"`
    MaxConcurrent int           `yaml:"max_concurrent"`
    Plugins       PluginsConfig `yaml:"plugins"`
    CLITools      CLIToolsConfig `yaml:"cli_tools"` // NEW
}
```

**Zero-value behaviour:** All CLI tools are disabled and binary paths are empty strings when `cli_tools` is omitted from YAML. `dec.KnownFields(true)` requires the field to exist in the struct; it does not require it to be present in the YAML file.

---

## No Changes to Domain Types

The following existing domain types are **not modified**:

- `domain.Plugin` — unchanged
- `domain.PluginManifest` — unchanged
- `domain.PluginProvides` — unchanged (plugin tools remain `[]MCPTool`)
- `domain.PluginStore` interface — unchanged
- `domain.MCPClient` interface — unchanged
- `domain.MCPTool` — unchanged
- `domain.MCPToolResult` — unchanged
- `domain.ModelProvider` — unchanged
- `codeagent.Provider` — unchanged (model replacement path preserved)

---

## New Infrastructure Types

### `SubprocessRunner` (new, in `internal/infrastructure/cliagent/`)

Concrete implementation of `domain.CLIAgentRunner` using `os/exec`.

```go
// SubprocessRunner implements domain.CLIAgentRunner by spawning subprocesses.
type SubprocessRunner struct{}

// New returns a SubprocessRunner. No configuration required at construction;
// all parameters are passed per-invocation via CLIAgentConfig.
func New() *SubprocessRunner
```

Internal to `cliagent` package:
- Uses `exec.CommandContext` with `cmd.Cancel` set to send `syscall.SIGTERM`.
- Sets `cmd.WaitDelay = 5 * time.Second`.
- Reads stdout with `bufio.Scanner`.
- Writes stdin from the provided channel in a separate goroutine.
- Uses `exec.LookPath(cfg.Binary)` before attempting to start.

---

## Extracted/Shared Functions

### `ParseStreamLine` (new exported function in `internal/infrastructure/codeagent/stream.go`)

```go
// ParseStreamLine parses one JSON line from Claude Code's --output-format=stream-json
// output and returns any text it contains. Returns ("", true) for non-text events
// and ("", false) if the line cannot be parsed.
func ParseStreamLine(line string) (text string, ok bool)
```

This is a rename+export of the existing unexported `extractText` function in `provider.go`. The `streamEvent` and `deltaField` types move to `stream.go` as well (or remain in `provider.go` with `stream.go` importing them — keep in same package). The `codeagent.Provider.Invoke` method continues to call `ParseStreamLine` instead of `extractText`.

---

## MCP Client Struct Changes

### `Client` (modified, in `internal/infrastructure/local/mcp/`)

Additional fields:

```go
type Client struct {
    allowedDirs []string
    boardStore  domain.BoardStore
    pluginStore domain.PluginStore
    installDir  string
    cliRunner   domain.CLIAgentRunner // NEW: nil if no CLI tools configured
    cliTools    config.CLIToolsConfig // NEW: zero value = all disabled
    progressFn  func(line string)     // NEW: optional; called for each CLI output line
}
```

New constructor options:

```go
// WithCLIRunner injects the CLI agent runner for CLI tool dispatching.
func WithCLIRunner(r domain.CLIAgentRunner) func(*Client)

// WithCLITools sets the CLI tools configuration used to gate tool availability.
func WithCLITools(cfg config.CLIToolsConfig) func(*Client)

// WithProgressFn sets the progress callback invoked for each output line from CLI tools.
// If nil, progress lines are silently discarded.
func WithProgressFn(fn func(line string)) func(*Client)
```
