package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

const maxToolIterations = 50

type ExecuteTaskUseCase struct {
	provider     domain.ModelProvider
	chatProvider domain.ModelProvider // used for chat-source tasks; nil falls back to provider
	mcp          domain.MCPClient
	memory       domain.MemoryStore
	embedder     domain.Embedder
	vectors      domain.VectorStore
	soulPrompt   string
	progressFn   func(taskID, line string)
	askCh        <-chan domain.AskRequest
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

// WithChatProvider sets a dedicated model provider for chat-source tasks.
func (u *ExecuteTaskUseCase) WithChatProvider(p domain.ModelProvider) {
	u.chatProvider = p
}

// WithProgressHandler registers a callback invoked after each tool call with a
// human-readable progress line. taskID matches the executing task's ID.
func (u *ExecuteTaskUseCase) WithProgressHandler(fn func(taskID, line string)) {
	u.progressFn = fn
}

// WithAskChannel registers a channel from which mid-task user questions are
// drained between tool-call iterations. Each AskRequest.ReplyFn is called once
// with the model's answer.
func (u *ExecuteTaskUseCase) WithAskChannel(ch <-chan domain.AskRequest) {
	u.askCh = ch
}

func (u *ExecuteTaskUseCase) Execute(ctx context.Context, task domain.Task) (domain.TaskResult, error) {
	msgCtx, err := u.buildContext(ctx, task)
	if err != nil {
		return domain.TaskResult{TaskID: task.ID}, fmt.Errorf("build context: %w", err)
	}

	provider := u.provider
	if task.Source == "chat" && u.chatProvider != nil {
		provider = u.chatProvider
	}

	tools, _ := u.mcp.ListTools(ctx) // graceful degrade: empty tools → single-turn

	messages := []domain.ProviderMessage{{Role: "user", Content: msgCtx}}

	for i := range maxToolIterations {
		resp, err := provider.Invoke(ctx, domain.InvokeRequest{
			SystemPrompt: u.soulPrompt,
			Messages:     messages,
			Tools:        tools,
		})
		if err != nil {
			return domain.TaskResult{TaskID: task.ID, Err: err}, fmt.Errorf("model invoke (iteration %d): %w", i+1, err)
		}

		// No tool calls — model produced its final response.
		if len(resp.ToolCalls) == 0 {
			return domain.TaskResult{
				TaskID:  task.ID,
				Output:  resp.Content,
				Success: true,
			}, nil
		}

		// Append the assistant message with tool calls.
		messages = append(messages, domain.ProviderMessage{
			Role:      "assistant",
			ToolCalls: resp.ToolCalls,
		})

		// Execute each tool call and append the results.
		for _, tc := range resp.ToolCalls {
			result, callErr := u.mcp.CallTool(ctx, tc.Name, tc.Args)
			content := toolResultContent(result, callErr)
			messages = append(messages, domain.ProviderMessage{
				Role:       "tool",
				Content:    content,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
			})
			if u.progressFn != nil {
				u.progressFn(task.ID, formatProgressLine(tc, result, callErr))
			}
		}

		// Answer any mid-task user questions before the next model invocation.
		u.drainAsks(ctx, provider, messages)
	}

	return domain.TaskResult{TaskID: task.ID, Err: fmt.Errorf("exceeded max tool iterations (%d)", maxToolIterations)},
		fmt.Errorf("execute task: exceeded max tool iterations (%d)", maxToolIterations)
}

// toolResultContent collapses an MCPToolResult into a single string for the
// tool result message. Call errors are surfaced as plain text so the model
// can recover or report them.
func toolResultContent(result domain.MCPToolResult, callErr error) string {
	if callErr != nil {
		return fmt.Sprintf("error: %v", callErr)
	}
	parts := make([]string, 0, len(result.Content))
	for _, c := range result.Content {
		parts = append(parts, c.Text)
	}
	text := strings.Join(parts, "\n")
	if result.IsError {
		return "error: " + text
	}
	return text
}

// drainAsks non-blockingly reads all pending AskRequests from the ask channel,
// invokes the model once per ask (no tools), and calls each ReplyFn with the answer.
func (u *ExecuteTaskUseCase) drainAsks(ctx context.Context, provider domain.ModelProvider, history []domain.ProviderMessage) {
	if u.askCh == nil {
		return
	}
	for {
		select {
		case ask, ok := <-u.askCh:
			if !ok {
				return
			}
			askMsgs := append(history, domain.ProviderMessage{ //nolint:gocritic // intentional copy
				Role:    "user",
				Content: "[User question — please answer concisely]: " + ask.Question,
			})
			resp, err := provider.Invoke(ctx, domain.InvokeRequest{
				SystemPrompt: u.soulPrompt,
				Messages:     askMsgs,
			})
			if err == nil && ask.ReplyFn != nil {
				ask.ReplyFn(resp.Content)
			}
		default:
			return
		}
	}
}

// formatProgressLine builds a human-readable trace line for a completed tool call.
func formatProgressLine(tc domain.ToolCall, result domain.MCPToolResult, callErr error) string {
	status := "ok"
	if callErr != nil {
		status = "error: " + callErr.Error()
	} else if result.IsError && len(result.Content) > 0 {
		status = "error: " + result.Content[0].Text
	}
	arg := argSummaryFor(tc)
	if arg != "" {
		return fmt.Sprintf("→ [%s] %s → %s", tc.Name, arg, status)
	}
	return fmt.Sprintf("→ [%s] → %s", tc.Name, status)
}

// argSummaryFor returns the most meaningful single argument value for a tool call.
func argSummaryFor(tc domain.ToolCall) string {
	for _, key := range []string{"path", "command"} {
		if v, ok := tc.Args[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
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
