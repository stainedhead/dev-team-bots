package domain

import "context"

type ModelProvider interface {
	Invoke(ctx context.Context, req InvokeRequest) (InvokeResponse, error)
}

type ProviderFactory interface {
	Get(name string) (ModelProvider, error)
}

// ToolCall represents a single tool invocation requested by the model.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

type InvokeRequest struct {
	SystemPrompt string
	Messages     []ProviderMessage
	Tools        []MCPTool // tool definitions available to the model; nil means no tool use
	MaxTokens    int
	Temperature  float32
}

// ProviderMessage is a single turn in the conversation.
// For role "assistant" with tool calls, set ToolCalls and leave Content empty.
// For role "tool", set ToolCallID, ToolName, and Content with the result.
type ProviderMessage struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall // non-nil when role == "assistant" and model requested tool calls
	ToolCallID string     // non-empty when role == "tool"
	ToolName   string     // non-empty when role == "tool"
}

type InvokeResponse struct {
	Content    string
	ToolCalls  []ToolCall // non-empty when the model wants to call tools before responding
	StopReason string
	Usage      TokenUsage
}

type TokenUsage struct {
	InputTokens  int
	OutputTokens int
}
