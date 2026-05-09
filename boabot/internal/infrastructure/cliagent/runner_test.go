package cliagent_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/cliagent"
)

// writeSh writes a shell script to a temp directory and returns its path.
func writeSh(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+content), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestSubprocessRunner_NormalCompletion verifies that the runner accumulates
// all stdout lines and returns them.
func TestSubprocessRunner_NormalCompletion(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}

	dir := t.TempDir()
	script := writeSh(t, dir, "multi.sh", `printf 'line1\nline2\nline3\n'`)

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  script,
		WorkDir: dir,
	}

	result, err := runner.Run(context.Background(), cfg, "", nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(result, "line1") {
		t.Errorf("expected result to contain 'line1', got: %q", result)
	}
	if !strings.Contains(result, "line2") {
		t.Errorf("expected result to contain 'line2', got: %q", result)
	}
	if !strings.Contains(result, "line3") {
		t.Errorf("expected result to contain 'line3', got: %q", result)
	}
}

// TestSubprocessRunner_EchoOutput tests normal completion using the echo command.
func TestSubprocessRunner_EchoOutput(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}

	dir := t.TempDir()
	script := writeSh(t, dir, "echo.sh", `printf 'hello\n'`)

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  script,
		WorkDir: dir,
	}

	result, err := runner.Run(context.Background(), cfg, "", nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected 'hello' in result, got: %q", result)
	}
}

// TestSubprocessRunner_ProgressCallback verifies that progress is called for
// each non-empty output line.
func TestSubprocessRunner_ProgressCallback(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}

	dir := t.TempDir()
	script := writeSh(t, dir, "prog.sh", `printf 'alpha\nbeta\ngamma\n'`)

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  script,
		WorkDir: dir,
	}

	var mu sync.Mutex
	var lines []string
	progress := func(line string) {
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
	}

	_, err := runner.Run(context.Background(), cfg, "", nil, progress)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	got := lines
	mu.Unlock()

	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("expected %d progress lines, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("progress[%d]: expected %q, got %q", i, w, got[i])
		}
	}
}

// TestSubprocessRunner_NilStdinChannel verifies that nil stdin channel works normally.
func TestSubprocessRunner_NilStdinChannel(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}

	dir := t.TempDir()
	script := writeSh(t, dir, "nil_stdin.sh", `printf 'done\n'`)

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  script,
		WorkDir: dir,
	}

	result, err := runner.Run(context.Background(), cfg, "", nil, nil)
	if err != nil {
		t.Fatalf("Run with nil stdin: %v", err)
	}
	if !strings.Contains(result, "done") {
		t.Errorf("expected 'done' in result, got: %q", result)
	}
}

// TestSubprocessRunner_StdinForwarding verifies that stdin channel input is
// forwarded to the subprocess's stdin pipe.
func TestSubprocessRunner_StdinForwarding(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}

	dir := t.TempDir()
	// Read a line from stdin and echo it back.
	script := writeSh(t, dir, "stdin.sh", `read line; printf 'got: %s\n' "$line"`)

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  script,
		WorkDir: dir,
	}

	stdinCh := make(chan string, 1)
	stdinCh <- "hello from stdin"
	close(stdinCh)

	result, err := runner.Run(context.Background(), cfg, "", stdinCh, nil)
	if err != nil {
		t.Fatalf("Run with stdin: %v", err)
	}
	if !strings.Contains(result, "hello from stdin") {
		t.Errorf("expected stdin forwarded, got: %q", result)
	}
}

// TestSubprocessRunner_BinaryNotFound verifies that a clear error is returned
// when the binary cannot be found on PATH.
func TestSubprocessRunner_BinaryNotFound(t *testing.T) {
	t.Parallel()

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  "definitely-not-a-real-binary-xyz-abc-123",
		WorkDir: t.TempDir(),
	}

	_, err := runner.Run(context.Background(), cfg, "do something", nil, nil)
	if err == nil {
		t.Fatal("expected error for binary not found, got nil")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// TestSubprocessRunner_ContextCancellation verifies that context cancellation
// sends SIGTERM to the subprocess and returns.
func TestSubprocessRunner_ContextCancellation(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM not supported on windows")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  "sleep",
		WorkDir: t.TempDir(),
		Args:    []string{"60"}, // would run for 60 seconds without cancellation
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		_, runErr = runner.Run(ctx, cfg, "", nil, nil)
	}()

	// Cancel after a short delay.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}

	if runErr == nil {
		t.Error("expected error after context cancellation, got nil")
	}
}

// TestSubprocessRunner_Timeout verifies that a short timeout terminates the subprocess.
func TestSubprocessRunner_Timeout(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM not supported on windows")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available")
	}

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  "sleep",
		WorkDir: t.TempDir(),
		Args:    []string{"60"},
		Timeout: 200 * time.Millisecond,
	}

	start := time.Now()
	_, err := runner.Run(context.Background(), cfg, "", nil, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected error after timeout, got nil")
	}
	// Should have terminated well before the 60-second sleep.
	if elapsed > 15*time.Second {
		t.Errorf("expected timeout within 15s, took %v", elapsed)
	}
}
