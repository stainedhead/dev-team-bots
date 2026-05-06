// Package openai provides adapters for OpenAI-compatible HTTP APIs, including
// Ollama's local embedding endpoint.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Embedder calls an OpenAI-compatible embeddings endpoint (e.g. Ollama at
// http://localhost:11434/v1) and implements domain.Embedder.
type Embedder struct {
	endpoint string
	modelID  string
	client   *http.Client
}

// NewEmbedder creates an Embedder that posts to <endpoint>/embeddings.
// endpoint should be the base URL (e.g. "http://localhost:11434/v1").
func NewEmbedder(endpoint, modelID string) (*Embedder, error) {
	if endpoint == "" {
		return nil, errors.New("openai embedder: endpoint is required")
	}
	if modelID == "" {
		return nil, errors.New("openai embedder: model_id is required")
	}
	return &Embedder{
		endpoint: strings.TrimSuffix(endpoint, "/") + "/embeddings",
		modelID:  modelID,
		client:   &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Embed sends text to the embeddings endpoint and returns the result vector.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(map[string]string{
		"model": e.modelID,
		"input": text,
	})
	if err != nil {
		return nil, fmt.Errorf("openai embedder: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai embedder: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embedder: post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai embedder: server returned %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openai embedder: decode response: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, errors.New("openai embedder: response contained no embedding data")
	}
	return result.Data[0].Embedding, nil
}
