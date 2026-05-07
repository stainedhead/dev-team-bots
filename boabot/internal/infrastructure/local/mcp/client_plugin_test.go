package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/mcp"
)

func makeActivePlugin(name string, tools []domain.MCPTool) domain.Plugin {
	return domain.Plugin{
		ID:     name + "-id",
		Name:   name,
		Status: domain.PluginStatusActive,
		Manifest: domain.PluginManifest{
			Provides: domain.PluginProvides{Tools: tools},
		},
	}
}

func TestMCPClient_ListTools_IncludesPluginTools(t *testing.T) {
	pluginTool := domain.MCPTool{Name: "my_plugin_tool", Description: "does stuff"}
	activePlugin := makeActivePlugin("my-plugin", []domain.MCPTool{pluginTool})

	store := &mocks.PluginStore{
		ListFn: func(_ context.Context) ([]domain.Plugin, error) {
			return []domain.Plugin{activePlugin}, nil
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
		if tool.Name == "my_plugin_tool" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected my_plugin_tool in tools list, got: %+v", tools)
	}
}

func TestMCPClient_ListTools_DisabledPlugin_Excluded(t *testing.T) {
	pluginTool := domain.MCPTool{Name: "disabled_tool", Description: "should not appear"}
	disabledPlugin := domain.Plugin{
		ID:     "disabled-id",
		Name:   "disabled-plugin",
		Status: domain.PluginStatusDisabled,
		Manifest: domain.PluginManifest{
			Provides: domain.PluginProvides{Tools: []domain.MCPTool{pluginTool}},
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

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range tools {
		if tool.Name == "disabled_tool" {
			t.Error("disabled_tool should not appear in tools list")
		}
	}
}

func TestMCPClient_ListTools_Collision_SecondExcluded(t *testing.T) {
	tool := domain.MCPTool{Name: "shared_tool", Description: "first plugin's tool"}
	plugin1 := makeActivePlugin("plugin-one", []domain.MCPTool{tool})
	plugin2 := makeActivePlugin("plugin-two", []domain.MCPTool{{Name: "shared_tool", Description: "second plugin - collision"}})

	store := &mocks.PluginStore{
		ListFn: func(_ context.Context) ([]domain.Plugin, error) {
			return []domain.Plugin{plugin1, plugin2}, nil
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

	count := 0
	for _, t2 := range tools {
		if t2.Name == "shared_tool" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 shared_tool, got %d", count)
	}
}

func TestMCPClient_CallTool_DispatchesToPluginEntrypoint(t *testing.T) {
	installDir := t.TempDir()

	// Create a simple shell script as the plugin entrypoint.
	pluginDir := filepath.Join(installDir, "my-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entrypointScript := filepath.Join(pluginDir, "run.sh")
	// Script reads JSON from stdin, returns JSON to stdout.
	scriptContent := `#!/bin/sh
# Read stdin args and echo a JSON result.
read -r args
printf '{"content":[{"type":"text","text":"called with args"}],"is_error":false}\n'
`
	if err := os.WriteFile(entrypointScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}

	pluginTool := domain.MCPTool{Name: "my_tool", Description: "test tool"}
	activePlugin := domain.Plugin{
		ID:     "my-plugin-id",
		Name:   "my-plugin",
		Status: domain.PluginStatusActive,
		Manifest: domain.PluginManifest{
			Entrypoint: "run.sh",
			Provides:   domain.PluginProvides{Tools: []domain.MCPTool{pluginTool}},
		},
	}

	store := &mocks.PluginStore{
		ListFn: func(_ context.Context) ([]domain.Plugin, error) {
			return []domain.Plugin{activePlugin}, nil
		},
	}

	client := mcp.NewClient([]string{installDir},
		mcp.WithPluginStore(store),
		mcp.WithInstallDir(installDir),
	)

	result, err := client.CallTool(context.Background(), "my_tool", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success result, got error: %+v", result)
	}
	if len(result.Content) == 0 {
		t.Error("expected non-empty content")
	}
}
