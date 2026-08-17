package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	localmcp "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/mcp"
	localplugin "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/plugin"
)

// listToolNames is a small test helper collecting MCPTool.Name from ListTools.
func listToolNames(t *testing.T, client *localmcp.Client) map[string]bool {
	t.Helper()
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	return names
}

// --- P2.1/P2.2: board store (FR-402/403) ---

func TestBuildACPMCPOptions_BoardStore_ActivatedForNonTechLead(t *testing.T) {
	cfg := config.Config{Bot: config.BotConfig{Name: "worker-bot", BotType: "worker"}}
	opts, _ := buildACPMCPOptions(cfg, t.TempDir(), nil)
	client := localmcp.NewClient(nil, opts...)

	if !listToolNames(t, client)["complete_board_item"] {
		t.Error("expected complete_board_item tool to be present for a non-tech-lead persona")
	}
}

func TestBuildACPMCPOptions_BoardStore_NotActivatedForTechLead(t *testing.T) {
	cfg := config.Config{Bot: config.BotConfig{Name: "tl-bot", BotType: "tech-lead"}}
	opts, _ := buildACPMCPOptions(cfg, t.TempDir(), nil)
	client := localmcp.NewClient(nil, opts...)

	if listToolNames(t, client)["complete_board_item"] {
		t.Error("expected complete_board_item tool to be absent for a tech-lead persona, mirroring team_manager.go:1023-1024")
	}
}

// TestBuildACPMCPOptions_BoardStore_ActivatedForNonTechLead_IncludesListTool
// is channel-agnostic-tool-parity-PRD.md FR-601's wiring-level regression
// test: the read-side tool must reach the ACP-mode MCP client at the exact
// same construction site/gate as complete_board_item, not just exist in the
// mcp package in isolation.
func TestBuildACPMCPOptions_BoardStore_ActivatedForNonTechLead_IncludesListTool(t *testing.T) {
	cfg := config.Config{Bot: config.BotConfig{Name: "worker-bot", BotType: "worker"}}
	opts, _ := buildACPMCPOptions(cfg, t.TempDir(), nil)
	client := localmcp.NewClient(nil, opts...)

	if !listToolNames(t, client)["list_board_items"] {
		t.Error("expected list_board_items tool to be present for a non-tech-lead persona")
	}
}

// TestBuildACPMCPOptions_DirectTaskStore_ActivatedWhenProvided is FR-602's
// wiring-level test: a non-nil taskStore reaches the ACP-mode MCP client as
// list_my_tasks.
func TestBuildACPMCPOptions_DirectTaskStore_ActivatedWhenProvided(t *testing.T) {
	cfg := config.Config{Bot: config.BotConfig{Name: "worker-bot", BotType: "worker"}}
	ts := &fakeDirectTaskStoreForACPTest{}
	opts, _ := buildACPMCPOptions(cfg, t.TempDir(), ts)
	client := localmcp.NewClient(nil, opts...)

	if !listToolNames(t, client)["list_my_tasks"] {
		t.Error("expected list_my_tasks tool to be present when a direct task store is provided")
	}
}

func TestBuildACPMCPOptions_DirectTaskStore_NotActivatedWhenNil(t *testing.T) {
	cfg := config.Config{Bot: config.BotConfig{Name: "worker-bot", BotType: "worker"}}
	opts, _ := buildACPMCPOptions(cfg, t.TempDir(), nil)
	client := localmcp.NewClient(nil, opts...)

	if listToolNames(t, client)["list_my_tasks"] {
		t.Error("expected list_my_tasks tool to be absent without a direct task store")
	}
}

// TestBuildACPMCPOptions_BoardStore_EndToEnd_CompletesItem proves the board
// store isn't just wired -- an ACP-sourced task can actually call
// complete_board_item and mark a real work item done end-to-end (spec.md's
// acceptance criteria explicitly reject asserting only that the constructor
// option was passed).
func TestBuildACPMCPOptions_BoardStore_EndToEnd_CompletesItem(t *testing.T) {
	memPath := t.TempDir()
	boardPath := filepath.Join(memPath, "board.json")
	seedBoardFile(t, boardPath, domain.WorkItem{
		ID:     "item-1",
		Title:  "do the thing",
		Status: domain.WorkItemStatusInProgress,
	})

	cfg := config.Config{Bot: config.BotConfig{Name: "worker-bot", BotType: "worker"}}
	opts, _ := buildACPMCPOptions(cfg, memPath, nil)
	mcpClient := localmcp.NewClient(nil, opts...)

	n := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			n++
			if n == 1 {
				return domain.InvokeResponse{
					ToolCalls: []domain.ToolCall{{
						ID:   "c1",
						Name: "complete_board_item",
						Args: map[string]any{"item_id": "item-1", "output": "did it"},
					}},
					StopReason: "tool_calls",
				}, nil
			}
			return domain.InvokeResponse{Content: "done", StopReason: "end_turn"}, nil
		},
	}

	worker := application.NewExecuteTaskUseCase(provider, mcpClient, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{}, "soul")
	result, err := worker.Execute(context.Background(), domain.Task{ID: "t1", Source: "acp", Instruction: "finish item-1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected Success=true, got result: %+v", result)
	}

	toolMsg := lastToolMessage(t, provider.InvokeCalls)
	if !strings.Contains(toolMsg, `board item "item-1" marked as done`) {
		t.Errorf("expected tool result to confirm completion, got %q", toolMsg)
	}
}

// --- P3.1: plugin store (FR-404) ---

func TestBuildACPMCPOptions_PluginStore_ActivatedWhenInstallDirSet(t *testing.T) {
	cfg := config.Config{
		Bot:          config.BotConfig{Name: "worker-bot", BotType: "worker"},
		Orchestrator: config.OrchestratorConfig{Plugins: config.PluginsConfig{InstallDir: filepath.Join(t.TempDir(), "plugins")}},
	}
	opts, _ := buildACPMCPOptions(cfg, t.TempDir(), nil)
	client := localmcp.NewClient(nil, opts...)

	if !listToolNames(t, client)["read_skill"] {
		t.Error("expected read_skill tool to be present when orchestrator.plugins.install_dir is set")
	}
}

func TestBuildACPMCPOptions_PluginStore_NotActivatedWhenInstallDirEmpty(t *testing.T) {
	cfg := config.Config{Bot: config.BotConfig{Name: "worker-bot", BotType: "worker"}}
	opts, _ := buildACPMCPOptions(cfg, t.TempDir(), nil)
	client := localmcp.NewClient(nil, opts...)

	if listToolNames(t, client)["read_skill"] {
		t.Error("expected read_skill tool to be absent when orchestrator.plugins.install_dir is not set")
	}
}

// TestBuildACPMCPOptions_PluginStore_RelativeInstallDirResolvedAgainstMemPath
// mirrors team_manager.go:517-519's relative-install-dir resolution: a
// relative install_dir is joined against the persona's own memPath, not
// treated as relative to the process CWD.
func TestBuildACPMCPOptions_PluginStore_RelativeInstallDirResolvedAgainstMemPath(t *testing.T) {
	memPath := t.TempDir()
	cfg := config.Config{
		Bot:          config.BotConfig{Name: "worker-bot", BotType: "worker"},
		Orchestrator: config.OrchestratorConfig{Plugins: config.PluginsConfig{InstallDir: "plugins"}},
	}
	opts, _ := buildACPMCPOptions(cfg, memPath, nil)
	client := localmcp.NewClient(nil, opts...)

	if !listToolNames(t, client)["read_skill"] {
		t.Error("expected read_skill tool to be present for a relative install_dir")
	}
	if info, err := os.Stat(filepath.Join(memPath, "plugins")); err != nil || !info.IsDir() {
		t.Errorf("expected relative install_dir to resolve under memPath (%s/plugins), stat err: %v", memPath, err)
	}
}

// TestBuildACPMCPOptions_PluginStore_ConstructionFailureDegradesGracefully
// verifies NFR-Reliability: a plugin store that fails to construct (its
// install_dir collides with an existing regular file, so MkdirAll fails)
// is logged and simply omitted -- not a fatal error, and the board/CLI
// wiring around it is unaffected.
func TestBuildACPMCPOptions_PluginStore_ConstructionFailureDegradesGracefully(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	cfg := config.Config{
		Bot:          config.BotConfig{Name: "worker-bot", BotType: "worker"},
		Orchestrator: config.OrchestratorConfig{Plugins: config.PluginsConfig{InstallDir: filepath.Join(blocker, "plugins")}},
	}
	opts, _ := buildACPMCPOptions(cfg, t.TempDir(), nil)
	client := localmcp.NewClient(nil, opts...)

	if listToolNames(t, client)["read_skill"] {
		t.Error("expected read_skill tool to be absent when plugin store construction fails")
	}
	// Board wiring (unrelated to plugin construction) must still be present.
	if !listToolNames(t, client)["complete_board_item"] {
		t.Error("expected board wiring to be unaffected by a plugin store construction failure")
	}
}

// TestBuildACPMCPOptions_PluginStore_EndToEnd_ReadSkill proves an
// ACP-sourced task can actually invoke a plugin-provided tool -- calling
// the tool by the name the plugin's own manifest declares in
// Provides.Tools ("my-skill"), dispatched through Client.callPluginTool
// (client.go:299-330), not just that WithPluginStore was passed as a
// constructor argument. This deliberately does NOT call the client's own
// always-present read_skill builtin directly (client.go:167-185) -- that
// would only prove the plugin store is non-nil, not that a plugin's own
// declared tool is reachable.
//
// The manifest's Entrypoint is "plugin.json" (Claude-Code-style skill
// plugin), which routes callPluginTool to isPluginJSONEntrypoint's
// readSkill delegation instead of spawning the entrypoint as a subprocess
// (client.go:317-321). This is deliberate, not a simplification of
// convenience: Extract's extractArchive (installer.go's os.Create-based
// file write) does not preserve the tar header's executable bit, so an
// archive-shipped run.sh-style entrypoint is never executable after
// installation in this test harness -- exercising the subprocess-entrypoint
// branch would require a real filesystem chmod after Install, which is not
// how a real plugin ever installs skills of this shape either.
func TestBuildACPMCPOptions_PluginStore_EndToEnd_ReadSkill(t *testing.T) {
	installDir := filepath.Join(t.TempDir(), "plugins")
	seedStore, err := localplugin.NewLocalPluginStore(installDir)
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}
	archive, checksum := buildSkillArchive(t, "my-skill", "do the documented thing")
	manifest := domain.PluginManifest{
		Name:       "my-skill-plugin",
		Version:    "1.0.0",
		Entrypoint: "plugin.json",
		Checksums:  map[string]string{"sha256": checksum},
		Provides: domain.PluginProvides{
			Tools: []domain.MCPTool{{Name: "my-skill", Description: "a test skill"}},
		},
	}
	if _, err := seedStore.Install(context.Background(), manifest, archive, "test", true); err != nil {
		t.Fatalf("Install: %v", err)
	}

	cfg := config.Config{
		Bot:          config.BotConfig{Name: "worker-bot", BotType: "worker"},
		Orchestrator: config.OrchestratorConfig{Plugins: config.PluginsConfig{InstallDir: installDir}},
	}
	opts, _ := buildACPMCPOptions(cfg, t.TempDir(), nil)
	mcpClient := localmcp.NewClient(nil, opts...)

	// Confirm ListTools actually advertises the plugin's own declared tool
	// name, not just the always-present read_skill builtin.
	if !listToolNames(t, mcpClient)["my-skill"] {
		t.Fatal("expected the plugin-declared tool \"my-skill\" to be listed")
	}

	n := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			n++
			if n == 1 {
				return domain.InvokeResponse{
					ToolCalls: []domain.ToolCall{{
						ID:   "c1",
						Name: "my-skill",
						Args: map[string]any{},
					}},
					StopReason: "tool_calls",
				}, nil
			}
			return domain.InvokeResponse{Content: "done", StopReason: "end_turn"}, nil
		},
	}

	worker := application.NewExecuteTaskUseCase(provider, mcpClient, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{}, "soul")
	result, err := worker.Execute(context.Background(), domain.Task{ID: "t1", Source: "acp", Instruction: "use my-skill"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected Success=true, got result: %+v", result)
	}

	toolMsg := lastToolMessage(t, provider.InvokeCalls)
	if !strings.Contains(toolMsg, "do the documented thing") {
		t.Errorf("expected tool result to contain the plugin skill's markdown content, got %q", toolMsg)
	}
}

// --- P4.1: CLI tools (FR-405) ---

func TestBuildACPMCPOptions_CLIRunner_AlwaysWired_NoToolsListedWithoutEnabling(t *testing.T) {
	cfg := config.Config{Bot: config.BotConfig{Name: "worker-bot", BotType: "worker"}}
	opts, _ := buildACPMCPOptions(cfg, t.TempDir(), nil)
	client := localmcp.NewClient(nil, opts...)

	names := listToolNames(t, client)
	for _, cliTool := range []string{"run_claude_code", "run_codex", "run_openai_codex", "run_opencode"} {
		if names[cliTool] {
			t.Errorf("expected %s to be absent when no CLI tool is enabled", cliTool)
		}
	}
}

func TestBuildACPMCPOptions_CLITools_PerToolGatingMatchesEnabledFlag(t *testing.T) {
	fakeBin := fakeExecutable(t, "opencode-fake", "#!/bin/sh\nprintf 'ok'\n")
	cfg := config.Config{
		Bot: config.BotConfig{Name: "worker-bot", BotType: "worker"},
		Orchestrator: config.OrchestratorConfig{
			CLITools: config.CLIToolsConfig{
				OpenCode: config.CLIToolConfig{Enabled: true, BinaryPath: fakeBin},
			},
		},
	}
	opts, _ := buildACPMCPOptions(cfg, t.TempDir(), nil)
	client := localmcp.NewClient(nil, opts...)

	names := listToolNames(t, client)
	if !names["run_opencode"] {
		t.Error("expected run_opencode to be present when cli_tools.opencode.enabled=true and its binary resolves")
	}
	if names["run_claude_code"] || names["run_codex"] || names["run_openai_codex"] {
		t.Error("expected only run_opencode to be present; other CLI tools were not enabled")
	}
}

// TestBuildACPMCPOptions_CLITools_EndToEnd_RunOpenCode proves an
// ACP-sourced task can actually invoke an enabled CLI tool end-to-end
// (real cliagent.SubprocessRunner spawning a real, if fake, binary), not
// just that WithCLIRunner/WithCLITools were passed as constructor
// arguments.
func TestBuildACPMCPOptions_CLITools_EndToEnd_RunOpenCode(t *testing.T) {
	fakeBin := fakeExecutable(t, "opencode-fake", "#!/bin/sh\nprintf 'opencode fake output'\n")
	workDir := t.TempDir()

	cfg := config.Config{
		Bot: config.BotConfig{Name: "worker-bot", BotType: "worker"},
		Orchestrator: config.OrchestratorConfig{
			CLITools: config.CLIToolsConfig{
				OpenCode: config.CLIToolConfig{Enabled: true, BinaryPath: fakeBin},
			},
		},
	}
	opts, _ := buildACPMCPOptions(cfg, t.TempDir(), nil)
	mcpClient := localmcp.NewClient([]string{workDir}, opts...)

	n := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			n++
			if n == 1 {
				return domain.InvokeResponse{
					ToolCalls: []domain.ToolCall{{
						ID:   "c1",
						Name: "run_opencode",
						Args: map[string]any{"instruction": "do it", "work_dir": workDir},
					}},
					StopReason: "tool_calls",
				}, nil
			}
			return domain.InvokeResponse{Content: "done", StopReason: "end_turn"}, nil
		},
	}

	worker := application.NewExecuteTaskUseCase(provider, mcpClient, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{}, "soul")
	result, err := worker.Execute(context.Background(), domain.Task{ID: "t1", Source: "acp", Instruction: "run opencode"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected Success=true, got result: %+v", result)
	}

	toolMsg := lastToolMessage(t, provider.InvokeCalls)
	if !strings.Contains(toolMsg, "opencode fake output") {
		t.Errorf("expected tool result to contain the fake CLI tool's stdout, got %q", toolMsg)
	}
}

// --- shared test helpers ---

// seedBoardFile writes items in the exact on-disk format
// orchestratorlocal.InMemoryBoardStore.loadFromDisk expects (a bare JSON
// array of domain.WorkItem), so a freshly constructed store picks them up
// without needing access to the store's own internals.
func seedBoardFile(t *testing.T, path string, items ...domain.WorkItem) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir board dir: %v", err)
	}
	data, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal board items: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write board file: %v", err)
	}
}

// lastToolMessage returns the Content of the last role="tool" message across
// all recorded InvokeCalls -- the tool result the second model invocation
// actually saw.
func lastToolMessage(t *testing.T, calls []domain.InvokeRequest) string {
	t.Helper()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 provider invocations, got %d", len(calls))
	}
	messages := calls[len(calls)-1].Messages
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "tool" {
			return messages[i].Content
		}
	}
	t.Fatal("no tool-role message found in the final invocation's history")
	return ""
}

// buildSkillArchive builds a tar.gz archive containing
// commands/<skillName>.md with the given content, plus a run.sh (satisfies
// no particular requirement here, just realistic plugin shape), returning
// the archive bytes and its sha256 hex checksum for domain.PluginManifest.
func buildSkillArchive(t *testing.T, skillName, mdContent string) (archive []byte, checksumHex string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	files := map[string]string{
		"run.sh":                        "#!/bin/sh\necho hello\n",
		"commands/" + skillName + ".md": mdContent,
	}
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write tar content %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

// fakeExecutable writes an executable shell script to a temp dir and
// returns its absolute path, for CLI-tool end-to-end tests that need a
// real (if fake) subprocess-launchable binary.
func fakeExecutable(t *testing.T, name, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake executable %s: %v", name, err)
	}
	return path
}

// fakeDirectTaskStoreForACPTest is a minimal domain.DirectTaskStore for
// buildACPMCPOptions' FR-602 wiring tests -- only List is ever exercised by
// list_my_tasks; every other method is an unused stub.
type fakeDirectTaskStoreForACPTest struct{}

func (fakeDirectTaskStoreForACPTest) Create(_ context.Context, t domain.DirectTask) (domain.DirectTask, error) {
	return t, nil
}
func (fakeDirectTaskStoreForACPTest) Update(_ context.Context, t domain.DirectTask) (domain.DirectTask, error) {
	return t, nil
}
func (fakeDirectTaskStoreForACPTest) Get(_ context.Context, _ string) (domain.DirectTask, error) {
	return domain.DirectTask{}, nil
}
func (fakeDirectTaskStoreForACPTest) List(_ context.Context, _ string) ([]domain.DirectTask, error) {
	return nil, nil
}
func (fakeDirectTaskStoreForACPTest) ListAll(_ context.Context) ([]domain.DirectTask, error) {
	return nil, nil
}
func (fakeDirectTaskStoreForACPTest) ListBySource(_ context.Context, _ domain.DirectTaskSource) ([]domain.DirectTask, error) {
	return nil, nil
}
func (fakeDirectTaskStoreForACPTest) Delete(_ context.Context, _ string) error { return nil }
func (fakeDirectTaskStoreForACPTest) ListDue(_ context.Context, _ time.Time) ([]domain.DirectTask, error) {
	return nil, nil
}
func (fakeDirectTaskStoreForACPTest) ClaimDue(_ context.Context, _ string) (bool, error) {
	return false, nil
}

var _ domain.DirectTaskStore = fakeDirectTaskStoreForACPTest{}
