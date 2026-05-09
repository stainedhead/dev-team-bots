package cliagent_test

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/cliagent"
)

// requireSh returns the path to /bin/sh (or sh on PATH), skipping the test if unavailable.
func requireSh(t *testing.T) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	return sh
}

// TestSubprocessRunner_NormalCompletion verifies that the runner accumulates
// all stdout lines and returns them.
func TestSubprocessRunner_NormalCompletion(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}
	sh := requireSh(t)

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  sh,
		Args:    []string{"-c", "printf 'line1\\nline2\\nline3\\n'"},
		WorkDir: t.TempDir(),
	}

	result, err := runner.Run(context.Background(), cfg, "", nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(result, want) {
			t.Errorf("expected result to contain %q, got: %q", want, result)
		}
	}
}

// TestSubprocessRunner_EchoOutput tests normal completion using the echo command.
func TestSubprocessRunner_EchoOutput(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}
	sh := requireSh(t)

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  sh,
		Args:    []string{"-c", "printf 'hello\\n'"},
		WorkDir: t.TempDir(),
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
	sh := requireSh(t)

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  sh,
		Args:    []string{"-c", "printf 'alpha\\nbeta\\ngamma\\n'"},
		WorkDir: t.TempDir(),
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
	sh := requireSh(t)

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  sh,
		Args:    []string{"-c", "printf 'done\\n'"},
		WorkDir: t.TempDir(),
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
	sh := requireSh(t)

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  sh,
		Args:    []string{"-c", "read line; printf 'got: %s\\n' \"$line\""},
		WorkDir: t.TempDir(),
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

// TestSubprocessRunner_ContextCancelledWhileForwardingStdin verifies that when the
// context is cancelled while a subprocess is blocking on stdin, drainStdin closes
// the pipe and Run returns promptly.
func TestSubprocessRunner_ContextCancelledWhileForwardingStdin(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM not supported on windows")
	}
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat not available")
	}

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  "cat", // blocks indefinitely reading from stdin
		WorkDir: t.TempDir(),
	}

	stdinCh := make(chan string) // open channel; drainStdin will block on it

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		_, runErr = runner.Run(ctx, cfg, "", stdinCh, nil)
	}()

	// Cancel after a short delay while cat is blocking on stdin.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good — Run returned.
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s after context cancellation while forwarding stdin")
	}

	if runErr == nil {
		t.Error("expected error after context cancellation, got nil")
	}
}

// TestSubprocessRunner_NonZeroExitIncludesStderr verifies that when a subprocess
// exits non-zero and writes to stderr, the error string contains the stderr text.
func TestSubprocessRunner_NonZeroExitIncludesStderr(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}
	sh := requireSh(t)

	runner := cliagent.New()
	cfg := domain.CLIAgentConfig{
		Binary:  sh,
		Args:    []string{"-c", "printf 'fail detail from stderr\\n' >&2; exit 1"},
		WorkDir: t.TempDir(),
	}

	_, err := runner.Run(context.Background(), cfg, "", nil, nil)
	if err == nil {
		t.Fatal("expected error for non-zero exit, got nil")
	}
	if !strings.Contains(err.Error(), "fail detail from stderr") {
		t.Errorf("expected stderr text in error string, got: %v", err)
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
