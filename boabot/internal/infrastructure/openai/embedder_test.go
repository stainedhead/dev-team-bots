package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/openai"
)

func TestNewEmbedder_MissingEndpoint(t *testing.T) {
	_, err := openai.NewEmbedder("", "nomic-embed-text:v1.5")
	if err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}

func TestNewEmbedder_MissingModelID(t *testing.T) {
	_, err := openai.NewEmbedder("http://localhost:11434/v1", "")
	if err == nil {
		t.Fatal("expected error for empty model_id")
	}
}

func TestEmbedder_Embed_HappyPath(t *testing.T) {
	want := []float32{0.1, 0.2, 0.3}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/embeddings" {
			t.Errorf("expected /embeddings path, got %s", r.URL.Path)
		}
		var body struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "nomic-embed-text:v1.5" {
			t.Errorf("expected model nomic-embed-text:v1.5, got %s", body.Model)
		}
		if body.Input != "hello world" {
			t.Errorf("expected input 'hello world', got %s", body.Input)
		}
		resp := map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"object": "embedding", "index": 0, "embedding": want},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e, err := openai.NewEmbedder(srv.URL, "nomic-embed-text:v1.5")
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	got, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d floats, got %d", len(want), len(got))
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("index %d: want %f, got %f", i, v, got[i])
		}
	}
}

func TestEmbedder_Embed_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	e, _ := openai.NewEmbedder(srv.URL, "nomic-embed-text:v1.5")
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error on server 500")
	}
}

func TestEmbedder_Embed_EmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"object": "list", "data": []any{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e, _ := openai.NewEmbedder(srv.URL, "nomic-embed-text:v1.5")
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for empty data array")
	}
}
