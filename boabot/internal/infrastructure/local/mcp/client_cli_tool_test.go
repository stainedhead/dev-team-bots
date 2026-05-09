package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/mcp"
)

// fakeBinaryInDir creates an executable script named binName in dir and returns its path.
func fakeBinaryInDir(t *testing.T, dir, binName, script string) string {
	t.Helper()
	path := filepath.Join(dir, binName)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("write fake binary %s: %v", binName, err)
	}
	return path
}

// TestMCPClient_ListTools_IncludesRunClaudeCode verifies that run_claude_code
// appears in ListTools when enabled and the binary resolves.
func TestMCPClient_ListTools_IncludesRunClaudeCode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binaryPath := fakeBinaryInDir(t, dir, "claude", `printf 'ok\n'`)

	runner := &mocks.MockCLIAgentRunner{}
	cliTools := config.CLIToolsConfig{
		ClaudeCode: config.CLIToolConfig{Enabled: true, BinaryPath: binaryPath},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	found := false
	for _, tool := range tools {
		if tool.Name == "run_claude_code" {
			found = true
		}
	}
	if !found {
		t.Error("expected run_claude_code in tools list when enabled and binary resolves")
	}
}

// TestMCPClient_ListTools_ExcludesRunClaudeCode_Disabled verifies that
// run_claude_code is absent when enabled=false.
func TestMCPClient_ListTools_ExcludesRunClaudeCode_Disabled(t *testing.T) {
	t.Parallel()

	runner := &mocks.MockCLIAgentRunner{}
	cliTools := config.CLIToolsConfig{
		ClaudeCode: config.CLIToolConfig{Enabled: false, BinaryPath: "claude"},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range tools {
		if tool.Name == "run_claude_code" {
			t.Error("run_claude_code should not appear when disabled")
		}
	}
}

// TestMCPClient_ListTools_ExcludesRunClaudeCode_BinaryNotFound verifies that
// run_claude_code is absent when the binary cannot be resolved.
func TestMCPClient_ListTools_ExcludesRunClaudeCode_BinaryNotFound(t *testing.T) {
	t.Parallel()

	runner := &mocks.MockCLIAgentRunner{}
	cliTools := config.CLIToolsConfig{
		ClaudeCode: config.CLIToolConfig{Enabled: true, BinaryPath: "definitely-not-real-claude-xyz"},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range tools {
		if tool.Name == "run_claude_code" {
			t.Error("run_claude_code should not appear when binary not found")
		}
	}
}

// TestMCPClient_CallTool_RunClaudeCode_InvokesRunner verifies that calling
// run_claude_code invokes the CLIAgentRunner with the correct args.
func TestMCPClient_CallTool_RunClaudeCode_InvokesRunner(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binaryPath := fakeBinaryInDir(t, dir, "claude", `printf 'result\n'`)

	var capturedCfg domain.CLIAgentConfig
	var capturedInstruction string

	runner := &mocks.MockCLIAgentRunner{
		RunFn: func(_ context.Context, cfg domain.CLIAgentConfig, instruction string,
			_ <-chan string, _ func(string)) (string, error) {
			capturedCfg = cfg
			capturedInstruction = instruction
			return "task output", nil
		},
	}
	cliTools := config.CLIToolsConfig{
		ClaudeCode: config.CLIToolConfig{Enabled: true, BinaryPath: binaryPath},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	result, err := client.CallTool(context.Background(), "run_claude_code", map[string]any{
		"instruction": "do some work",
		"work_dir":    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %+v", result)
	}

	// Verify the runner was invoked with the correct args.
	if capturedCfg.Binary != binaryPath {
		t.Errorf("expected binary %q, got %q", binaryPath, capturedCfg.Binary)
	}
	if capturedInstruction != "do some work" {
		t.Errorf("expected instruction 'do some work', got %q", capturedInstruction)
	}
	// Claude args should include stream-json and dangerously-skip-permissions.
	argsStr := strings.Join(capturedCfg.Args, " ")
	if !strings.Contains(argsStr, "--output-format=stream-json") {
		t.Errorf("expected --output-format=stream-json in args, got: %q", argsStr)
	}
	if !strings.Contains(argsStr, "--dangerously-skip-permissions") {
		t.Errorf("expected --dangerously-skip-permissions in args, got: %q", argsStr)
	}
	if !strings.Contains(argsStr, "-p") {
		t.Errorf("expected -p in args, got: %q", argsStr)
	}
}

// TestMCPClient_CallTool_RunClaudeCode_ModelFlagIncluded verifies that the
// --model flag is included when the model argument is non-empty.
func TestMCPClient_CallTool_RunClaudeCode_ModelFlagIncluded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binaryPath := fakeBinaryInDir(t, dir, "claude", `printf 'ok\n'`)

	var capturedCfg domain.CLIAgentConfig
	runner := &mocks.MockCLIAgentRunner{
		RunFn: func(_ context.Context, cfg domain.CLIAgentConfig, _ string,
			_ <-chan string, _ func(string)) (string, error) {
			capturedCfg = cfg
			return "ok", nil
		},
	}
	cliTools := config.CLIToolsConfig{
		ClaudeCode: config.CLIToolConfig{Enabled: true, BinaryPath: binaryPath},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	_, err := client.CallTool(context.Background(), "run_claude_code", map[string]any{
		"instruction": "do work",
		"work_dir":    t.TempDir(),
		"model":       "claude-opus-4-5",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	argsStr := strings.Join(capturedCfg.Args, " ")
	if !strings.Contains(argsStr, "--model") {
		t.Errorf("expected --model in args when model specified, got: %q", argsStr)
	}
	if !strings.Contains(argsStr, "claude-opus-4-5") {
		t.Errorf("expected model name in args, got: %q", argsStr)
	}
}

// TestMCPClient_CallTool_RunClaudeCode_NoModelFlagWhenEmpty verifies that the
// --model flag is omitted when the model argument is empty.
func TestMCPClient_CallTool_RunClaudeCode_NoModelFlagWhenEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binaryPath := fakeBinaryInDir(t, dir, "claude", `printf 'ok\n'`)

	var capturedCfg domain.CLIAgentConfig
	runner := &mocks.MockCLIAgentRunner{
		RunFn: func(_ context.Context, cfg domain.CLIAgentConfig, _ string,
			_ <-chan string, _ func(string)) (string, error) {
			capturedCfg = cfg
			return "ok", nil
		},
	}
	cliTools := config.CLIToolsConfig{
		ClaudeCode: config.CLIToolConfig{Enabled: true, BinaryPath: binaryPath},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	_, err := client.CallTool(context.Background(), "run_claude_code", map[string]any{
		"instruction": "do work",
		"work_dir":    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	argsStr := strings.Join(capturedCfg.Args, " ")
	if strings.Contains(argsStr, "--model") {
		t.Errorf("expected no --model flag when model is empty, got: %q", argsStr)
	}
}

// TestMCPClient_CallTool_RunClaudeCode_ProgressCallback verifies that progress
// is forwarded from the runner to the MCP client's progress function.
func TestMCPClient_CallTool_RunClaudeCode_ProgressCallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binaryPath := fakeBinaryInDir(t, dir, "claude", `printf 'ok\n'`)

	runner := &mocks.MockCLIAgentRunner{
		RunFn: func(_ context.Context, _ domain.CLIAgentConfig, _ string,
			_ <-chan string, progress func(string)) (string, error) {
			if progress != nil {
				progress("progress line 1")
				progress("progress line 2")
			}
			return "final output", nil
		},
	}
	cliTools := config.CLIToolsConfig{
		ClaudeCode: config.CLIToolConfig{Enabled: true, BinaryPath: binaryPath},
	}

	var progressLines []string
	progressFn := func(line string) {
		progressLines = append(progressLines, line)
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
		mcp.WithProgressFn(progressFn),
	)

	_, err := client.CallTool(context.Background(), "run_claude_code", map[string]any{
		"instruction": "do work",
		"work_dir":    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if len(progressLines) != 2 {
		t.Errorf("expected 2 progress lines, got %d: %v", len(progressLines), progressLines)
	}
}

// TestMCPClient_ListTools_RunCodex_IncludedWhenEnabled verifies run_codex
// appears when enabled and binary is found.
func TestMCPClient_ListTools_RunCodex_IncludedWhenEnabled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binaryPath := fakeBinaryInDir(t, dir, "codex", `printf 'ok\n'`)

	runner := &mocks.MockCLIAgentRunner{}
	cliTools := config.CLIToolsConfig{
		Codex: config.CLIToolConfig{Enabled: true, BinaryPath: binaryPath},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	found := false
	for _, tool := range tools {
		if tool.Name == "run_codex" {
			found = true
		}
	}
	if !found {
		t.Error("expected run_codex in tools list")
	}
}

// TestMCPClient_CallTool_RunCodex_InvokesRunner verifies run_codex args.
func TestMCPClient_CallTool_RunCodex_InvokesRunner(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binaryPath := fakeBinaryInDir(t, dir, "codex", `printf 'codex output\n'`)

	var capturedCfg domain.CLIAgentConfig
	runner := &mocks.MockCLIAgentRunner{
		RunFn: func(_ context.Context, cfg domain.CLIAgentConfig, _ string,
			_ <-chan string, _ func(string)) (string, error) {
			capturedCfg = cfg
			return "codex output", nil
		},
	}
	cliTools := config.CLIToolsConfig{
		Codex: config.CLIToolConfig{Enabled: true, BinaryPath: binaryPath},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	_, err := client.CallTool(context.Background(), "run_codex", map[string]any{
		"instruction": "implement the feature",
		"work_dir":    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	argsStr := strings.Join(capturedCfg.Args, " ")
	if !strings.Contains(argsStr, "-q") {
		t.Errorf("expected -q in codex args, got: %q", argsStr)
	}
	if !strings.Contains(argsStr, "--approval-mode=full-auto") {
		t.Errorf("expected --approval-mode=full-auto in codex args, got: %q", argsStr)
	}
}

// TestMCPClient_ListTools_RunOpenAICodex_IncludedWhenEnabled verifies run_openai_codex.
func TestMCPClient_ListTools_RunOpenAICodex_IncludedWhenEnabled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binaryPath := fakeBinaryInDir(t, dir, "openai-codex", `printf 'ok\n'`)

	runner := &mocks.MockCLIAgentRunner{}
	cliTools := config.CLIToolsConfig{
		OpenAICodex: config.CLIToolConfig{Enabled: true, BinaryPath: binaryPath},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	found := false
	for _, tool := range tools {
		if tool.Name == "run_openai_codex" {
			found = true
		}
	}
	if !found {
		t.Error("expected run_openai_codex in tools list")
	}
}

// TestMCPClient_ListTools_RunOpenCode_IncludedWhenEnabled verifies run_opencode.
func TestMCPClient_ListTools_RunOpenCode_IncludedWhenEnabled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binaryPath := fakeBinaryInDir(t, dir, "opencode", `printf 'ok\n'`)

	runner := &mocks.MockCLIAgentRunner{}
	cliTools := config.CLIToolsConfig{
		OpenCode: config.CLIToolConfig{Enabled: true, BinaryPath: binaryPath},
	}

	client := mcp.NewClient([]string{t.TempDir()},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	found := false
	for _, tool := range tools {
		if tool.Name == "run_opencode" {
			found = true
		}
	}
	if !found {
		t.Error("expected run_opencode in tools list")
	}
}
