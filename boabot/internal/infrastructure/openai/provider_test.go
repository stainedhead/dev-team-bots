package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/openai"
)

func TestNewProvider_MissingEndpoint(t *testing.T) {
	_, err := openai.NewProvider("", "qwen3:latest")
	if err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}

func TestNewProvider_MissingModelID(t *testing.T) {
	_, err := openai.NewProvider("http://localhost:11434/v1", "")
	if err == nil {
		t.Fatal("expected error for empty model_id")
	}
}

func TestProvider_Invoke_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected /chat/completions, got %s", r.URL.Path)
		}

		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			MaxTokens   int     `json:"max_tokens"`
			Temperature float32 `json:"temperature"`
			Stream      bool    `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "qwen3:latest" {
			t.Errorf("expected model qwen3:latest, got %s", body.Model)
		}
		if body.Stream {
			t.Error("stream must be false")
		}
		// system + user messages expected
		if len(body.Messages) < 2 {
			t.Errorf("expected at least 2 messages, got %d", len(body.Messages))
		}
		if body.Messages[0].Role != "system" {
			t.Errorf("first message role: want system, got %s", body.Messages[0].Role)
		}

		resp := map[string]any{
			"id":     "test-id",
			"object": "chat.completion",
			"model":  "qwen3:latest",
			"choices": []map[string]any{
				{
					"index":         0,
					"finish_reason": "stop",
					"message":       map[string]string{"role": "assistant", "content": "hello from qwen"},
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     50,
				"completion_tokens": 10,
				"total_tokens":      60,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, err := openai.NewProvider(srv.URL, "qwen3:latest")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	resp, err := p.Invoke(context.Background(), domain.InvokeRequest{
		SystemPrompt: "you are helpful",
		Messages:     []domain.ProviderMessage{{Role: "user", Content: "hi"}},
		MaxTokens:    512,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Content != "hello from qwen" {
		t.Errorf("want 'hello from qwen', got %q", resp.Content)
	}
	if resp.StopReason != "stop" {
		t.Errorf("want stop reason 'stop', got %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 50 || resp.Usage.OutputTokens != 10 {
		t.Errorf("usage: want 50/10, got %d/%d", resp.Usage.InputTokens, resp.Usage.OutputTokens)
	}
}

func TestProvider_Invoke_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "overloaded", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p, _ := openai.NewProvider(srv.URL, "qwen3:latest")
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on server 503")
	}
}

func TestProvider_Invoke_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"choices": []any{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := openai.NewProvider(srv.URL, "qwen3:latest")
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestProvider_Invoke_ThinkingModel_FallsBackToReasoningContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"finish_reason": "stop",
					"message": map[string]any{
						"role":              "assistant",
						"content":           "",
						"reasoning_content": "I reasoned through this carefully.",
					},
				},
			},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := openai.NewProvider(srv.URL, "qwen3:latest")
	resp, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "think about this"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "I reasoned through this carefully." {
		t.Errorf("expected reasoning_content fallback, got %q", resp.Content)
	}
}

func TestProvider_Invoke_ThinkingModel_StripsThinkTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"finish_reason": "stop",
					"message": map[string]any{
						"role":    "assistant",
						"content": "<think>internal reasoning here</think>\nThe answer is 42.",
					},
				},
			},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := openai.NewProvider(srv.URL, "qwen3:latest")
	resp, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "answer"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "The answer is 42." {
		t.Errorf("expected think tags stripped, got %q", resp.Content)
	}
}

func TestProvider_Invoke_SendsToolDefinitions(t *testing.T) {
	var gotTools []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []map[string]any `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTools = body.Tools

		resp := map[string]any{
			"choices": []map[string]any{
				{"finish_reason": "stop", "message": map[string]string{"role": "assistant", "content": "done"}},
			},
			"usage": map[string]int{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := openai.NewProvider(srv.URL, "qwen3:latest")
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "use a tool"}},
		Tools: []domain.MCPTool{
			{Name: "read_file", Description: "reads a file", InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotTools) != 1 {
		t.Fatalf("expected 1 tool sent, got %d", len(gotTools))
	}
	if gotTools[0]["type"] != "function" {
		t.Errorf("expected tool type 'function', got %v", gotTools[0]["type"])
	}
	fn, _ := gotTools[0]["function"].(map[string]any)
	if fn["name"] != "read_file" {
		t.Errorf("expected tool name 'read_file', got %v", fn["name"])
	}
}

func TestProvider_Invoke_ParsesToolCallsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"finish_reason": "tool_calls",
					"message": map[string]any{
						"role":    "assistant",
						"content": nil,
						"tool_calls": []map[string]any{
							{
								"id":   "call_abc123",
								"type": "function",
								"function": map[string]any{
									"name":      "read_file",
									"arguments": `{"path":"/tmp/foo.txt"}`,
								},
							},
						},
					},
				},
			},
			"usage": map[string]int{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := openai.NewProvider(srv.URL, "qwen3:latest")
	resp, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "read a file"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc123" {
		t.Errorf("expected tool call ID 'call_abc123', got %q", tc.ID)
	}
	if tc.Name != "read_file" {
		t.Errorf("expected tool name 'read_file', got %q", tc.Name)
	}
	if tc.Args["path"] != "/tmp/foo.txt" {
		t.Errorf("expected path arg '/tmp/foo.txt', got %v", tc.Args["path"])
	}
	if resp.StopReason != "tool_calls" {
		t.Errorf("expected stop reason 'tool_calls', got %q", resp.StopReason)
	}
}

func TestProvider_Invoke_ToolResultMessageFormatting(t *testing.T) {
	var gotMessages []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotMessages = body.Messages

		resp := map[string]any{
			"choices": []map[string]any{
				{"finish_reason": "stop", "message": map[string]string{"role": "assistant", "content": "done"}},
			},
			"usage": map[string]int{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := openai.NewProvider(srv.URL, "qwen3:latest")
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{
			{Role: "user", Content: "read a file"},
			{Role: "assistant", ToolCalls: []domain.ToolCall{
				{ID: "call_1", Name: "read_file", Args: map[string]any{"path": "/tmp/f.txt"}},
			}},
			{Role: "tool", ToolCallID: "call_1", ToolName: "read_file", Content: "file contents"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect 3 messages: user, assistant-with-tool-calls, tool-result
	if len(gotMessages) != 3 {
		t.Fatalf("expected 3 messages, got %d: %v", len(gotMessages), gotMessages)
	}
	// Tool result message must have role=tool and tool_call_id
	toolMsg := gotMessages[2]
	if toolMsg["role"] != "tool" {
		t.Errorf("expected role 'tool', got %v", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_1" {
		t.Errorf("expected tool_call_id 'call_1', got %v", toolMsg["tool_call_id"])
	}
	if toolMsg["content"] != "file contents" {
		t.Errorf("expected content 'file contents', got %v", toolMsg["content"])
	}
	// Assistant message must have tool_calls array
	assistMsg := gotMessages[1]
	if assistMsg["role"] != "assistant" {
		t.Errorf("expected role 'assistant', got %v", assistMsg["role"])
	}
	toolCalls, ok := assistMsg["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Errorf("expected assistant message with 1 tool_call, got %v", assistMsg["tool_calls"])
	}
}

func TestProvider_Invoke_NoToolsNoToolsField(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		resp := map[string]any{
			"choices": []map[string]any{
				{"finish_reason": "stop", "message": map[string]string{"role": "assistant", "content": "ok"}},
			},
			"usage": map[string]int{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := openai.NewProvider(srv.URL, "qwen3:latest")
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "hi"}},
		// No Tools field
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, exists := gotBody["tools"]; exists {
		t.Error("expected no 'tools' field when no tools provided")
	}
}

func TestProvider_Invoke_ThinkingModel_OnlyThinkBlock_FallsBackToReasoning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{
				{
					"finish_reason": "stop",
					"message": map[string]any{
						"role":              "assistant",
						"content":           "<think>only thinking, no final answer</think>",
						"reasoning_content": "only thinking, no final answer",
					},
				},
			},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, _ := openai.NewProvider(srv.URL, "qwen3:latest")
	resp, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "think"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content == "" {
		t.Error("expected non-empty content when only think block present")
	}
}
