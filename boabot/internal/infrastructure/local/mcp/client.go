// Package mcp provides a local filesystem MCP client for use by bot workers.
// It exposes file and shell tools scoped to a set of allowed directories.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/codeagent"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
)

const (
	shellTimeout  = 5 * time.Minute
	pluginTimeout = 30 * time.Second
)

// Client is a local filesystem MCP client that enforces path restrictions.
type Client struct {
	allowedDirs []string // absolute, cleaned paths
	boardStore  domain.BoardStore
	pluginStore domain.PluginStore
	installDir  string
	// CLI tool support
	cliRunner domain.CLIAgentRunner
	cliTools  config.CLIToolsConfig
	// progressFn is read without synchronisation; this is safe because bots process
	// tasks sequentially — only one tool call is active at a time per Client instance.
	progressFn func(line string) // optional; called for each output line from CLI tools
}

// WithBoardStore adds the complete_board_item tool to the client, allowing
// standalone bots to mark a Kanban item done when they finish independently.
func WithBoardStore(bs domain.BoardStore) func(*Client) {
	return func(c *Client) { c.boardStore = bs }
}

// WithPluginStore adds plugin tool support to the client.
func WithPluginStore(ps domain.PluginStore) func(*Client) {
	return func(c *Client) { c.pluginStore = ps }
}

// WithInstallDir sets the plugin install directory used for entrypoint resolution.
func WithInstallDir(dir string) func(*Client) {
	return func(c *Client) { c.installDir = dir }
}

// WithCLIRunner sets the CLIAgentRunner used to spawn CLI tool subprocesses.
func WithCLIRunner(r domain.CLIAgentRunner) func(*Client) {
	return func(c *Client) { c.cliRunner = r }
}

// WithCLITools configures which CLI agent tools are enabled and where their
// binaries are located.
func WithCLITools(ct config.CLIToolsConfig) func(*Client) {
	return func(c *Client) { c.cliTools = ct }
}

// WithProgressFn sets an optional function called for each output line from
// CLI tool subprocesses. Use this to surface real-time progress in the operator UI.
func WithProgressFn(fn func(line string)) func(*Client) {
	return func(c *Client) { c.progressFn = fn }
}

// NewClient creates a Client restricted to the given directories.
// Paths are cleaned and made absolute before use.
func NewClient(allowedDirs []string, opts ...func(*Client)) *Client {
	clean := make([]string, 0, len(allowedDirs))
	for _, d := range allowedDirs {
		abs, err := filepath.Abs(d)
		if err == nil {
			clean = append(clean, filepath.Clean(abs))
		}
	}
	c := &Client{allowedDirs: clean}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ListTools returns the set of built-in filesystem tools.
func (c *Client) ListTools(ctx context.Context) ([]domain.MCPTool, error) {
	tools := []domain.MCPTool{
		{
			Name:        "read_file",
			Description: "Read the contents of a file. Returns the file content as a string.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"path"},
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Absolute path to the file"},
				},
			},
		},
		{
			Name:        "write_file",
			Description: "Write content to a file, creating parent directories as needed. Overwrites existing files.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"path", "content"},
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Absolute path to write"},
					"content": map[string]any{"type": "string", "description": "Content to write"},
				},
			},
		},
		{
			Name:        "create_dir",
			Description: "Create a directory (and all parent directories) at the given path.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"path"},
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Absolute path of the directory to create"},
				},
			},
		},
		{
			Name:        "list_dir",
			Description: "List the contents of a directory. Returns a JSON array of {name, is_dir} objects.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"path"},
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Absolute path of the directory to list"},
				},
			},
		},
		{
			Name:        "run_shell",
			Description: "Run a shell command in a working directory. Returns combined stdout and stderr. Non-zero exit is reported as an error result.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"command", "working_dir"},
				"properties": map[string]any{
					"command":     map[string]any{"type": "string", "description": "Shell command to execute"},
					"working_dir": map[string]any{"type": "string", "description": "Absolute path of the working directory"},
				},
			},
		},
	}
	if c.boardStore != nil {
		tools = append(tools, domain.MCPTool{
			Name:        "complete_board_item",
			Description: "Mark a Kanban board item as done. Only call this when you have independently finished all work on the item and it does not need team-lead review. Always provide a summary of what you did and what the outcome was.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"item_id", "output"},
				"properties": map[string]any{
					"item_id": map[string]any{"type": "string", "description": "The board item ID (visible in the item details)"},
					"output":  map[string]any{"type": "string", "description": "Summary of what you did and the outcome. Include any errors, blockers, or results. This is shown to the operator in the Output tab."},
				},
			},
		})
	}

	if c.pluginStore != nil {
		tools = append(tools, domain.MCPTool{
			Name: "read_skill",
			Description: "Read the Markdown instruction file for an installed plugin skill. Returns the full content of " +
				"commands/<name>.md from the plugin's install directory. After reading, carry out the described steps " +
				"yourself using your built-in tools (run_shell, read_file, write_file, etc.) — do not look for an " +
				"external executor. Returns an error string if the skill is not found or its plugin is not active.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"name"},
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "The skill name (e.g. \"review-code\", \"create-prd\"). Must match a tool name listed in an active plugin's manifest.",
					},
				},
			},
		})
	}

	// Append active plugin tools, skipping collisions with builtin or earlier plugin tools.
	if c.pluginStore != nil {
		plugins, err := c.pluginStore.List(ctx)
		if err == nil {
			seen := make(map[string]string) // tool name → claimant (builtin or plugin name)
			for _, t := range tools {
				seen[t.Name] = "builtin"
			}
			for _, p := range plugins {
				if p.Status != domain.PluginStatusActive {
					continue
				}
				for _, t := range p.Manifest.Provides.Tools {
					if existing, conflict := seen[t.Name]; conflict {
						slog.Warn("plugin tool name collision, skipping",
							"tool", t.Name,
							"plugin", p.Name,
							"claimed_by", existing)
						continue
					}
					seen[t.Name] = p.Name
					tools = append(tools, t)
				}
			}
		}
	}

	// Append enabled CLI tools (when runner is configured and binary resolves).
	if c.cliRunner != nil {
		if _, ok := resolveBinary(c.cliTools.ClaudeCode, "claude"); ok {
			tools = append(tools, domain.MCPTool{
				Name: "run_claude_code",
				Description: "Run a task using the Claude Code CLI agent (`claude`). Claude Code has full autonomous " +
					"access to the filesystem and shell in the given work directory — it can read/write files, run " +
					"commands, use git, and complete multi-step coding tasks. Use for complex implementation, " +
					"refactoring, spec-driven development, or anything benefiting from Claude's full agentic loop. " +
					"Supports --model to select cost/quality tradeoff.",
				InputSchema: cliToolSchema(),
			})
		}
		if _, ok := resolveBinary(c.cliTools.Codex, "codex"); ok {
			tools = append(tools, domain.MCPTool{
				Name: "run_codex",
				Description: "Run a task using the OpenAI Codex CLI agent (`codex`). Codex has filesystem and shell " +
					"access in the given work directory and runs fully automatically in quiet mode with full-auto " +
					"approval. Best for implementation tasks using OpenAI models. Supports model selection via " +
					"the --model flag.",
				InputSchema: cliToolSchema(),
			})
		}
		if _, ok := resolveBinary(c.cliTools.OpenAICodex, "openai-codex"); ok {
			tools = append(tools, domain.MCPTool{
				Name: "run_openai_codex",
				Description: "Run a task using the OpenAI Codex open-source CLI agent (`openai-codex`). " +
					"Provides autonomous filesystem and shell access in the given work directory. Runs in " +
					"full-auto mode (--full-auto) to avoid interactive prompts. Best for implementation tasks " +
					"using OpenAI models. Supports model selection via the --model flag.",
				InputSchema: cliToolSchema(),
			})
		}
		if _, ok := resolveBinary(c.cliTools.OpenCode, "opencode"); ok {
			tools = append(tools, domain.MCPTool{
				Name: "run_opencode",
				Description: "Run a task using the OpenCode CLI agent (`opencode`). OpenCode provides autonomous " +
					"agentic coding in the given work directory with AI-powered file editing, shell commands, and " +
					"multi-step task execution. Runs in non-interactive mode (-q). Supports model selection via " +
					"the --model flag for cost/quality tradeoff.",
				InputSchema: cliToolSchema(),
			})
		}
	}

	return tools, nil
}

// CallTool dispatches the named tool with the provided arguments.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (domain.MCPToolResult, error) {
	// Check if the tool belongs to an active plugin before falling through to builtins.
	if c.pluginStore != nil {
		if result, handled, err := c.callPluginTool(ctx, name, args); handled {
			return result, err
		}
	}

	switch name {
	case "read_file":
		return c.readFile(args)
	case "write_file":
		return c.writeFile(args)
	case "create_dir":
		return c.createDir(args)
	case "list_dir":
		return c.listDir(args)
	case "run_shell":
		return c.runShell(args)
	case "complete_board_item":
		return c.completeBoardItem(ctx, args)
	case "read_skill":
		return c.readSkill(ctx, args)
	case "run_claude_code":
		return c.callCLITool(ctx, "claude_code", args)
	case "run_codex":
		return c.callCLITool(ctx, "codex", args)
	case "run_openai_codex":
		return c.callCLITool(ctx, "openai_codex", args)
	case "run_opencode":
		return c.callCLITool(ctx, "opencode", args)
	default:
		return errResult(fmt.Sprintf("unknown tool: %s", name)), nil
	}
}

// callPluginTool checks if the named tool is provided by an active plugin, and
// if so, dispatches to the plugin entrypoint subprocess.
// Returns (result, true, nil/err) if the tool is handled by a plugin,
// or (zero, false, nil) if the tool is not a plugin tool.
func (c *Client) callPluginTool(ctx context.Context, name string, args map[string]any) (domain.MCPToolResult, bool, error) {
	plugins, err := c.pluginStore.List(ctx)
	if err != nil {
		return domain.MCPToolResult{}, false, nil
	}

	for _, p := range plugins {
		if p.Status != domain.PluginStatusActive {
			continue
		}
		for _, t := range p.Manifest.Provides.Tools {
			if t.Name != name {
				continue
			}
			// Check if the entrypoint is a non-executable plugin.json (Claude Code plugin).
			// If so, delegate to readSkill which reads the Markdown instructions instead.
			if isPluginJSONEntrypoint(p.Manifest.Entrypoint) {
				result, readErr := c.readSkill(ctx, map[string]any{"name": name})
				return result, true, readErr
			}

			// Found the plugin. Run the entrypoint.
			pluginDir := filepath.Join(c.installDir, p.Name)
			entrypoint := filepath.Join(pluginDir, p.Manifest.Entrypoint)
			info, statErr := os.Stat(entrypoint)
			if os.IsNotExist(statErr) {
				return errResult(fmt.Sprintf("plugin %q entrypoint not found: %s", p.Name, entrypoint)), true, nil
			}
			if statErr != nil {
				return errResult(fmt.Sprintf("plugin %q stat entrypoint: %v", p.Name, statErr)), true, nil
			}
			if info.Mode()&0o100 == 0 {
				return errResult(fmt.Sprintf("plugin %q entrypoint is not executable: %s", p.Name, entrypoint)), true, nil
			}

			argsJSON, marshalErr := json.Marshal(args)
			if marshalErr != nil {
				return errResult(fmt.Sprintf("plugin %q: marshal args: %v", p.Name, marshalErr)), true, nil
			}

			callCtx, cancel := context.WithTimeout(ctx, pluginTimeout)
			defer cancel()

			cmd := exec.CommandContext(callCtx, entrypoint)
			cmd.Stdin = bytes.NewReader(argsJSON)

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			runErr := cmd.Run()
			if runErr != nil {
				errMsg := fmt.Sprintf("plugin %q exited with error: %v\nstderr: %s", p.Name, runErr, stderr.String())
				return errResult(errMsg), true, nil
			}

			var result domain.MCPToolResult
			if decodeErr := json.NewDecoder(&stdout).Decode(&result); decodeErr != nil {
				return errResult(fmt.Sprintf("plugin %q: decode output: %v\nstdout: %s", p.Name, decodeErr, stdout.String())), true, nil
			}
			return result, true, nil
		}
	}
	return domain.MCPToolResult{}, false, nil
}

// --- tool implementations ---

// readSkill looks up an active plugin skill by name and returns the full
// content of its commands/<name>.md file.
func (c *Client) readSkill(ctx context.Context, args map[string]any) (domain.MCPToolResult, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return errResult("read_skill: missing required argument \"name\""), nil
	}
	if c.pluginStore == nil {
		return errResult("read_skill: plugin store not available"), nil
	}
	plugins, err := c.pluginStore.List(ctx)
	if err != nil {
		return errResult(fmt.Sprintf("read_skill: list plugins: %v", err)), nil
	}
	for _, p := range plugins {
		if p.Status != domain.PluginStatusActive {
			continue
		}
		for _, t := range p.Manifest.Provides.Tools {
			if t.Name != name {
				continue
			}
			mdPath := filepath.Join(c.installDir, p.Name, "commands", name+".md")
			data, readErr := os.ReadFile(mdPath)
			if readErr != nil {
				return errResult(fmt.Sprintf("read_skill: read %s: %v", mdPath, readErr)), nil
			}
			return okResult(string(data)), nil
		}
	}
	return errResult(fmt.Sprintf("skill %q not found in any active plugin", name)), nil
}

// isPluginJSONEntrypoint returns true when the entrypoint base name is exactly
// "plugin.json". This is an exact match on the base name only — filenames such
// as "myplugin.json" or paths ending in "/someplugin.json" do not match.
// Matching "plugin.json" identifies Claude Code plugin manifests, which are
// non-executable and are handled by delegating to readSkill instead of exec.
func isPluginJSONEntrypoint(entrypoint string) bool {
	return filepath.Base(entrypoint) == "plugin.json"
}

func (c *Client) readFile(args map[string]any) (domain.MCPToolResult, error) {
	path, err := c.resolvePath(args, "path")
	if err != nil {
		return errResult(err.Error()), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return errResult(fmt.Sprintf("read_file: %v", err)), nil
	}
	return okResult(string(data)), nil
}

func (c *Client) writeFile(args map[string]any) (domain.MCPToolResult, error) {
	path, err := c.resolvePath(args, "path")
	if err != nil {
		return errResult(err.Error()), nil
	}
	content, _ := args["content"].(string)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errResult(fmt.Sprintf("write_file mkdir: %v", err)), nil
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return errResult(fmt.Sprintf("write_file: %v", err)), nil
	}
	return okResult("ok"), nil
}

func (c *Client) createDir(args map[string]any) (domain.MCPToolResult, error) {
	path, err := c.resolvePath(args, "path")
	if err != nil {
		return errResult(err.Error()), nil
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return errResult(fmt.Sprintf("create_dir: %v", err)), nil
	}
	return okResult("ok"), nil
}

func (c *Client) listDir(args map[string]any) (domain.MCPToolResult, error) {
	path, err := c.resolvePath(args, "path")
	if err != nil {
		return errResult(err.Error()), nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return errResult(fmt.Sprintf("list_dir: %v", err)), nil
	}
	type entry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
	}
	list := make([]entry, 0, len(entries))
	for _, e := range entries {
		list = append(list, entry{Name: e.Name(), IsDir: e.IsDir()})
	}
	data, _ := json.Marshal(list)
	return okResult(string(data)), nil
}

func (c *Client) runShell(args map[string]any) (domain.MCPToolResult, error) {
	workDir, err := c.resolvePath(args, "working_dir")
	if err != nil {
		return errResult(err.Error()), nil
	}
	command, _ := args["command"].(string)

	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = workDir

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()
	output := buf.String()
	if runErr != nil {
		return domain.MCPToolResult{
			IsError: true,
			Content: []domain.MCPContent{{Type: "text", Text: fmt.Sprintf("exit error: %v\n%s", runErr, output)}},
		}, nil
	}
	return okResult(output), nil
}

func (c *Client) completeBoardItem(ctx context.Context, args map[string]any) (domain.MCPToolResult, error) {
	if c.boardStore == nil {
		return errResult("complete_board_item: board store not available"), nil
	}
	itemID, _ := args["item_id"].(string)
	if itemID == "" {
		return errResult("complete_board_item: missing required argument \"item_id\""), nil
	}
	item, err := c.boardStore.Get(ctx, itemID)
	if err != nil {
		return errResult(fmt.Sprintf("complete_board_item: item not found: %v", err)), nil
	}
	item.Status = domain.WorkItemStatusDone
	if output, _ := args["output"].(string); output != "" {
		now := time.Now().UTC()
		item.LastResult = output
		item.LastResultAt = &now
	}
	if _, err := c.boardStore.Update(ctx, item); err != nil {
		return errResult(fmt.Sprintf("complete_board_item: update failed: %v", err)), nil
	}
	return okResult(fmt.Sprintf("board item %q marked as done", itemID)), nil
}

// callCLITool dispatches a run_<tool> call by resolving the binary and invoking
// the CLIAgentRunner. toolID matches the CLIToolsConfig field names:
// "claude_code", "codex", "openai_codex", "opencode".
func (c *Client) callCLITool(ctx context.Context, toolID string, args map[string]any) (domain.MCPToolResult, error) {
	if c.cliRunner == nil {
		return errResult("CLI runner not configured"), nil
	}

	instruction, _ := args["instruction"].(string)
	if instruction == "" {
		return errResult("callCLITool: missing required argument \"instruction\""), nil
	}
	workDir, err := c.resolvePath(args, "work_dir")
	if err != nil {
		return errResult(err.Error()), nil
	}
	model, _ := args["model"].(string)

	var toolCfg config.CLIToolConfig
	var defaultBinary string
	var cliArgs []string
	var isStreamJSON bool

	switch toolID {
	case "claude_code":
		toolCfg = c.cliTools.ClaudeCode
		defaultBinary = "claude"
		cliArgs = []string{"--output-format=stream-json", "--dangerously-skip-permissions"}
		if model != "" {
			cliArgs = append(cliArgs, "--model", model)
		}
		cliArgs = append(cliArgs, "-p")
		isStreamJSON = true
	case "codex":
		toolCfg = c.cliTools.Codex
		defaultBinary = "codex"
		cliArgs = []string{"-q", "--approval-mode=full-auto"}
		if model != "" {
			cliArgs = append(cliArgs, "--model", model)
		}
	case "openai_codex":
		toolCfg = c.cliTools.OpenAICodex
		defaultBinary = "openai-codex"
		cliArgs = []string{"--full-auto"}
		if model != "" {
			cliArgs = append(cliArgs, "--model", model)
		}
	case "opencode":
		toolCfg = c.cliTools.OpenCode
		defaultBinary = "opencode"
		cliArgs = []string{"-q"}
		if model != "" {
			cliArgs = append(cliArgs, "--model", model)
		}
	default:
		return errResult(fmt.Sprintf("unknown CLI tool ID: %s", toolID)), nil
	}

	bin, ok := resolveBinary(toolCfg, defaultBinary)
	if !ok {
		return errResult(fmt.Sprintf("CLI tool %q is not enabled or binary not found", toolID)), nil
	}

	cfg := domain.CLIAgentConfig{
		Binary:  bin,
		WorkDir: workDir,
		Args:    cliArgs,
	}

	output, runErr := c.cliRunner.Run(ctx, cfg, instruction, nil, c.progressFn)
	if runErr != nil {
		return errResult(fmt.Sprintf("CLI tool %q error: %v", toolID, runErr)), nil
	}

	// For Claude Code stream-json output, post-process to extract text events.
	if isStreamJSON {
		var extracted strings.Builder
		for _, line := range strings.Split(output, "\n") {
			if line == "" {
				continue
			}
			text, ok := codeagent.ParseStreamLine(line)
			if ok && text != "" {
				extracted.WriteString(text)
			}
		}
		if extracted.Len() > 0 {
			return okResult(extracted.String()), nil
		}
	}

	return okResult(output), nil
}

// resolveBinary checks if a CLI tool is enabled and its binary is available.
// Returns the resolved binary path and true on success, or ("", false) if the
// tool is disabled or the binary cannot be found. This is a normal condition and
// must not be logged as an error.
func resolveBinary(cfg config.CLIToolConfig, defaultName string) (string, bool) {
	if !cfg.Enabled {
		return "", false
	}
	bin := cfg.BinaryPath
	if bin == "" {
		bin = defaultName
	}
	if filepath.IsAbs(bin) {
		info, err := os.Stat(bin)
		if err != nil {
			return "", false
		}
		// Verify the executable bit (owner execute) mirrors what exec.LookPath checks
		// for relative binary names. A non-executable binary resolves as absent so that
		// ListTools does not advertise a tool that will fail at subprocess launch.
		if info.Mode()&0o100 == 0 {
			return "", false
		}
		return bin, true
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return "", false
	}
	return resolved, true
}

// cliToolSchema returns the shared JSON schema for CLI agent tool inputs.
func cliToolSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"instruction", "work_dir"},
		"properties": map[string]any{
			"instruction": map[string]any{
				"type":        "string",
				"description": "The task or instruction to pass to the CLI agent.",
			},
			"work_dir": map[string]any{
				"type":        "string",
				"description": "Absolute path of the working directory for the CLI agent subprocess.",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional model name to use. Omit to use the CLI agent's default.",
			},
		},
	}
}

// --- helpers ---

// resolvePath extracts the named string arg, cleans it, and enforces allowedDirs.
func (c *Client) resolvePath(args map[string]any, key string) (string, error) {
	raw, _ := args[key].(string)
	if raw == "" {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("invalid path %q: %v", raw, err)
	}
	clean := filepath.Clean(abs)
	if !c.isAllowed(clean) {
		return "", fmt.Errorf("path %q is outside the allowed directories", clean)
	}
	return clean, nil
}

// isAllowed returns true when path is inside at least one of the allowed dirs.
func (c *Client) isAllowed(path string) bool {
	for _, dir := range c.allowedDirs {
		if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// AllowDir temporarily adds dir to the client's allowed directories and returns
// a cleanup function that removes it. Callers must invoke the returned function
// (typically via defer) when the temporary permission is no longer needed.
// Safe to call because bots process tasks sequentially.
func (c *Client) AllowDir(path string) func() {
	abs, err := filepath.Abs(path)
	if err != nil {
		return func() {}
	}
	clean := filepath.Clean(abs)
	c.allowedDirs = append(c.allowedDirs, clean)
	return func() {
		updated := make([]string, 0, len(c.allowedDirs))
		for _, d := range c.allowedDirs {
			if d != clean {
				updated = append(updated, d)
			}
		}
		c.allowedDirs = updated
	}
}

func okResult(text string) domain.MCPToolResult {
	return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: text}}}
}

func errResult(msg string) domain.MCPToolResult {
	return domain.MCPToolResult{
		IsError: true,
		Content: []domain.MCPContent{{Type: "text", Text: msg}},
	}
}
