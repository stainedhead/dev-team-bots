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
