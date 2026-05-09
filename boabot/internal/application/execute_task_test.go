package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func TestExecuteTask_ToolLoop_SingleToolCall(t *testing.T) {
	callCount := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
			callCount++
			if callCount == 1 {
				// First turn: model calls a tool.
				return domain.InvokeResponse{
					ToolCalls:  []domain.ToolCall{{ID: "c1", Name: "read_file", Args: map[string]any{"path": "/allowed/f.txt"}}},
					StopReason: "tool_calls",
				}, nil
			}
			// Second turn: model produces final answer.
			return domain.InvokeResponse{Content: "file was read", StopReason: "stop"}, nil
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "read_file", Description: "reads a file"}}, nil
		},
		CallToolFn: func(_ context.Context, name string, _ map[string]any) (domain.MCPToolResult, error) {
			if name != "read_file" {
				return domain.MCPToolResult{IsError: true}, nil
			}
			return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: "contents"}}}, nil
		},
	}
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})

	result, err := uc.Execute(context.Background(), domain.Task{ID: "t-tool-1", Instruction: "read a file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatal("expected Success=true")
	}
	if result.Output != "file was read" {
		t.Errorf("expected 'file was read', got %q", result.Output)
	}
	if callCount != 2 {
		t.Errorf("expected 2 provider calls (tool + final), got %d", callCount)
	}
}

func TestExecuteTask_ToolLoop_MultipleRounds(t *testing.T) {
	calls := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
			calls++
			switch calls {
			case 1:
				return domain.InvokeResponse{
					ToolCalls:  []domain.ToolCall{{ID: "c1", Name: "write_file", Args: map[string]any{"path": "/tmp/a.txt", "content": "hello"}}},
					StopReason: "tool_calls",
				}, nil
			case 2:
				return domain.InvokeResponse{
					ToolCalls:  []domain.ToolCall{{ID: "c2", Name: "read_file", Args: map[string]any{"path": "/tmp/a.txt"}}},
					StopReason: "tool_calls",
				}, nil
			default:
				return domain.InvokeResponse{Content: "all done", StopReason: "stop"}, nil
			}
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{
				{Name: "write_file"}, {Name: "read_file"},
			}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: "ok"}}}, nil
		},
	}
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})

	result, err := uc.Execute(context.Background(), domain.Task{ID: "t-multi", Instruction: "write then read"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "all done" {
		t.Errorf("expected 'all done', got %q", result.Output)
	}
	if calls != 3 {
		t.Errorf("expected 3 provider calls, got %d", calls)
	}
}

func TestExecuteTask_ToolLoop_MaxIterationsExceeded_ReturnsError(t *testing.T) {
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			// Always return a tool call — never finishes.
			return domain.InvokeResponse{
				ToolCalls:  []domain.ToolCall{{ID: "cx", Name: "read_file", Args: map[string]any{"path": "/tmp/x"}}},
				StopReason: "tool_calls",
			}, nil
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "read_file"}}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: "ok"}}}, nil
		},
	}
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-inf", Instruction: "loop forever"})
	if err == nil {
		t.Fatal("expected error when max iterations exceeded")
	}
}

func TestExecuteTask_NoTools_SingleTurnUnchanged(t *testing.T) {
	invocations := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
			invocations++
			if len(req.Tools) != 0 {
				return domain.InvokeResponse{}, fmt.Errorf("expected no tools, got %d", len(req.Tools))
			}
			return domain.InvokeResponse{Content: "single turn", StopReason: "stop"}, nil
		},
	}
	// mcp returns no tools
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) { return nil, nil },
	}
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})

	result, err := uc.Execute(context.Background(), domain.Task{ID: "t-notool", Instruction: "simple"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "single turn" {
		t.Errorf("expected 'single turn', got %q", result.Output)
	}
	if invocations != 1 {
		t.Errorf("expected exactly 1 provider call, got %d", invocations)
	}
}

func TestExecuteTask_ToolCallError_IncludedAsToolResult(t *testing.T) {
	calls := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
			calls++
			if calls == 1 {
				return domain.InvokeResponse{
					ToolCalls:  []domain.ToolCall{{ID: "c1", Name: "read_file", Args: map[string]any{"path": "/bad"}}},
					StopReason: "tool_calls",
				}, nil
			}
			// Check the tool result message was included.
			lastMsg := req.Messages[len(req.Messages)-1]
			if lastMsg.Role != "tool" {
				return domain.InvokeResponse{}, fmt.Errorf("expected last message role=tool, got %q", lastMsg.Role)
			}
			return domain.InvokeResponse{Content: "handled error", StopReason: "stop"}, nil
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "read_file"}}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{
				IsError: true,
				Content: []domain.MCPContent{{Type: "text", Text: "permission denied"}},
			}, nil
		},
	}
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})

	result, err := uc.Execute(context.Background(), domain.Task{ID: "t-err", Instruction: "read bad path"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "handled error" {
		t.Errorf("expected 'handled error', got %q", result.Output)
	}
}

func TestExecuteTask_AskChannel_HandledBetweenToolCalls(t *testing.T) {
	n := 0
	var replyGot string

	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
			n++
			switch n {
			case 1: // main loop: call a tool
				return domain.InvokeResponse{
					ToolCalls:  []domain.ToolCall{{ID: "c1", Name: "read_file", Args: map[string]any{"path": "/tmp/f"}}},
					StopReason: "tool_calls",
				}, nil
			case 2: // ask drain: answer the queued question
				return domain.InvokeResponse{Content: "doing great", StopReason: "stop"}, nil
			default: // main loop: final answer
				return domain.InvokeResponse{Content: "task done", StopReason: "stop"}, nil
			}
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "read_file"}}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: "ok"}}}, nil
		},
	}

	askCh := make(chan domain.AskRequest, 1)
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithAskChannel(askCh)

	// Enqueue the ask before Execute runs so it is present when the first tool call completes.
	askCh <- domain.AskRequest{
		Question: "how is it going?",
		ReplyFn:  func(reply string) { replyGot = reply },
	}

	result, err := uc.Execute(context.Background(), domain.Task{ID: "t-ask-1", Instruction: "do work"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "task done" {
		t.Errorf("expected 'task done', got %q", result.Output)
	}
	if replyGot != "doing great" {
		t.Errorf("expected ask reply 'doing great', got %q", replyGot)
	}
	if n != 3 {
		t.Errorf("expected 3 provider calls (main tool + ask + main final), got %d", n)
	}
}

func TestExecuteTask_AskChannel_NoAsk_DoesNotBlock(t *testing.T) {
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			return domain.InvokeResponse{Content: "done", StopReason: "stop"}, nil
		},
	}
	askCh := make(chan domain.AskRequest, 1) // empty — no asks
	uc := newExecuteTaskUseCase(provider, &mocks.MCPClient{}, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithAskChannel(askCh)

	result, err := uc.Execute(context.Background(), domain.Task{ID: "t-ask-2", Instruction: "simple"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestExecuteTask_ProgressHandler_CalledAfterEachToolCall(t *testing.T) {
	callCount := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			callCount++
			if callCount == 1 {
				return domain.InvokeResponse{
					ToolCalls:  []domain.ToolCall{{ID: "c1", Name: "write_file", Args: map[string]any{"path": "/tmp/f.txt"}}},
					StopReason: "tool_calls",
				}, nil
			}
			return domain.InvokeResponse{Content: "done", StopReason: "stop"}, nil
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "write_file"}}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: "ok"}}}, nil
		},
	}

	var progressLines []string
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithProgressHandler(func(_ string, line string) {
		progressLines = append(progressLines, line)
	})

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-prog-1", Instruction: "write it"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(progressLines) != 1 {
		t.Fatalf("expected 1 progress line, got %d: %v", len(progressLines), progressLines)
	}
	if !strings.Contains(progressLines[0], "write_file") {
		t.Errorf("expected tool name in progress line, got %q", progressLines[0])
	}
}

func TestExecuteTask_ProgressHandler_ToolError_StillCalled(t *testing.T) {
	calls := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			calls++
			if calls == 1 {
				return domain.InvokeResponse{
					ToolCalls:  []domain.ToolCall{{ID: "c1", Name: "read_file", Args: map[string]any{"path": "/bad"}}},
					StopReason: "tool_calls",
				}, nil
			}
			return domain.InvokeResponse{Content: "handled", StopReason: "stop"}, nil
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "read_file"}}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{
				IsError: true,
				Content: []domain.MCPContent{{Type: "text", Text: "permission denied"}},
			}, nil
		},
	}

	var progressLines []string
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithProgressHandler(func(_ string, line string) {
		progressLines = append(progressLines, line)
	})

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-prog-2", Instruction: "read bad"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(progressLines) != 1 {
		t.Fatalf("expected 1 progress line, got %d", len(progressLines))
	}
	if !strings.Contains(progressLines[0], "error") {
		t.Errorf("expected 'error' in progress line for failed tool call, got %q", progressLines[0])
	}
}

func TestExecuteTask_ProgressHandler_MultipleToolCalls(t *testing.T) {
	n := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			n++
			switch n {
			case 1:
				return domain.InvokeResponse{
					ToolCalls: []domain.ToolCall{
						{ID: "c1", Name: "write_file", Args: map[string]any{"path": "/tmp/a.txt"}},
						{ID: "c2", Name: "create_dir", Args: map[string]any{"path": "/tmp/d"}},
					},
					StopReason: "tool_calls",
				}, nil
			default:
				return domain.InvokeResponse{Content: "all done", StopReason: "stop"}, nil
			}
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "write_file"}, {Name: "create_dir"}}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: "ok"}}}, nil
		},
	}

	var progressLines []string
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithProgressHandler(func(_ string, line string) {
		progressLines = append(progressLines, line)
	})

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-prog-3", Instruction: "multi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(progressLines) != 2 {
		t.Fatalf("expected 2 progress lines (one per tool call), got %d: %v", len(progressLines), progressLines)
	}
}

func TestExecuteTask_RulesTracker_PreloadsWorkDir(t *testing.T) {
	// The RulesTracker should be Reset and UpdateForDir called with task.WorkDir
	// before any model invocation, so rules appear in the initial context.
	var capturedMessages []domain.ProviderMessage
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
			capturedMessages = req.Messages
			return domain.InvokeResponse{Content: "done"}, nil
		},
	}
	rt := &mocks.RulesTracker{
		UpdateForDirFn: func(_ context.Context, dir string) domain.RulesUpdate {
			return domain.RulesUpdate{
				Add: []domain.RulesEntry{{Dir: dir, File: "AGENTS.md", Content: "# Work rules"}},
			}
		},
	}
	uc := newExecuteTaskUseCase(provider, &mocks.MCPClient{}, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithRulesTracker(rt)

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-rules-1", Instruction: "do work", WorkDir: "/tmp/work"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.Resets != 1 {
		t.Errorf("expected Reset called once, got %d", rt.Resets)
	}
	if len(rt.Dirs) == 0 || rt.Dirs[0] != "/tmp/work" {
		t.Errorf("expected first UpdateForDir call with WorkDir, got %v", rt.Dirs)
	}
	// Rules content should appear in the initial user message.
	if len(capturedMessages) == 0 {
		t.Fatal("no messages captured")
	}
	if !strings.Contains(capturedMessages[0].Content, "# Work rules") {
		t.Errorf("rules content not found in initial message: %q", capturedMessages[0].Content)
	}
}

func TestExecuteTask_RulesTracker_UpdatesOnToolCallPaths(t *testing.T) {
	// After each tool call, UpdateForDir should be called with the directory of
	// the path argument so the model sees rules context updates.
	callCount := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
			callCount++
			if callCount == 1 {
				return domain.InvokeResponse{
					ToolCalls: []domain.ToolCall{{
						ID:   "tc-1",
						Name: "read_file",
						Args: map[string]any{"path": "/tmp/repo/src/main.go"},
					}},
				}, nil
			}
			return domain.InvokeResponse{Content: "done"}, nil
		},
	}
	mcp := &mocks.MCPClient{
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{Content: []domain.MCPContent{{Text: "file content"}}}, nil
		},
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "read_file"}}, nil
		},
	}
	rt := &mocks.RulesTracker{}
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithRulesTracker(rt)

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-rules-2", Instruction: "read a file", WorkDir: "/tmp/repo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// UpdateForDir should have been called for the tool call's directory.
	foundSrc := false
	for _, d := range rt.Dirs {
		if d == "/tmp/repo/src" {
			foundSrc = true
			break
		}
	}
	if !foundSrc {
		t.Errorf("expected UpdateForDir called for /tmp/repo/src; got dirs: %v", rt.Dirs)
	}
}

func TestExecuteTask_RulesTracker_InjectsRulesAsUserMessage(t *testing.T) {
	// When a tool call triggers new rules, they should be injected as a user
	// message so the model sees them before the next invocation.
	callCount := 0
	var secondCallMessages []domain.ProviderMessage
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
			callCount++
			if callCount == 1 {
				return domain.InvokeResponse{
					ToolCalls: []domain.ToolCall{{
						ID:   "tc-1",
						Name: "read_file",
						Args: map[string]any{"path": "/tmp/repo/src/foo.go"},
					}},
				}, nil
			}
			secondCallMessages = req.Messages
			return domain.InvokeResponse{Content: "done"}, nil
		},
	}
	mcp := &mocks.MCPClient{
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{Content: []domain.MCPContent{{Text: "ok"}}}, nil
		},
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "read_file"}}, nil
		},
	}
	rt := &mocks.RulesTracker{
		UpdateForDirFn: func(_ context.Context, dir string) domain.RulesUpdate {
			if strings.Contains(dir, "src") {
				return domain.RulesUpdate{
					Add: []domain.RulesEntry{{Dir: dir, File: "AGENTS.md", Content: "# Src rules"}},
				}
			}
			return domain.RulesUpdate{}
		},
	}
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithRulesTracker(rt)

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-rules-3", Instruction: "read something"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	foundRulesMsg := false
	for _, msg := range secondCallMessages {
		if msg.Role == "user" && strings.Contains(msg.Content, "# Src rules") {
			foundRulesMsg = true
			break
		}
	}
	if !foundRulesMsg {
		t.Errorf("expected rules injection as user message in second call; messages: %+v", secondCallMessages)
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

// TestExecuteTask_WithChatProvider_UsedForChatSource verifies that a dedicated
// chat provider is used when task.Source == "chat".
func TestExecuteTask_WithChatProvider_UsedForChatSource(t *testing.T) {
	chatCalled := false
	chatProvider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			chatCalled = true
			return domain.InvokeResponse{Content: "chat response", StopReason: "stop"}, nil
		},
	}
	defaultProvider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			return domain.InvokeResponse{Content: "default response", StopReason: "stop"}, nil
		},
	}
	uc := newExecuteTaskUseCase(defaultProvider, &mocks.MCPClient{}, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithChatProvider(chatProvider)

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-chat-1", Source: "chat", Instruction: "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !chatCalled {
		t.Error("expected chat provider to be called for chat-source task")
	}
}

// TestExecuteTask_ProgressHandler_CallErr_InProgress verifies that formatProgressLine
// includes the error string when CallTool returns a hard error (callErr != nil).
func TestExecuteTask_ProgressHandler_CallErr_InProgress(t *testing.T) {
	n := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			n++
			if n == 1 {
				return domain.InvokeResponse{
					ToolCalls:  []domain.ToolCall{{ID: "c1", Name: "broken_tool", Args: map[string]any{}}},
					StopReason: "tool_calls",
				}, nil
			}
			return domain.InvokeResponse{Content: "done"}, nil
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "broken_tool"}}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{}, errors.New("connection refused")
		},
	}

	var progressLines []string
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithProgressHandler(func(_ string, line string) {
		progressLines = append(progressLines, line)
	})

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-callerr", Instruction: "do it"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(progressLines) == 0 {
		t.Fatal("expected a progress line, got none")
	}
	if !strings.Contains(progressLines[0], "connection refused") {
		t.Errorf("expected callErr text in progress line, got %q", progressLines[0])
	}
}

// TestExecuteTask_ProgressHandler_NoArgTool verifies that formatProgressLine
// emits the no-arg format when the tool has no "path" or "command" argument.
func TestExecuteTask_ProgressHandler_NoArgTool(t *testing.T) {
	n := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			n++
			if n == 1 {
				return domain.InvokeResponse{
					ToolCalls:  []domain.ToolCall{{ID: "c1", Name: "get_time", Args: map[string]any{"format": "unix"}}},
					StopReason: "tool_calls",
				}, nil
			}
			return domain.InvokeResponse{Content: "done"}, nil
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "get_time"}}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: "1234567890"}}}, nil
		},
	}

	var progressLines []string
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithProgressHandler(func(_ string, line string) {
		progressLines = append(progressLines, line)
	})

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-noarg", Instruction: "get time"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(progressLines) == 0 {
		t.Fatal("expected progress line, got none")
	}
	if !strings.Contains(progressLines[0], "get_time") {
		t.Errorf("expected tool name in no-arg progress line, got %q", progressLines[0])
	}
}

// TestExecuteTask_RulesTracker_RunShell verifies that dirForToolCall extracts the
// working_dir from a run_shell tool call and passes it to UpdateForDir.
func TestExecuteTask_RulesTracker_RunShell(t *testing.T) {
	var trackerDirs []string
	n := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			n++
			if n == 1 {
				return domain.InvokeResponse{
					ToolCalls: []domain.ToolCall{{
						ID:   "c1",
						Name: "run_shell",
						Args: map[string]any{"working_dir": "/tmp/proj", "command": "ls"},
					}},
					StopReason: "tool_calls",
				}, nil
			}
			return domain.InvokeResponse{Content: "done"}, nil
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "run_shell"}}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: "file.txt"}}}, nil
		},
	}
	rt := &mocks.RulesTracker{
		UpdateForDirFn: func(_ context.Context, dir string) domain.RulesUpdate {
			trackerDirs = append(trackerDirs, dir)
			return domain.RulesUpdate{}
		},
	}

	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithRulesTracker(rt)

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-runshell", Instruction: "run ls"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trackerDirs) == 0 {
		t.Error("expected RulesTracker.UpdateForDir called for run_shell working_dir")
	}
	if len(trackerDirs) > 0 && trackerDirs[0] != "/tmp/proj" {
		t.Errorf("expected dir /tmp/proj, got %q", trackerDirs[0])
	}
}

// TestExecuteTask_RulesTracker_RemoveRules verifies that formatRulesMessage emits
// [RULES ENDED] lines when a RulesUpdate contains Remove entries.
func TestExecuteTask_RulesTracker_RemoveRules(t *testing.T) {
	var secondCallMessages []domain.ProviderMessage
	n := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
			n++
			if n == 1 {
				return domain.InvokeResponse{
					ToolCalls: []domain.ToolCall{{
						ID:   "c1",
						Name: "read_file",
						Args: map[string]any{"path": "/tmp/leaving/file.txt"},
					}},
					StopReason: "tool_calls",
				}, nil
			}
			secondCallMessages = req.Messages
			return domain.InvokeResponse{Content: "done"}, nil
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "read_file"}}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: "content"}}}, nil
		},
	}
	rt := &mocks.RulesTracker{
		UpdateForDirFn: func(_ context.Context, _ string) domain.RulesUpdate {
			return domain.RulesUpdate{
				Remove: []domain.RulesEntry{{Dir: "/tmp/leaving", File: "AGENTS.md"}},
			}
		},
	}

	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithRulesTracker(rt)

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-removedrules", Instruction: "read file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rulesEndedMsg := false
	for _, msg := range secondCallMessages {
		if msg.Role == "user" && strings.Contains(msg.Content, "RULES ENDED") {
			rulesEndedMsg = true
			break
		}
	}
	if !rulesEndedMsg {
		t.Errorf("expected [RULES ENDED] message in second call; messages: %+v", secondCallMessages)
	}
}

// TestExecuteTask_RulesTracker_UnknownTool verifies that dirForToolCall returns
// "" for an unknown tool name, so UpdateForDir is not called.
func TestExecuteTask_RulesTracker_UnknownTool(t *testing.T) {
	updateCalled := false
	n := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			n++
			if n == 1 {
				return domain.InvokeResponse{
					ToolCalls:  []domain.ToolCall{{ID: "c1", Name: "unknown_tool", Args: map[string]any{}}},
					StopReason: "tool_calls",
				}, nil
			}
			return domain.InvokeResponse{Content: "done"}, nil
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "unknown_tool"}}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: "ok"}}}, nil
		},
	}
	rt := &mocks.RulesTracker{
		UpdateForDirFn: func(_ context.Context, _ string) domain.RulesUpdate {
			updateCalled = true
			return domain.RulesUpdate{}
		},
	}

	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})
	uc.WithRulesTracker(rt)

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-unknown", Instruction: "run unknown"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updateCalled {
		t.Error("expected RulesTracker.UpdateForDir NOT to be called for unknown tool")
	}
}

// TestExecuteTask_DrainAsks_ClosedChannel verifies that drainAsks exits cleanly
// when the ask channel is closed (channel not-ok case).
func TestExecuteTask_DrainAsks_ClosedChannel(t *testing.T) {
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			return domain.InvokeResponse{Content: "done"}, nil
		},
	}
	uc := newExecuteTaskUseCase(provider, &mocks.MCPClient{}, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})

	// Set up a closed ask channel.
	askCh := make(chan domain.AskRequest)
	close(askCh)
	uc.WithAskChannel(askCh)

	// Execute should complete without hanging or panicking.
	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-closed-ask", Instruction: "do it"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// dirAllowingMCPClient wraps MCPClient and adds an AllowDir method so the
// dirAllower interface check in Execute is satisfied.
type dirAllowingMCPClient struct {
	*mocks.MCPClient
	allowDirCalled bool
}

func (m *dirAllowingMCPClient) AllowDir(_ string) func() {
	m.allowDirCalled = true
	return func() {}
}

// TestExecuteTask_DirAllower_WorkDir verifies that AllowDir is called when the
// task has a non-empty WorkDir and the MCP client implements dirAllower.
func TestExecuteTask_DirAllower_WorkDir(t *testing.T) {
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			return domain.InvokeResponse{Content: "done"}, nil
		},
	}
	mcpClient := &dirAllowingMCPClient{MCPClient: &mocks.MCPClient{}}
	uc := newExecuteTaskUseCase(provider, mcpClient, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-workdir", Instruction: "run", WorkDir: "/tmp/work"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mcpClient.allowDirCalled {
		t.Error("expected AllowDir to be called when task.WorkDir is set")
	}
}

// TestExecuteTask_DrainAsks_ClosedChannel_WithToolCall verifies that drainAsks
// exits cleanly via the !ok path when the ask channel is closed and drainAsks
// is reached (requires at least one tool call to enter the tool-call loop).
func TestExecuteTask_DrainAsks_ClosedChannel_WithToolCall(t *testing.T) {
	n := 0
	provider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			n++
			if n == 1 {
				return domain.InvokeResponse{
					ToolCalls:  []domain.ToolCall{{ID: "c1", Name: "list_dir", Args: map[string]any{}}},
					StopReason: "tool_calls",
				}, nil
			}
			return domain.InvokeResponse{Content: "done"}, nil
		},
	}
	mcp := &mocks.MCPClient{
		ListToolsFn: func(_ context.Context) ([]domain.MCPTool, error) {
			return []domain.MCPTool{{Name: "list_dir"}}, nil
		},
		CallToolFn: func(_ context.Context, _ string, _ map[string]any) (domain.MCPToolResult, error) {
			return domain.MCPToolResult{Content: []domain.MCPContent{{Type: "text", Text: "ok"}}}, nil
		},
	}
	uc := newExecuteTaskUseCase(provider, mcp, &mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})

	// Closed channel: drainAsks select reads zero value with ok=false → !ok branch.
	askCh := make(chan domain.AskRequest)
	close(askCh)
	uc.WithAskChannel(askCh)

	_, err := uc.Execute(context.Background(), domain.Task{ID: "t-drain-closed", Instruction: "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
