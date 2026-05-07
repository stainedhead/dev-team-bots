package codeagent_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/codeagent"
)

// jsonLine serialises a value and appends a newline.
func jsonLine(v any) string {
	b, _ := json.Marshal(v)
	return string(b) + "\n"
}

// happyStreamOutput returns a valid claude stream-json sequence for the given
// assistant text and result string.
func happyStreamOutput(assistantText, result string) string {
	events := []any{
		map[string]any{"type": "message_start", "message": map[string]any{"role": "assistant"}},
		map[string]any{
			"type":  "content_block_delta",
			"delta": map[string]any{"type": "text_delta", "text": assistantText},
		},
		map[string]any{"type": "message_stop"},
		map[string]any{
			"type":   "result",
			"result": result,
			"usage":  map[string]any{"input_tokens": 10, "output_tokens": 5},
		},
	}
	var sb strings.Builder
	for _, e := range events {
		sb.WriteString(jsonLine(e))
	}
	return sb.String()
}

// writeOutputScript writes a shell script that cats a pre-written data file.
// The data is written to the temp dir alongside the script, so the script can
// reference it by an absolute path without needing shell escaping tricks.
func writeOutputScript(t *testing.T, output string) string {
	t.Helper()
	dir := t.TempDir()
	dataFile := filepath.Join(dir, "output.json")
	if err := os.WriteFile(dataFile, []byte(output), 0o600); err != nil {
		t.Fatalf("write data file: %v", err)
	}
	script := filepath.Join(dir, "claude")
	scriptBody := "#!/bin/sh\ncat " + dataFile + "\n"
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return script
}

// writeSleepScript writes a script that sleeps for a long time.
func writeSleepScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatalf("write sleep script: %v", err)
	}
	return script
}

// writeExitScript writes a script that prints to stderr and exits with code 1.
func writeExitScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	body := "#!/bin/sh\necho 'error output' >&2\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write exit script: %v", err)
	}
	return script
}

// makeProvider constructs a Provider pointing at the fake claude binary.
func makeProvider(t *testing.T, claudePath string, opts ...codeagent.Option) *codeagent.Provider {
	t.Helper()
	return codeagent.New(claudePath, t.TempDir(), opts...)
}

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

func TestProvider_HappyPath(t *testing.T) {
	assistantText := "I completed the task successfully."
	resultText := "Task done."
	output := happyStreamOutput(assistantText, resultText)

	script := writeOutputScript(t, output)
	p := makeProvider(t, script)

	resp, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "do the thing"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Content, assistantText) {
		t.Errorf("expected content to contain %q, got %q", assistantText, resp.Content)
	}
	if !strings.Contains(resp.Content, resultText) {
		t.Errorf("expected content to contain result %q, got %q", resultText, resp.Content)
	}
}

// ---------------------------------------------------------------------------
// Binary not found
// ---------------------------------------------------------------------------

func TestProvider_BinaryNotFound(t *testing.T) {
	p := codeagent.New("/no/such/binary/claude", t.TempDir())

	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "do something"}},
	})
	if err == nil {
		t.Fatal("expected error when binary is not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Non-zero exit code
// ---------------------------------------------------------------------------

func TestProvider_NonZeroExitCode(t *testing.T) {
	script := writeExitScript(t)
	p := makeProvider(t, script)

	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "fail"}},
	})
	if err == nil {
		t.Fatal("expected error on non-zero exit code")
	}
	if !strings.Contains(err.Error(), "exit") {
		t.Errorf("expected 'exit' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Timeout
// ---------------------------------------------------------------------------

func TestProvider_Timeout(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found")
	}

	script := writeSleepScript(t)
	p := makeProvider(t, script, codeagent.WithTimeout(100*time.Millisecond))

	ctx := context.Background()
	start := time.Now()
	_, err := p.Invoke(ctx, domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "hang"}},
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout took too long: %v (expected ~100ms)", elapsed)
	}
	// Accept any timeout/context/killed signal in the error message.
	msg := err.Error()
	if !strings.Contains(msg, "timed out") && !strings.Contains(msg, "deadline") &&
		!strings.Contains(msg, "context") && !strings.Contains(msg, "killed") &&
		!strings.Contains(msg, "signal") {
		t.Errorf("expected timeout-related error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Malformed JSON lines are skipped gracefully
// ---------------------------------------------------------------------------

func TestProvider_MalformedJSONSkipped(t *testing.T) {
	validDelta := map[string]any{
		"type":  "content_block_delta",
		"delta": map[string]any{"type": "text_delta", "text": "hello"},
	}
	output := "not json at all\n" + jsonLine(validDelta) + "also bad\n"

	script := writeOutputScript(t, output)
	p := makeProvider(t, script)

	resp, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Content, "hello") {
		t.Errorf("expected 'hello' in content, got %q", resp.Content)
	}
}

// ---------------------------------------------------------------------------
// System prompt and user messages are passed on the command line
// ---------------------------------------------------------------------------

func TestProvider_InstructionBuiltFromMessages(t *testing.T) {
	// A script that echoes all its args — we just confirm Invoke doesn't panic.
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"$@\"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	p := makeProvider(t, script)

	// Non-zero exit is acceptable here (echo returns 0 but no JSON); we only
	// verify the call completes without panicking.
	_, _ = p.Invoke(context.Background(), domain.InvokeRequest{
		SystemPrompt: "You are a helpful assistant.",
		Messages:     []domain.ProviderMessage{{Role: "user", Content: "what is 2+2?"}},
	})
}

// ---------------------------------------------------------------------------
// Codex dialect — plain text output
// ---------------------------------------------------------------------------

func TestProvider_CodexDialect_PlainText(t *testing.T) {
	// Write a fake codex binary that outputs plain text.
	dir := t.TempDir()
	dataFile := filepath.Join(dir, "output.txt")
	if err := os.WriteFile(dataFile, []byte("The refactoring is complete.\n"), 0o600); err != nil {
		t.Fatalf("write data file: %v", err)
	}
	script := filepath.Join(dir, "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat "+dataFile+"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	p := codeagent.New(script, t.TempDir(), codeagent.WithDialect(codeagent.DialectCodex))
	resp, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "refactor this"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Content, "The refactoring is complete.") {
		t.Errorf("expected plain-text content, got %q", resp.Content)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("expected stop_reason end_turn, got %q", resp.StopReason)
	}
}

func TestProvider_CodexDialect_BinaryNotFound(t *testing.T) {
	p := codeagent.New("/no/such/binary/codex", t.TempDir(), codeagent.WithDialect(codeagent.DialectCodex))
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "do something"}},
	})
	if err == nil {
		t.Fatal("expected error when codex binary is not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestProvider_ContextCancellation(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found")
	}

	script := writeSleepScript(t)
	p := makeProvider(t, script)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := p.Invoke(ctx, domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "wait forever"}},
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
	if elapsed > 5*time.Second {
		t.Errorf("cancellation took too long: %v", elapsed)
	}
}
