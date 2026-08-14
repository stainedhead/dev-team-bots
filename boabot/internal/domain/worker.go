package domain

import "context"

type Worker interface {
	Execute(ctx context.Context, task Task) (TaskResult, error)
}

type Task struct {
	ID          string
	BoardItemID string
	Instruction string
	Source      string
	WorkDir     string // optional; directory the bot should work in
}

type TaskResult struct {
	TaskID  string
	Output  string
	Success bool
	Err     error
	// ToolCalls records every tool call executed while producing Output, in
	// execution order. Lets a caller (e.g. the ACP adapter) detect whether a
	// specific tool was actually invoked during the turn, rather than only
	// having the final text.
	ToolCalls []ToolCall
}

// WorkerFactory creates workers pre-wired with the bot's model provider and MCP client.
type WorkerFactory interface {
	New() Worker
}
