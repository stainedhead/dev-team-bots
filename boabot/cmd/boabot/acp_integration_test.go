//go:build integration

package main

// T7 (specs/260813-boabot-acp-stdio-harness-support/tasks.md): a real
// end-to-end test of boabot's ACP mode -- it builds the actual boabot
// binary, spawns it as a real OS subprocess with `-acp`, and drives it over
// real stdio pipes using coder/acp-go-sdk's own client-side connection
// (the same machinery a real ACP host uses).
//
// What this does NOT verify: the real /Applications/Buzz.app/Contents/MacOS/
// buzz-acp binary itself. buzz-acp has no dry-run/local mode -- every one of
// its flags assumes a live Buzz relay connection (it authenticates to the
// relay and is driven entirely by relay-delivered events; there is no way
// to make it dispatch a session/prompt without one). Standing up a real (or
// faithfully mocked) Nostr relay with NIP-42 auth was judged out of scope
// for this pass -- see implementation-notes.md. What IS verified here is
// the actual wire-protocol contract a real buzz-acp process would rely on:
// initialize, session/new, and a full session/prompt turn (including the
// keep-alive mechanism) against the real compiled binary over a real
// process boundary, substituting a local mock OpenAI-compatible HTTP server
// for the model provider so the test needs no network access or API key.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	sdk "github.com/coder/acp-go-sdk"
)

// testACPClient implements sdk.Client, recording every session/update it
// receives so tests can assert on keep-alive behavior.
type testACPClient struct {
	mu      sync.Mutex
	updates []sdk.SessionNotification
}

func (c *testACPClient) SessionUpdate(_ context.Context, n sdk.SessionNotification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, n)
	return nil
}

func (c *testACPClient) snapshot() []sdk.SessionNotification {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]sdk.SessionNotification, len(c.updates))
	copy(out, c.updates)
	return out
}

func (c *testACPClient) RequestPermission(_ context.Context, params sdk.RequestPermissionRequest) (sdk.RequestPermissionResponse, error) {
	if len(params.Options) > 0 {
		return sdk.RequestPermissionResponse{Outcome: sdk.RequestPermissionOutcome{
			Selected: &sdk.RequestPermissionOutcomeSelected{OptionId: params.Options[0].OptionId},
		}}, nil
	}
	return sdk.RequestPermissionResponse{Outcome: sdk.RequestPermissionOutcome{Cancelled: &sdk.RequestPermissionOutcomeCancelled{}}}, nil
}

func (c *testACPClient) WriteTextFile(_ context.Context, _ sdk.WriteTextFileRequest) (sdk.WriteTextFileResponse, error) {
	return sdk.WriteTextFileResponse{}, nil
}
func (c *testACPClient) ReadTextFile(_ context.Context, _ sdk.ReadTextFileRequest) (sdk.ReadTextFileResponse, error) {
	return sdk.ReadTextFileResponse{}, nil
}
func (c *testACPClient) CreateTerminal(_ context.Context, _ sdk.CreateTerminalRequest) (sdk.CreateTerminalResponse, error) {
	return sdk.CreateTerminalResponse{}, nil
}
func (c *testACPClient) KillTerminal(_ context.Context, _ sdk.KillTerminalRequest) (sdk.KillTerminalResponse, error) {
	return sdk.KillTerminalResponse{}, nil
}
func (c *testACPClient) TerminalOutput(_ context.Context, _ sdk.TerminalOutputRequest) (sdk.TerminalOutputResponse, error) {
	return sdk.TerminalOutputResponse{}, nil
}
func (c *testACPClient) ReleaseTerminal(_ context.Context, _ sdk.ReleaseTerminalRequest) (sdk.ReleaseTerminalResponse, error) {
	return sdk.ReleaseTerminalResponse{}, nil
}
func (c *testACPClient) WaitForTerminalExit(_ context.Context, _ sdk.WaitForTerminalExitRequest) (sdk.WaitForTerminalExitResponse, error) {
	return sdk.WaitForTerminalExitResponse{}, nil
}

var _ sdk.Client = (*testACPClient)(nil)

// buildBoabotBinary compiles the real boabot binary once for this test file.
func buildBoabotBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "boabot")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build boabot: %v\n%s", err, out)
	}
	return bin
}

// mockOpenAIServer returns a minimal OpenAI-compatible /chat/completions
// endpoint. delay simulates a slow model response, to exercise the
// keep-alive mechanism.
func mockOpenAIServer(t *testing.T, content string, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": content}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
}

// writePersona writes a minimal persona config.yaml + SOUL.md pointing at
// the mock OpenAI server, returning the config.yaml path.
func writePersona(t *testing.T, endpoint string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("You are a test persona."), 0o600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}
	yaml := "bot:\n  name: acp-it-bot\n  type: acp-it-bot\nmodels:\n  default: mock\n  providers:\n    - name: mock\n      type: openai\n      model_id: mock-model\n      endpoint: " + endpoint + "\n"
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return configPath
}

func startACPSubprocess(t *testing.T, bin, configPath string, keepAlive time.Duration) (*testACPClient, *sdk.ClientSideConnection, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "-acp", "-config", configPath)
	cmd.Env = append(os.Environ(), "BOABOT_ACP_KEEPALIVE_INTERVAL="+keepAlive.String())
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start boabot -acp: %v", err)
	}

	client := &testACPClient{}
	conn := sdk.NewClientSideConnection(client, stdin, stdout)

	cleanup := func() {
		cancel()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	return client, conn, cleanup
}

func TestACPIntegration_FullTurn_AgainstRealBinary(t *testing.T) {
	bin := buildBoabotBinary(t)
	server := mockOpenAIServer(t, "the answer is 42", 0)
	defer server.Close()
	configPath := writePersona(t, server.URL)

	client, conn, cleanup := startACPSubprocess(t, bin, configPath, time.Second)
	defer cleanup()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelCtx()

	initResp, err := conn.Initialize(ctx, sdk.InitializeRequest{ProtocolVersion: sdk.ProtocolVersionNumber})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if initResp.ProtocolVersion != sdk.ProtocolVersionNumber {
		t.Errorf("ProtocolVersion = %v, want %v", initResp.ProtocolVersion, sdk.ProtocolVersionNumber)
	}

	sess, err := conn.NewSession(ctx, sdk.NewSessionRequest{Cwd: os.TempDir(), McpServers: []sdk.McpServer{}})
	if err != nil {
		t.Fatalf("session/new: %v", err)
	}
	if sess.SessionId == "" {
		t.Fatal("empty SessionId")
	}

	promptResp, err := conn.Prompt(ctx, sdk.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []sdk.ContentBlock{sdk.TextBlock("what is the answer?")},
	})
	if err != nil {
		t.Fatalf("session/prompt: %v", err)
	}
	if promptResp.StopReason != sdk.StopReasonEndTurn {
		t.Errorf("StopReason = %v, want %v", promptResp.StopReason, sdk.StopReasonEndTurn)
	}

	found := false
	for _, u := range client.snapshot() {
		if u.Update.AgentMessageChunk != nil && u.Update.AgentMessageChunk.Content.Text != nil &&
			u.Update.AgentMessageChunk.Content.Text.Text == "the answer is 42" {
			found = true
		}
	}
	if !found {
		t.Error("no session/update carried the final output text over the real subprocess boundary")
	}
}

func TestACPIntegration_SlowTurn_KeepAliveFiresOverRealSubprocess(t *testing.T) {
	bin := buildBoabotBinary(t)
	// Slower than the keep-alive interval below, so at least one keep-alive
	// update must arrive before the final response -- this is the actual
	// idle-timeout-compatibility mechanism buzz-acp depends on, proven here
	// over a real process boundary rather than an in-process fake.
	server := mockOpenAIServer(t, "done after a while", 300*time.Millisecond)
	defer server.Close()
	configPath := writePersona(t, server.URL)

	client, conn, cleanup := startACPSubprocess(t, bin, configPath, 50*time.Millisecond)
	defer cleanup()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelCtx()

	if _, err := conn.Initialize(ctx, sdk.InitializeRequest{ProtocolVersion: sdk.ProtocolVersionNumber}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	sess, err := conn.NewSession(ctx, sdk.NewSessionRequest{Cwd: os.TempDir(), McpServers: []sdk.McpServer{}})
	if err != nil {
		t.Fatalf("session/new: %v", err)
	}

	if _, err := conn.Prompt(ctx, sdk.PromptRequest{
		SessionId: sess.SessionId,
		Prompt:    []sdk.ContentBlock{sdk.TextBlock("slow question")},
	}); err != nil {
		t.Fatalf("session/prompt: %v", err)
	}

	thoughtCount := 0
	for _, u := range client.snapshot() {
		if u.Update.AgentThoughtChunk != nil {
			thoughtCount++
		}
	}
	if thoughtCount == 0 {
		t.Error("no acp::thought keep-alive updates arrived during a turn slower than the keep-alive interval " +
			"-- a real buzz-acp host would have killed this turn on --idle-timeout")
	}
}
