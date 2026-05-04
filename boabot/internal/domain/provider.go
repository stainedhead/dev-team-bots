package domain

import "context"

type ModelProvider interface {
	Invoke(ctx context.Context, req InvokeRequest) (InvokeResponse, error)
}

type ProviderFactory interface {
	Get(name string) (ModelProvider, error)
}

type InvokeRequest struct {
	SystemPrompt string
	Messages     []ProviderMessage
	MaxTokens    int
	Temperature  float32
}

type ProviderMessage struct {
	Role    string
	Content string
}

type InvokeResponse struct {
	Content    string
	StopReason string
	Usage      TokenUsage
}

type TokenUsage struct {
	InputTokens  int
	OutputTokens int
}
