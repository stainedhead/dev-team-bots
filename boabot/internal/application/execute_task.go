package application

import (
	"context"
	"fmt"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

type ExecuteTaskUseCase struct {
	provider      domain.ModelProvider
	mcp           domain.MCPClient
	memory        domain.MemoryStore
	embedder      domain.Embedder
	vectors       domain.VectorStore
	soulPrompt    string
	budgetTracker domain.BudgetTracker
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

// WithBudgetTracker wires a BudgetTracker into the use case.  When set, every
// model invocation is gated by CheckAndRecordToolCall before the call and
// CheckAndRecordTokens after, using the actual token counts from the response.
func (u *ExecuteTaskUseCase) WithBudgetTracker(bt domain.BudgetTracker) {
	u.budgetTracker = bt
}

func (u *ExecuteTaskUseCase) Execute(ctx context.Context, task domain.Task) (domain.TaskResult, error) {
	if u.budgetTracker != nil {
		if err := u.budgetTracker.CheckAndRecordToolCall(ctx); err != nil {
			return domain.TaskResult{TaskID: task.ID}, fmt.Errorf("budget tool call gate: %w", err)
		}
	}

	msgCtx, err := u.buildContext(ctx, task)
	if err != nil {
		return domain.TaskResult{TaskID: task.ID}, fmt.Errorf("build context: %w", err)
	}

	resp, err := u.provider.Invoke(ctx, domain.InvokeRequest{
		SystemPrompt: u.soulPrompt,
		Messages: []domain.ProviderMessage{
			{Role: "user", Content: msgCtx},
		},
	})
	if err != nil {
		return domain.TaskResult{TaskID: task.ID, Err: err}, fmt.Errorf("model invoke: %w", err)
	}

	if u.budgetTracker != nil {
		total := int64(resp.Usage.InputTokens + resp.Usage.OutputTokens)
		if err := u.budgetTracker.CheckAndRecordTokens(ctx, total); err != nil {
			return domain.TaskResult{TaskID: task.ID}, fmt.Errorf("budget token gate: %w", err)
		}
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

	result := task.Instruction + "\n\n--- Relevant memory ---\n"
	for _, r := range results {
		if data, err := u.memory.Read(ctx, r.Key); err == nil {
			result += string(data) + "\n"
		}
	}
	return result, nil
}
