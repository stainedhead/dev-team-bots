package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func newExecuteTaskUseCase(
	provider domain.ModelProvider,
	mcp domain.MCPClient,
	memory domain.MemoryStore,
	embedder domain.Embedder,
	vectors domain.VectorStore,
) *application.ExecuteTaskUseCase {
	return application.NewExecuteTaskUseCase(provider, mcp, memory, embedder, vectors, "system-prompt")
}

func TestExecuteTask_Success(t *testing.T) {
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
			return domain.InvokeResponse{Content: "task done", StopReason: "end_turn"}, nil
		},
	}
	embedder := &mocks.Embedder{
		EmbedFn: func(_ context.Context, _ string) ([]float32, error) {
			return []float32{0.1, 0.2}, nil
		},
	}
	vectors := &mocks.VectorStore{
		SearchFn: func(_ context.Context, _ []float32, _ int) ([]domain.VectorResult, error) {
			return nil, nil // no relevant memory
		},
	}
	uc := newExecuteTaskUseCase(provider, &mocks.MCPClient{}, &mocks.MemoryStore{}, embedder, vectors)

	task := domain.Task{ID: "t-1", Instruction: "build the feature"}
	result, err := uc.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected Success=true")
	}
	if result.Output != "task done" {
		t.Fatalf("unexpected output: %s", result.Output)
	}
	if result.TaskID != "t-1" {
		t.Fatalf("unexpected TaskID: %s", result.TaskID)
	}
}

func TestExecuteTask_WithMemoryResults(t *testing.T) {
	provider := &mocks.ModelProvider{}
	embedder := &mocks.Embedder{
		EmbedFn: func(_ context.Context, _ string) ([]float32, error) {
			return []float32{0.5, 0.5}, nil
		},
	}
	vectors := &mocks.VectorStore{
		SearchFn: func(_ context.Context, _ []float32, _ int) ([]domain.VectorResult, error) {
			return []domain.VectorResult{{Key: "mem-key-1", Score: 0.9}}, nil
		},
	}
	memory := &mocks.MemoryStore{
		ReadFn: func(_ context.Context, key string) ([]byte, error) {
			if key == "mem-key-1" {
				return []byte("previous context"), nil
			}
			return nil, errors.New("not found")
		},
	}

	uc := newExecuteTaskUseCase(provider, &mocks.MCPClient{}, memory, embedder, vectors)

	task := domain.Task{ID: "t-2", Instruction: "do something"}
	result, err := uc.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected Success=true")
	}
	// The provider must have been called.
	if len(provider.InvokeCalls) != 1 {
		t.Fatalf("expected 1 provider call got %d", len(provider.InvokeCalls))
	}
}

func TestExecuteTask_EmbedError_GracefulDegrade(t *testing.T) {
	provider := &mocks.ModelProvider{}
	embedder := &mocks.Embedder{
		EmbedFn: func(_ context.Context, _ string) ([]float32, error) {
			return nil, errors.New("embedder unavailable")
		},
	}

	uc := newExecuteTaskUseCase(provider, &mocks.MCPClient{}, &mocks.MemoryStore{}, embedder, &mocks.VectorStore{})

	task := domain.Task{ID: "t-3", Instruction: "do something else"}
	result, err := uc.Execute(context.Background(), task)
	// Embed failure must be silently degraded — the use case falls back to raw instruction.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected Success=true even when embed fails")
	}
}

func TestExecuteTask_VectorSearchError_GracefulDegrade(t *testing.T) {
	provider := &mocks.ModelProvider{}
	embedder := &mocks.Embedder{
		EmbedFn: func(_ context.Context, _ string) ([]float32, error) {
			return []float32{0.1}, nil
		},
	}
	vectors := &mocks.VectorStore{
		SearchFn: func(_ context.Context, _ []float32, _ int) ([]domain.VectorResult, error) {
			return nil, errors.New("vector store unavailable")
		},
	}

	uc := newExecuteTaskUseCase(provider, &mocks.MCPClient{}, &mocks.MemoryStore{}, embedder, vectors)

	task := domain.Task{ID: "t-4", Instruction: "something"}
	result, err := uc.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected Success=true even when vector search fails")
	}
}

func TestExecuteTask_ProviderError_ReturnsError(t *testing.T) {
	sentinelErr := errors.New("model unavailable")
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			return domain.InvokeResponse{}, sentinelErr
		},
	}

	uc := newExecuteTaskUseCase(provider, &mocks.MCPClient{}, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})

	task := domain.Task{ID: "t-5", Instruction: "build something"}
	result, err := uc.Execute(context.Background(), task)
	if err == nil {
		t.Fatal("expected error from provider failure")
	}
	if result.TaskID != "t-5" {
		t.Fatalf("expected TaskID preserved on error, got %s", result.TaskID)
	}
	if result.Err == nil {
		t.Fatal("expected result.Err to be set on provider failure")
	}
}

func TestExecuteTask_SystemPromptPassedToProvider(t *testing.T) {
	const systemPrompt = "you are a helpful developer bot"
	var capturedSystemPrompt string
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
			capturedSystemPrompt = req.SystemPrompt
			return domain.InvokeResponse{Content: "ok"}, nil
		},
	}
	uc := application.NewExecuteTaskUseCase(
		provider, &mocks.MCPClient{}, &mocks.MemoryStore{},
		&mocks.Embedder{}, &mocks.VectorStore{},
		systemPrompt,
	)

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-6", Instruction: "do it"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedSystemPrompt != systemPrompt {
		t.Fatalf("expected system prompt %q got %q", systemPrompt, capturedSystemPrompt)
	}
}

func TestExecuteTask_MemoryReadError_SkipsMemory(t *testing.T) {
	provider := &mocks.ModelProvider{}
	embedder := &mocks.Embedder{
		EmbedFn: func(_ context.Context, _ string) ([]float32, error) {
			return []float32{0.5}, nil
		},
	}
	vectors := &mocks.VectorStore{
		SearchFn: func(_ context.Context, _ []float32, _ int) ([]domain.VectorResult, error) {
			return []domain.VectorResult{{Key: "missing-key", Score: 0.8}}, nil
		},
	}
	memory := &mocks.MemoryStore{
		ReadFn: func(_ context.Context, _ string) ([]byte, error) {
			return nil, errors.New("key not found")
		},
	}
	uc := newExecuteTaskUseCase(provider, &mocks.MCPClient{}, memory, embedder, vectors)

	task := domain.Task{ID: "t-7", Instruction: "help"}
	result, err := uc.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected Success=true when memory read fails silently")
	}
}
