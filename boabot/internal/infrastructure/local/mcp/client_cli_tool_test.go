package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	workDir := t.TempDir() // a directory in allowedDirs

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

	client := mcp.NewClient([]string{workDir},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	result, err := client.CallTool(context.Background(), "run_claude_code", map[string]any{
		"instruction": "do some work",
		"work_dir":    workDir,
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
	workDir := t.TempDir()

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

	client := mcp.NewClient([]string{workDir},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	_, err := client.CallTool(context.Background(), "run_claude_code", map[string]any{
		"instruction": "do work",
		"work_dir":    workDir,
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
	workDir := t.TempDir()

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

	client := mcp.NewClient([]string{workDir},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	_, err := client.CallTool(context.Background(), "run_claude_code", map[string]any{
		"instruction": "do work",
		"work_dir":    workDir,
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
	workDir := t.TempDir()

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

	client := mcp.NewClient([]string{workDir},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
		mcp.WithProgressFn(progressFn),
	)

	_, err := client.CallTool(context.Background(), "run_claude_code", map[string]any{
		"instruction": "do work",
		"work_dir":    workDir,
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
	workDir := t.TempDir()

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

	client := mcp.NewClient([]string{workDir},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	_, err := client.CallTool(context.Background(), "run_codex", map[string]any{
		"instruction": "implement the feature",
		"work_dir":    workDir,
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

// TestCallCLITool_WorkDirOutsideAllowedDirs verifies that callCLITool returns an
// error result when work_dir is outside the client's allowed directories.
func TestCallCLITool_WorkDirOutsideAllowedDirs(t *testing.T) {
	t.Parallel()

	allowedDir := t.TempDir()
	forbiddenDir := t.TempDir() // a different temp dir — outside allowed

	binaryDir := t.TempDir()
	binaryPath := fakeBinaryInDir(t, binaryDir, "claude", `printf 'ok\n'`)

	runner := &mocks.MockCLIAgentRunner{
		RunFn: func(_ context.Context, _ domain.CLIAgentConfig, _ string,
			_ <-chan string, _ func(string)) (string, error) {
			return "should not reach here", nil
		},
	}
	cliTools := config.CLIToolsConfig{
		ClaudeCode: config.CLIToolConfig{Enabled: true, BinaryPath: binaryPath},
	}

	client := mcp.NewClient([]string{allowedDir},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	result, err := client.CallTool(context.Background(), "run_claude_code", map[string]any{
		"instruction": "do some work",
		"work_dir":    forbiddenDir,
	})
	if err != nil {
		t.Fatalf("CallTool returned unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected IsError=true when work_dir is outside allowedDirs, got success: %+v", result)
	}
	if len(runner.RunCalls) > 0 {
		t.Error("runner.Run should not have been called when work_dir is rejected")
	}
}

// TestCallCLITool_EmptyWorkDir verifies that callCLITool returns an error when
// work_dir is absent or empty.
func TestCallCLITool_EmptyWorkDir(t *testing.T) {
	t.Parallel()

	allowedDir := t.TempDir()
	binaryDir := t.TempDir()
	binaryPath := fakeBinaryInDir(t, binaryDir, "claude", `printf 'ok\n'`)

	runner := &mocks.MockCLIAgentRunner{}
	cliTools := config.CLIToolsConfig{
		ClaudeCode: config.CLIToolConfig{Enabled: true, BinaryPath: binaryPath},
	}

	client := mcp.NewClient([]string{allowedDir},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	// Missing work_dir (empty string via type assertion on absent key).
	result, err := client.CallTool(context.Background(), "run_claude_code", map[string]any{
		"instruction": "do some work",
		// work_dir intentionally absent
	})
	if err != nil {
		t.Fatalf("CallTool returned unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected IsError=true when work_dir is missing, got success: %+v", result)
	}
}

// TestCallCLITool_AllToolsRejectOutOfScopeWorkDir verifies that all four CLI tools
// reject a work_dir outside allowedDirs.
func TestCallCLITool_AllToolsRejectOutOfScopeWorkDir(t *testing.T) {
	t.Parallel()

	allowedDir := t.TempDir()
	forbiddenDir := t.TempDir()

	binaryDir := t.TempDir()
	claudePath := fakeBinaryInDir(t, binaryDir, "claude", `printf 'ok\n'`)
	codexPath := fakeBinaryInDir(t, binaryDir, "codex", `printf 'ok\n'`)
	openaiCodexPath := fakeBinaryInDir(t, binaryDir, "openai-codex", `printf 'ok\n'`)
	opencodePath := fakeBinaryInDir(t, binaryDir, "opencode", `printf 'ok\n'`)

	runner := &mocks.MockCLIAgentRunner{}
	cliTools := config.CLIToolsConfig{
		ClaudeCode:  config.CLIToolConfig{Enabled: true, BinaryPath: claudePath},
		Codex:       config.CLIToolConfig{Enabled: true, BinaryPath: codexPath},
		OpenAICodex: config.CLIToolConfig{Enabled: true, BinaryPath: openaiCodexPath},
		OpenCode:    config.CLIToolConfig{Enabled: true, BinaryPath: opencodePath},
	}

	client := mcp.NewClient([]string{allowedDir},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	toolNames := []string{"run_claude_code", "run_codex", "run_openai_codex", "run_opencode"}
	for _, toolName := range toolNames {
		t.Run(toolName, func(t *testing.T) {
			result, err := client.CallTool(context.Background(), toolName, map[string]any{
				"instruction": "do work",
				"work_dir":    forbiddenDir,
			})
			if err != nil {
				t.Fatalf("%s: CallTool returned unexpected Go error: %v", toolName, err)
			}
			if !result.IsError {
				t.Errorf("%s: expected IsError=true when work_dir outside allowedDirs", toolName)
			}
		})
	}

	if len(runner.RunCalls) > 0 {
		t.Errorf("runner.Run should not have been called for any out-of-scope work_dir, got %d calls", len(runner.RunCalls))
	}
}

// TestCallCLITool_ContextCancelledDuringRun verifies that context cancellation
// propagates from callCLITool to the underlying CLI runner.
func TestCallCLITool_ContextCancelledDuringRun(t *testing.T) {
	t.Parallel()

	allowedDir := t.TempDir()
	binaryDir := t.TempDir()
	binaryPath := fakeBinaryInDir(t, binaryDir, "claude", `printf 'ok\n'`)

	ctx, cancel := context.WithCancel(context.Background())

	runner := &mocks.MockCLIAgentRunner{
		RunFn: func(runCtx context.Context, _ domain.CLIAgentConfig, _ string,
			_ <-chan string, _ func(string)) (string, error) {
			// Block until context is cancelled (simulates a long-running subprocess).
			<-runCtx.Done()
			return "", runCtx.Err()
		},
	}
	cliTools := config.CLIToolsConfig{
		ClaudeCode: config.CLIToolConfig{Enabled: true, BinaryPath: binaryPath},
	}

	client := mcp.NewClient([]string{allowedDir},
		mcp.WithCLIRunner(runner),
		mcp.WithCLITools(cliTools),
	)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = client.CallTool(ctx, "run_claude_code", map[string]any{
			"instruction": "do work",
			"work_dir":    allowedDir,
		})
	}()

	// Cancel context after short delay.
	cancel()

	select {
	case <-done:
		// Good — CallTool returned.
	case <-time.After(2 * time.Second):
		t.Fatal("CallTool did not return after context cancellation within 2s")
	}
}

// TestListTools_NonExecutableAbsoluteBinaryExcluded verifies that a CLI tool
// backed by a non-executable absolute-path binary is absent from ListTools.
func TestListTools_NonExecutableAbsoluteBinaryExcluded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Create a file without the executable bit.
	nonExecPath := filepath.Join(dir, "claude")
	if err := os.WriteFile(nonExecPath, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("write non-executable binary: %v", err)
	}

	runner := &mocks.MockCLIAgentRunner{}
	cliTools := config.CLIToolsConfig{
		ClaudeCode: config.CLIToolConfig{Enabled: true, BinaryPath: nonExecPath},
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
			t.Error("run_claude_code should not appear when binary is not executable")
		}
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
