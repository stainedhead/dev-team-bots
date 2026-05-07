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
func (c *Client) ListTools(_ context.Context) ([]domain.MCPTool, error) {
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
			Description: "Mark a Kanban board item as done. Only call this when you have independently finished all work on the item and it does not need team-lead review.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"item_id"},
				"properties": map[string]any{
					"item_id": map[string]any{"type": "string", "description": "The board item ID (visible in the item details)"},
				},
			},
		})
	}

	// Append active plugin tools, skipping collisions with builtin or earlier plugin tools.
	if c.pluginStore != nil {
		plugins, err := c.pluginStore.List(context.Background())
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
			// Found the plugin. Run the entrypoint.
			pluginDir := filepath.Join(c.installDir, p.Name)
			entrypoint := filepath.Join(pluginDir, p.Manifest.Entrypoint)
			if _, statErr := os.Stat(entrypoint); os.IsNotExist(statErr) {
				return errResult(fmt.Sprintf("plugin %q entrypoint not found: %s", p.Name, entrypoint)), true, nil
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
	if _, err := c.boardStore.Update(ctx, item); err != nil {
		return errResult(fmt.Sprintf("complete_board_item: update failed: %v", err)), nil
	}
	return okResult(fmt.Sprintf("board item %q marked as done", itemID)), nil
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

func okResult(text string) domain.MCPToolResult {
	return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: text}}}
}

func errResult(msg string) domain.MCPToolResult {
	return domain.MCPToolResult{
		IsError: true,
		Content: []domain.MCPContent{{Type: "text", Text: msg}},
	}
}
