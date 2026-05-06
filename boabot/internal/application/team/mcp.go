package team

import (
	"context"
	"fmt"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// noopMCPClient is a stub MCP client used until MCP integration is wired (M5+).
type noopMCPClient struct{}

func (n *noopMCPClient) ListTools(_ context.Context) ([]domain.MCPTool, error) {
	return nil, nil
}

func (n *noopMCPClient) CallTool(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
	return domain.MCPToolResult{}, fmt.Errorf("MCP not configured")
}
