package anthropic_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	anthropicpkg "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/anthropic"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/anthropic/mocks"
)

const testModelID = "claude-sonnet-4-5"

func newTestProvider(client *mocks.MessagesClient) *anthropicpkg.Provider {
	return anthropicpkg.NewProvider(client, testModelID)
}

func makeMessage(text string, stopReason anthropicsdk.StopReason, inputTokens, outputTokens int64) *anthropicsdk.Message {
	msg := &anthropicsdk.Message{}
	msg.StopReason = stopReason
	msg.Usage.InputTokens = inputTokens
	msg.Usage.OutputTokens = outputTokens
	// Content is a []ContentBlockUnion; we build one text block via the AsText path.
	// The SDK's ContentBlockUnion.Text field holds the text when the variant is TextBlock.
	msg.Content = []anthropicsdk.ContentBlockUnion{
		{Text: text},
	}
	return msg
}

// ---------------------------------------------------------------------------
// Invoke — success
// ---------------------------------------------------------------------------

func TestProvider_Invoke_SuccessfulResponse(t *testing.T) {
	want := "Hello, world!"
	mockClient := &mocks.MessagesClient{
		NewFn: func(_ context.Context, _ anthropicsdk.MessageNewParams, _ ...option.RequestOption) (*anthropicsdk.Message, error) {
			return makeMessage(want, anthropicsdk.StopReasonEndTurn, 10, 5), nil
		},
	}
	p := newTestProvider(mockClient)

	req := domain.InvokeRequest{
		SystemPrompt: "You are a helpful assistant.",
		Messages:     []domain.ProviderMessage{{Role: "user", Content: "Hello"}},
	}

	resp, err := p.Invoke(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != want {
		t.Errorf("expected %q, got %q", want, resp.Content)
	}
	if resp.StopReason != string(anthropicsdk.StopReasonEndTurn) {
		t.Errorf("expected stop_reason %q, got %q", anthropicsdk.StopReasonEndTurn, resp.StopReason)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("expected 5 output tokens, got %d", resp.Usage.OutputTokens)
	}
	if len(mockClient.NewCalls) != 1 {
		t.Fatalf("expected 1 New call, got %d", len(mockClient.NewCalls))
	}
}

func TestProvider_Invoke_DefaultsMaxTokens(t *testing.T) {
	var capturedParams anthropicsdk.MessageNewParams
	mockClient := &mocks.MessagesClient{
		NewFn: func(_ context.Context, params anthropicsdk.MessageNewParams, _ ...option.RequestOption) (*anthropicsdk.Message, error) {
			capturedParams = params
			return makeMessage("ok", anthropicsdk.StopReasonEndTurn, 1, 1), nil
		},
	}
	p := newTestProvider(mockClient)
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedParams.MaxTokens != 4096 {
		t.Errorf("expected default MaxTokens=4096, got %d", capturedParams.MaxTokens)
	}
}

func TestProvider_Invoke_PassesMaxTokensWhenSet(t *testing.T) {
	var capturedParams anthropicsdk.MessageNewParams
	mockClient := &mocks.MessagesClient{
		NewFn: func(_ context.Context, params anthropicsdk.MessageNewParams, _ ...option.RequestOption) (*anthropicsdk.Message, error) {
			capturedParams = params
			return makeMessage("ok", anthropicsdk.StopReasonEndTurn, 1, 1), nil
		},
	}
	p := newTestProvider(mockClient)
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages:  []domain.ProviderMessage{{Role: "user", Content: "hi"}},
		MaxTokens: 2048,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedParams.MaxTokens != 2048 {
		t.Errorf("expected MaxTokens=2048, got %d", capturedParams.MaxTokens)
	}
}

func TestProvider_Invoke_SystemPromptMapped(t *testing.T) {
	var capturedParams anthropicsdk.MessageNewParams
	mockClient := &mocks.MessagesClient{
		NewFn: func(_ context.Context, params anthropicsdk.MessageNewParams, _ ...option.RequestOption) (*anthropicsdk.Message, error) {
			capturedParams = params
			return makeMessage("ok", anthropicsdk.StopReasonEndTurn, 1, 1), nil
		},
	}
	p := newTestProvider(mockClient)
	const sysPrompt = "You are a senior engineer."
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		SystemPrompt: sysPrompt,
		Messages:     []domain.ProviderMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capturedParams.System) == 0 {
		t.Fatal("expected non-empty System param")
	}
	if capturedParams.System[0].Text != sysPrompt {
		t.Errorf("expected system prompt %q, got %q", sysPrompt, capturedParams.System[0].Text)
	}
}

func TestProvider_Invoke_MessagesRoleMapped(t *testing.T) {
	var capturedParams anthropicsdk.MessageNewParams
	mockClient := &mocks.MessagesClient{
		NewFn: func(_ context.Context, params anthropicsdk.MessageNewParams, _ ...option.RequestOption) (*anthropicsdk.Message, error) {
			capturedParams = params
			return makeMessage("ok", anthropicsdk.StopReasonEndTurn, 1, 1), nil
		},
	}
	p := newTestProvider(mockClient)
	msgs := []domain.ProviderMessage{
		{Role: "user", Content: "What's up?"},
		{Role: "assistant", Content: "Not much."},
	}
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{Messages: msgs})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capturedParams.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(capturedParams.Messages))
	}
	if string(capturedParams.Messages[0].Role) != "user" {
		t.Errorf("expected role 'user', got %q", capturedParams.Messages[0].Role)
	}
	if string(capturedParams.Messages[1].Role) != "assistant" {
		t.Errorf("expected role 'assistant', got %q", capturedParams.Messages[1].Role)
	}
}

func TestProvider_Invoke_EmptyContentBlock(t *testing.T) {
	mockClient := &mocks.MessagesClient{
		NewFn: func(_ context.Context, _ anthropicsdk.MessageNewParams, _ ...option.RequestOption) (*anthropicsdk.Message, error) {
			msg := &anthropicsdk.Message{}
			msg.StopReason = anthropicsdk.StopReasonEndTurn
			// No content blocks.
			return msg, nil
		},
	}
	p := newTestProvider(mockClient)
	resp, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
	}
}

// ---------------------------------------------------------------------------
// Invoke — rate limit error mapping
// ---------------------------------------------------------------------------

func newRateLimitAPIError() error {
	apiErr := &anthropicsdk.Error{}
	apiErr.StatusCode = http.StatusTooManyRequests
	return apiErr
}

func newOverloadedAPIError() error {
	apiErr := &anthropicsdk.Error{}
	apiErr.StatusCode = http.StatusServiceUnavailable
	return apiErr
}

func TestProvider_Invoke_RateLimitErrorMapped(t *testing.T) {
	mockClient := &mocks.MessagesClient{
		NewFn: func(_ context.Context, _ anthropicsdk.MessageNewParams, _ ...option.RequestOption) (*anthropicsdk.Message, error) {
			return nil, newRateLimitAPIError()
		},
	}
	p := newTestProvider(mockClient)
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, anthropicpkg.ErrRateLimit) {
		t.Errorf("expected ErrRateLimit, got: %v", err)
	}
	var rlErr *anthropicpkg.RateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("expected *RateLimitError, got %T", err)
	}
	if rlErr.RetryAfter == 0 {
		t.Error("expected non-zero RetryAfter")
	}
}

func TestProvider_Invoke_OverloadedErrorMappedToRateLimit(t *testing.T) {
	mockClient := &mocks.MessagesClient{
		NewFn: func(_ context.Context, _ anthropicsdk.MessageNewParams, _ ...option.RequestOption) (*anthropicsdk.Message, error) {
			return nil, newOverloadedAPIError()
		},
	}
	p := newTestProvider(mockClient)
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, anthropicpkg.ErrRateLimit) {
		t.Errorf("expected ErrRateLimit for overloaded, got: %v", err)
	}
}

func TestProvider_Invoke_GenericErrorWrapped(t *testing.T) {
	mockClient := &mocks.MessagesClient{
		NewFn: func(_ context.Context, _ anthropicsdk.MessageNewParams, _ ...option.RequestOption) (*anthropicsdk.Message, error) {
			return nil, errors.New("some other error")
		},
	}
	p := newTestProvider(mockClient)
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages: []domain.ProviderMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, anthropicpkg.ErrRateLimit) {
		t.Error("expected non-rate-limit error")
	}
	// Verify wrapping prefix is present.
	if err.Error()[:len("anthropic:")] != "anthropic:" {
		t.Errorf("expected error to start with 'anthropic:', got: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// NewFromEnv
// ---------------------------------------------------------------------------

func TestNewFromEnv_ErrorWhenKeyNotSet(t *testing.T) {
	if _, alreadySet := os.LookupEnv("ANTHROPIC_API_KEY"); alreadySet {
		t.Skip("ANTHROPIC_API_KEY is set in the environment; skipping NewFromEnv error test")
	}
	// Also unset ANTHROPIC_AUTH_TOKEN to prevent fallback.
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	_, err := anthropicpkg.NewFromEnv(testModelID)
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is not set")
	}
}

func TestNewFromEnv_SucceedsWhenKeySet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key-value")

	p, err := anthropicpkg.NewFromEnv(testModelID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

// ---------------------------------------------------------------------------
// RateLimitError methods
// ---------------------------------------------------------------------------

func TestRateLimitError_ErrorString(t *testing.T) {
	cause := errors.New("underlying cause")
	rlErr := &anthropicpkg.RateLimitError{
		RetryAfter: 60 * time.Second, //nolint:revive
		Cause:      cause,
	}
	s := rlErr.Error()
	if s == "" {
		t.Error("expected non-empty error string")
	}
	// Should contain the retry duration.
	if len(s) < 10 {
		t.Errorf("error string too short: %q", s)
	}
}

func TestRateLimitError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	rlErr := &anthropicpkg.RateLimitError{
		RetryAfter: 60 * time.Second, //nolint:revive
		Cause:      cause,
	}
	if !errors.Is(rlErr, cause) {
		t.Error("expected Unwrap to expose the cause")
	}
}

// ---------------------------------------------------------------------------
// Temperature mapping
// ---------------------------------------------------------------------------

func TestProvider_Invoke_TemperatureMapped(t *testing.T) {
	var capturedParams anthropicsdk.MessageNewParams
	mockClient := &mocks.MessagesClient{
		NewFn: func(_ context.Context, params anthropicsdk.MessageNewParams, _ ...option.RequestOption) (*anthropicsdk.Message, error) {
			capturedParams = params
			return makeMessage("ok", anthropicsdk.StopReasonEndTurn, 1, 1), nil
		},
	}
	p := newTestProvider(mockClient)
	_, err := p.Invoke(context.Background(), domain.InvokeRequest{
		Messages:    []domain.ProviderMessage{{Role: "user", Content: "hi"}},
		Temperature: 0.7,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !capturedParams.Temperature.Valid() {
		t.Error("expected Temperature to be set")
	}
}
