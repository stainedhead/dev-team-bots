package commands_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabotctl/internal/commands"
	"github.com/stainedhead/dev-team-bots/boabotctl/internal/domain"
)

func TestPluginList_Output(t *testing.T) {
	mc := &mockClient{
		pluginListResp: []domain.Plugin{
			{
				ID:          "abc123",
				Name:        "github-pr-reviewer",
				Version:     "1.2.0",
				Registry:    "official",
				Status:      "active",
				InstalledAt: "2026-05-07T12:00:00Z",
			},
		},
	}
	var buf bytes.Buffer
	cmd := commands.NewPluginCmd(mc, &buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plugin list: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "github-pr-reviewer") {
		t.Errorf("expected plugin name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "official") {
		t.Errorf("expected registry in output, got:\n%s", out)
	}
	if !strings.Contains(out, "active") {
		t.Errorf("expected status in output, got:\n%s", out)
	}
}

func TestPluginInfo_Output(t *testing.T) {
	mc := &mockClient{
		pluginListResp: []domain.Plugin{
			{
				ID:          "abc123",
				Name:        "github-pr-reviewer",
				Version:     "1.2.0",
				Registry:    "official",
				Status:      "active",
				InstalledAt: "2026-05-07T12:00:00Z",
				Manifest: domain.PluginManifest{
					Entrypoint: "run.sh",
					Provides: domain.PluginProvides{
						Tools: []domain.PluginTool{{Name: "review_pr", Description: "Review a PR"}},
					},
				},
			},
		},
		pluginGetResp: domain.Plugin{
			ID:          "abc123",
			Name:        "github-pr-reviewer",
			Version:     "1.2.0",
			Registry:    "official",
			Status:      "active",
			InstalledAt: "2026-05-07T12:00:00Z",
			Manifest: domain.PluginManifest{
				Entrypoint: "run.sh",
				Provides: domain.PluginProvides{
					Tools: []domain.PluginTool{{Name: "review_pr", Description: "Review a PR"}},
				},
			},
		},
	}
	var buf bytes.Buffer
	cmd := commands.NewPluginCmd(mc, &buf)
	cmd.SetArgs([]string{"info", "github-pr-reviewer"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plugin info: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "github-pr-reviewer") {
		t.Errorf("expected name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "review_pr") {
		t.Errorf("expected tool name in output, got:\n%s", out)
	}
}

func TestPluginInstall_CallsClient(t *testing.T) {
	mc := &mockClient{
		pluginInstallResp: domain.Plugin{
			ID:     "new-id",
			Name:   "my-tool",
			Status: "staged",
		},
	}
	var buf bytes.Buffer
	cmd := commands.NewPluginCmd(mc, &buf)
	cmd.SetArgs([]string{"install", "my-tool", "--registry", "official", "--version", "1.0.0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plugin install: %v", err)
	}
	if mc.lastPluginInstallReq.Name != "my-tool" {
		t.Errorf("expected name=my-tool, got %q", mc.lastPluginInstallReq.Name)
	}
	if mc.lastPluginInstallReq.Registry != "official" {
		t.Errorf("expected registry=official, got %q", mc.lastPluginInstallReq.Registry)
	}
	if mc.lastPluginInstallReq.Version != "1.0.0" {
		t.Errorf("expected version=1.0.0, got %q", mc.lastPluginInstallReq.Version)
	}
}

func TestPluginRemove_CallsClient(t *testing.T) {
	mc := &mockClient{
		pluginListResp: []domain.Plugin{
			{ID: "abc123", Name: "my-tool", Status: "active"},
		},
	}
	var buf bytes.Buffer
	cmd := commands.NewPluginCmd(mc, &buf)
	cmd.SetArgs([]string{"remove", "my-tool"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plugin remove: %v", err)
	}
	if mc.lastPluginRemoveID != "abc123" {
		t.Errorf("expected remove called with abc123, got %q", mc.lastPluginRemoveID)
	}
}

func TestPluginReload_CallsClient(t *testing.T) {
	mc := &mockClient{
		pluginListResp: []domain.Plugin{
			{ID: "abc123", Name: "my-tool", Status: "active"},
		},
	}
	var buf bytes.Buffer
	cmd := commands.NewPluginCmd(mc, &buf)
	cmd.SetArgs([]string{"reload", "my-tool"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plugin reload: %v", err)
	}
	if mc.lastPluginReloadID != "abc123" {
		t.Errorf("expected reload called with abc123, got %q", mc.lastPluginReloadID)
	}
}

func TestPluginInfo_NotFound(t *testing.T) {
	mc := &mockClient{
		pluginListResp: []domain.Plugin{}, // empty
	}
	var buf bytes.Buffer
	cmd := commands.NewPluginCmd(mc, &buf)
	cmd.SetArgs([]string{"info", "nonexistent"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent plugin, got nil")
	}
}
