package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

var thinkTagRE = regexp.MustCompile(`(?s)<think>.*?</think>`)

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
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	bodyMap := map[string]any{
		"model":       p.modelID,
		"messages":    buildMessages(req),
		"max_tokens":  maxTokens,
		"temperature": req.Temperature,
		"stream":      false,
	}
	if len(req.Tools) > 0 {
		bodyMap["tools"] = buildToolDefinitions(req.Tools)
		bodyMap["tool_choice"] = "auto"
	}

	body, err := json.Marshal(bodyMap)
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
				Content          string        `json:"content"`
				ReasoningContent string        `json:"reasoning_content"` // qwen3/deepseek thinking models
				ToolCalls        []rawToolCall `json:"tool_calls"`
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

	choice := result.Choices[0]
	toolCalls := parseToolCalls(choice.Message.ToolCalls)
	content := extractContent(choice.Message.Content, choice.Message.ReasoningContent)

	return domain.InvokeResponse{
		Content:    content,
		ToolCalls:  toolCalls,
		StopReason: choice.FinishReason,
		Usage: domain.TokenUsage{
			InputTokens:  result.Usage.PromptTokens,
			OutputTokens: result.Usage.CompletionTokens,
		},
	}, nil
}

// rawToolCall is the wire format returned by the OpenAI API.
type rawToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON-encoded string
	} `json:"function"`
}

// parseToolCalls converts raw OpenAI tool calls to domain.ToolCall values.
func parseToolCalls(raw []rawToolCall) []domain.ToolCall {
	if len(raw) == 0 {
		return nil
	}
	out := make([]domain.ToolCall, 0, len(raw))
	for _, tc := range raw {
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		out = append(out, domain.ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: args,
		})
	}
	return out
}

// buildToolDefinitions converts domain.MCPTool values to the OpenAI tool format.
func buildToolDefinitions(tools []domain.MCPTool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			},
		})
	}
	return out
}

// buildMessages converts domain.ProviderMessage values to the OpenAI wire format.
// Assistant messages with ToolCalls are serialised with a tool_calls array.
// Tool result messages (role "tool") include tool_call_id and content.
func buildMessages(req domain.InvokeRequest) []map[string]any {
	msgs := make([]map[string]any, 0, len(req.Messages)+1)
	if req.SystemPrompt != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": req.SystemPrompt})
	}
	for _, m := range req.Messages {
		switch {
		case m.Role == "assistant" && len(m.ToolCalls) > 0:
			rawCalls := make([]map[string]any, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Args)
				rawCalls = append(rawCalls, map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": string(argsJSON),
					},
				})
			}
			msgs = append(msgs, map[string]any{
				"role":       "assistant",
				"content":    nil,
				"tool_calls": rawCalls,
			})
		case m.Role == "tool":
			msgs = append(msgs, map[string]any{
				"role":         "tool",
				"tool_call_id": m.ToolCallID,
				"content":      m.Content,
			})
		default:
			msgs = append(msgs, map[string]any{"role": m.Role, "content": m.Content})
		}
	}
	return msgs
}

// extractContent strips <think> blocks from content and falls back to
// reasoningContent when the visible content is empty (thinking-only responses).
func extractContent(content, reasoningContent string) string {
	stripped := strings.TrimSpace(thinkTagRE.ReplaceAllString(content, ""))
	if stripped != "" {
		return stripped
	}
	if reasoningContent != "" {
		return reasoningContent
	}
	return content
}
