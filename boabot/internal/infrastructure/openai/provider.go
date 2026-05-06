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

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// Provider implements domain.ModelProvider against any OpenAI-compatible chat
// completions endpoint (e.g. Ollama at http://localhost:11434/v1).
type Provider struct {
	endpoint string
	modelID  string
	client   *http.Client
}

// NewProvider creates a Provider that posts to <endpoint>/chat/completions.
func NewProvider(endpoint, modelID string) (*Provider, error) {
	if endpoint == "" {
		return nil, errors.New("openai provider: endpoint is required")
	}
	if modelID == "" {
		return nil, errors.New("openai provider: model_id is required")
	}
	return &Provider{
		endpoint: strings.TrimSuffix(endpoint, "/") + "/chat/completions",
		modelID:  modelID,
		client:   &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

// Invoke sends a chat completion request and returns the first choice.
func (p *Provider) Invoke(ctx context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
	messages := buildMessages(req)

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	body, err := json.Marshal(map[string]any{
		"model":       p.modelID,
		"messages":    messages,
		"max_tokens":  maxTokens,
		"temperature": req.Temperature,
		"stream":      false,
	})
	if err != nil {
		return domain.InvokeResponse{}, fmt.Errorf("openai provider: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return domain.InvokeResponse{}, fmt.Errorf("openai provider: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return domain.InvokeResponse{}, fmt.Errorf("openai provider: post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return domain.InvokeResponse{}, fmt.Errorf("openai provider: server returned %d", resp.StatusCode)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return domain.InvokeResponse{}, fmt.Errorf("openai provider: decode response: %w", err)
	}
	if len(result.Choices) == 0 {
		return domain.InvokeResponse{}, errors.New("openai provider: response contained no choices")
	}

	return domain.InvokeResponse{
		Content:    result.Choices[0].Message.Content,
		StopReason: result.Choices[0].FinishReason,
		Usage: domain.TokenUsage{
			InputTokens:  result.Usage.PromptTokens,
			OutputTokens: result.Usage.CompletionTokens,
		},
	}, nil
}

func buildMessages(req domain.InvokeRequest) []map[string]string {
	msgs := make([]map[string]string, 0, len(req.Messages)+1)
	if req.SystemPrompt != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": req.SystemPrompt})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, map[string]string{"role": m.Role, "content": m.Content})
	}
	return msgs
}
