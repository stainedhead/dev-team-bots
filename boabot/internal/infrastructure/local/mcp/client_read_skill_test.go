package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/mcp"
)

// makeActivePluginWithManifest builds an active plugin with a specific entrypoint.
func makeActivePluginWithManifest(name, entrypoint string, tools []domain.MCPTool) domain.Plugin {
	return domain.Plugin{
		ID:     name + "-id",
		Name:   name,
		Status: domain.PluginStatusActive,
		Manifest: domain.PluginManifest{
			Entrypoint: entrypoint,
			Provides:   domain.PluginProvides{Tools: tools},
		},
	}
}

// TestMCPClient_ReadSkill_ReturnsMarkdown verifies that read_skill returns the
// full content of commands/<name>.md from the installed plugin directory.
func TestMCPClient_ReadSkill_ReturnsMarkdown(t *testing.T) {
	t.Parallel()

	installDir := t.TempDir()
	// Create the commands file for the plugin.
	pluginDir := filepath.Join(installDir, "dev-flow")
	cmdDir := filepath.Join(pluginDir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	markdownContent := "# Review Code\n\nThis is the review-code skill.\n\nDo step 1, step 2, step 3.\n"
	if err := os.WriteFile(filepath.Join(cmdDir, "review-code.md"), []byte(markdownContent), 0o644); err != nil {
		t.Fatal(err)
	}

	pluginTool := domain.MCPTool{Name: "review-code", Description: "Review code skill"}
	plugin := makeActivePluginWithManifest("dev-flow", ".claude-plugin/plugin.json", []domain.MCPTool{pluginTool})

	store := &mocks.PluginStore{
		ListFn: func(_ context.Context) ([]domain.Plugin, error) {
			return []domain.Plugin{plugin}, nil
		},
	}

	client := mcp.NewClient([]string{installDir},
		mcp.WithPluginStore(store),
		mcp.WithInstallDir(installDir),
	)

	result, err := client.CallTool(context.Background(), "read_skill", map[string]any{
		"name": "review-code",
	})
	if err != nil {
		t.Fatalf("CallTool read_skill: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got error: %+v", result)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
	if result.Content[0].Text != markdownContent {
		t.Errorf("expected markdown content, got: %q", result.Content[0].Text)
	}
}

// TestMCPClient_ReadSkill_UnknownSkill verifies that read_skill returns a
// descriptive error when the skill name does not match any active plugin tool.
func TestMCPClient_ReadSkill_UnknownSkill(t *testing.T) {
	t.Parallel()

	installDir := t.TempDir()
	pluginTool := domain.MCPTool{Name: "review-code", Description: "Review code skill"}
	plugin := makeActivePluginWithManifest("dev-flow", "plugin.json", []domain.MCPTool{pluginTool})

	store := &mocks.PluginStore{
		ListFn: func(_ context.Context) ([]domain.Plugin, error) {
			return []domain.Plugin{plugin}, nil
		},
	}

	client := mcp.NewClient([]string{installDir},
		mcp.WithPluginStore(store),
		mcp.WithInstallDir(installDir),
	)

	result, err := client.CallTool(context.Background(), "read_skill", map[string]any{
		"name": "nonexistent",
	})
	if err != nil {
		t.Fatalf("CallTool read_skill: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for unknown skill")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
	errMsg := result.Content[0].Text
	if !strings.Contains(errMsg, "nonexistent") {
		t.Errorf("expected error message to mention skill name, got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "not found") {
		t.Errorf("expected error message to say 'not found', got: %q", errMsg)
	}
}

// TestMCPClient_ReadSkill_MissingNameArg verifies read_skill returns an error
// when the 'name' argument is missing.
func TestMCPClient_ReadSkill_MissingNameArg(t *testing.T) {
	t.Parallel()

	store := &mocks.PluginStore{
		ListFn: func(_ context.Context) ([]domain.Plugin, error) {
			return nil, nil
		},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithPluginStore(store),
		mcp.WithInstallDir(t.TempDir()),
	)

	result, err := client.CallTool(context.Background(), "read_skill", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool read_skill: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for missing name arg")
	}
	if !strings.Contains(result.Content[0].Text, "name") {
		t.Errorf("expected error to mention 'name', got: %q", result.Content[0].Text)
	}
}

// TestMCPClient_ReadSkill_AppearsInListTools verifies that read_skill appears
// in ListTools when a plugin store is configured.
func TestMCPClient_ReadSkill_AppearsInListTools(t *testing.T) {
	t.Parallel()

	store := &mocks.PluginStore{
		ListFn: func(_ context.Context) ([]domain.Plugin, error) {
			return nil, nil
		},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithPluginStore(store),
		mcp.WithInstallDir(t.TempDir()),
	)

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	found := false
	for _, tool := range tools {
		if tool.Name == "read_skill" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected read_skill in tools list when plugin store is configured")
	}
}

// TestMCPClient_ReadSkill_NotInListToolsWithoutStore verifies that read_skill
// does NOT appear in ListTools when no plugin store is configured.
func TestMCPClient_ReadSkill_NotInListToolsWithoutStore(t *testing.T) {
	t.Parallel()

	client := mcp.NewClient([]string{t.TempDir()})

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range tools {
		if tool.Name == "read_skill" {
			t.Error("read_skill should not appear in tools list without plugin store")
		}
	}
}

// TestMCPClient_CallPluginTool_JSONEntrypoint_ReadsSkill verifies that calling
// a plugin tool whose entrypoint is a plugin.json file delegates to readSkill
// rather than attempting exec.Command.
func TestMCPClient_CallPluginTool_JSONEntrypoint_ReadsSkill(t *testing.T) {
	t.Parallel()

	installDir := t.TempDir()
	pluginDir := filepath.Join(installDir, "dev-flow")
	cmdDir := filepath.Join(pluginDir, "commands")
	pluginJSONDir := filepath.Join(pluginDir, ".claude-plugin")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pluginJSONDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create the plugin.json entrypoint (non-executable).
	if err := os.WriteFile(filepath.Join(pluginJSONDir, "plugin.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	markdownContent := "# Create PRD\n\nSteps to create a PRD.\n"
	if err := os.WriteFile(filepath.Join(cmdDir, "create-prd.md"), []byte(markdownContent), 0o644); err != nil {
		t.Fatal(err)
	}

	pluginTool := domain.MCPTool{Name: "create-prd", Description: "Create a PRD skill"}
	plugin := makeActivePluginWithManifest("dev-flow", ".claude-plugin/plugin.json", []domain.MCPTool{pluginTool})

	store := &mocks.PluginStore{
		ListFn: func(_ context.Context) ([]domain.Plugin, error) {
			return []domain.Plugin{plugin}, nil
		},
	}

	client := mcp.NewClient([]string{installDir},
		mcp.WithPluginStore(store),
		mcp.WithInstallDir(installDir),
	)

	result, err := client.CallTool(context.Background(), "create-prd", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got error: %+v", result)
	}
	if len(result.Content) == 0 || result.Content[0].Text != markdownContent {
		t.Errorf("expected markdown content %q, got: %+v", markdownContent, result.Content)
	}
}

// TestMCPClient_ReadSkill_DisabledPlugin_NotFound verifies that read_skill
// returns "not found" when the matching tool belongs to a disabled plugin.
func TestMCPClient_ReadSkill_DisabledPlugin_NotFound(t *testing.T) {
	t.Parallel()

	pluginTool := domain.MCPTool{Name: "some-skill", Description: "skill"}
	disabledPlugin := domain.Plugin{
		ID:     "disabled-id",
		Name:   "disabled-plugin",
		Status: domain.PluginStatusDisabled,
		Manifest: domain.PluginManifest{
			Entrypoint: "plugin.json",
			Provides:   domain.PluginProvides{Tools: []domain.MCPTool{pluginTool}},
		},
	}

	store := &mocks.PluginStore{
		ListFn: func(_ context.Context) ([]domain.Plugin, error) {
			return []domain.Plugin{disabledPlugin}, nil
		},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithPluginStore(store),
		mcp.WithInstallDir(t.TempDir()),
	)

	result, err := client.CallTool(context.Background(), "read_skill", map[string]any{
		"name": "some-skill",
	})
	if err != nil {
		t.Fatalf("CallTool read_skill: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for skill in disabled plugin")
	}
	if !strings.Contains(result.Content[0].Text, "not found") {
		t.Errorf("expected 'not found' in error, got: %q", result.Content[0].Text)
	}
}
