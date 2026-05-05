package domain

import "context"

// Tool is a harness tool or MCP tool available for injection into the model context.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ToolStub is a compact name-and-summary representation injected when a tool
// scores below the full-injection threshold.
type ToolStub struct {
	Name        string
	Description string
}

// ScoredTool pairs a Tool with its relevance score for a given query.
type ScoredTool struct {
	Tool  Tool
	Score float64
}

// ToolScorer scores tools against a task intent string.
// The BM25 implementation is the default; swappable to neural embeddings.
type ToolScorer interface {
	Score(query string, tools []Tool) []ScoredTool
}

// ToolGater selects which tools receive full schema injection for a given task.
// Top-k tools get full schemas; remaining tools get stubs. Hard cap: 20 full schemas.
type ToolGater interface {
	Select(ctx context.Context, intent string, all []Tool) (full []Tool, stubs []ToolStub, err error)
}
