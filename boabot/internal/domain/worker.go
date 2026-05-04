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
}

type TaskResult struct {
	TaskID  string
	Output  string
	Success bool
	Err     error
}

// WorkerFactory creates workers pre-wired with the bot's model provider and MCP client.
type WorkerFactory interface {
	New() Worker
}
