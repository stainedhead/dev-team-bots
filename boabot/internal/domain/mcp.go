package domain

import "context"

type MCPClient interface {
	ListTools(ctx context.Context) ([]MCPTool, error)
	CallTool(ctx context.Context, name string, args map[string]any) (MCPToolResult, error)
}

type MCPTool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

type MCPToolResult struct {
	Content []MCPContent
	IsError bool
}

type MCPContent struct {
	Type string
	Text string
}
