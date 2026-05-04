package application

import (
	"context"
	"fmt"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

type ExecuteTaskUseCase struct {
	provider  domain.ModelProvider
	mcp       domain.MCPClient
	memory    domain.MemoryStore
	embedder  domain.Embedder
	vectors   domain.VectorStore
	soulPrompt string
}

func NewExecuteTaskUseCase(
	provider domain.ModelProvider,
	mcp domain.MCPClient,
	memory domain.MemoryStore,
	embedder domain.Embedder,
	vectors domain.VectorStore,
	soulPrompt string,
) *ExecuteTaskUseCase {
	return &ExecuteTaskUseCase{
		provider:   provider,
		mcp:        mcp,
		memory:     memory,
		embedder:   embedder,
		vectors:    vectors,
		soulPrompt: soulPrompt,
	}
}

func (u *ExecuteTaskUseCase) Execute(ctx context.Context, task domain.Task) (domain.TaskResult, error) {
	context, err := u.buildContext(ctx, task)
	if err != nil {
		return domain.TaskResult{TaskID: task.ID}, fmt.Errorf("build context: %w", err)
	}

	resp, err := u.provider.Invoke(ctx, domain.InvokeRequest{
		SystemPrompt: u.soulPrompt,
		Messages: []domain.ProviderMessage{
			{Role: "user", Content: context},
		},
	})
	if err != nil {
		return domain.TaskResult{TaskID: task.ID, Err: err}, fmt.Errorf("model invoke: %w", err)
	}

	return domain.TaskResult{
		TaskID:  task.ID,
		Output:  resp.Content,
		Success: true,
	}, nil
}

func (u *ExecuteTaskUseCase) buildContext(ctx context.Context, task domain.Task) (string, error) {
	embedding, err := u.embedder.Embed(ctx, task.Instruction)
	if err != nil {
		return task.Instruction, nil // gracefully degrade if embedding fails
	}

	results, err := u.vectors.Search(ctx, embedding, 5)
	if err != nil || len(results) == 0 {
		return task.Instruction, nil
	}

	context := task.Instruction + "\n\n--- Relevant memory ---\n"
	for _, r := range results {
		if data, err := u.memory.Read(ctx, r.Key); err == nil {
			context += string(data) + "\n"
		}
	}
	return context, nil
}
