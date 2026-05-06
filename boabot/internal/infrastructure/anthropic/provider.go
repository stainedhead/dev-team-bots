// Package anthropic provides an Anthropic-API-backed implementation of
// domain.ModelProvider, with rate-limit error mapping and an injectable client
// for testability.
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// MessagesClient is the subset of the Anthropic SDK client used by Provider.
// Consumers inject a concrete *anthropicsdk.MessageService or a test mock.
type MessagesClient interface {
	New(ctx context.Context, params anthropicsdk.MessageNewParams, opts ...option.RequestOption) (*anthropicsdk.Message, error)
}

// RateLimitError is returned when the Anthropic API signals rate-limiting or
// service overload. Callers should inspect RetryAfter for the suggested wait.
type RateLimitError struct {
	RetryAfter time.Duration
	Cause      error
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("anthropic: rate limited (retry after %s): %v", e.RetryAfter, e.Cause)
}

func (e *RateLimitError) Unwrap() error { return e.Cause }

// ErrRateLimit is the sentinel that callers can match via errors.Is.
var ErrRateLimit = errors.New("anthropic: rate limit")

func (e *RateLimitError) Is(target error) bool { return target == ErrRateLimit }

// Provider is an Anthropic-API-backed ModelProvider.
type Provider struct {
	client  MessagesClient
	modelID string
}

// NewProvider creates a Provider using the supplied MessagesClient.
func NewProvider(client MessagesClient, modelID string) *Provider {
	return &Provider{client: client, modelID: modelID}
}

// NewFromEnv constructs a Provider backed by the real Anthropic SDK, reading
// credentials exclusively from the ANTHROPIC_API_KEY environment variable.
// Returns a clear error when the variable is not set or is empty.
func NewFromEnv(modelID string) (*Provider, error) {
	key, ok := os.LookupEnv("ANTHROPIC_API_KEY")
	if !ok || key == "" {
		return nil, errors.New("anthropic: ANTHROPIC_API_KEY environment variable is not set")
	}
	c := anthropicsdk.NewClient(option.WithAPIKey(key))
	return NewProvider(&c.Messages, modelID), nil
}

// Invoke calls the Anthropic Messages API and maps the response to domain types.
// If the API returns a 429 (rate limit) or 529/503 (overloaded) status code the
// error is wrapped in a RateLimitError (RetryAfter defaults to 60s).
func (p *Provider) Invoke(ctx context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
	params := buildParams(req, p.modelID)

	msg, err := p.client.New(ctx, params)
	if err != nil {
		return domain.InvokeResponse{}, p.mapError(err)
	}

	return mapResponse(msg), nil
}

// mapError converts Anthropic SDK errors to domain-level errors where appropriate.
// It is only called when err is non-nil.
func (p *Provider) mapError(err error) error {
	var apiErr *anthropicsdk.Error
	if errors.As(err, &apiErr) {
		sc := apiErr.StatusCode
		if sc == http.StatusTooManyRequests ||
			sc == http.StatusServiceUnavailable ||
			sc == 529 { // Anthropic-specific overloaded status
			return &RateLimitError{RetryAfter: 60 * time.Second, Cause: err}
		}
	}
	return fmt.Errorf("anthropic: %w", err)
}

// --- internal helpers ---

func buildParams(req domain.InvokeRequest, modelID string) anthropicsdk.MessageNewParams {
	maxTokens := int64(req.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 4096
	}

	msgs := make([]anthropicsdk.MessageParam, len(req.Messages))
	for i, m := range req.Messages {
		block := anthropicsdk.NewTextBlock(m.Content)
		switch m.Role {
		case "assistant":
			msgs[i] = anthropicsdk.NewAssistantMessage(block)
		default:
			msgs[i] = anthropicsdk.NewUserMessage(block)
		}
	}

	params := anthropicsdk.MessageNewParams{
		Model:     anthropicsdk.Model(modelID),
		MaxTokens: maxTokens,
		Messages:  msgs,
	}

	if req.SystemPrompt != "" {
		params.System = []anthropicsdk.TextBlockParam{
			{Text: req.SystemPrompt},
		}
	}

	if req.Temperature != 0 {
		params.Temperature = param.NewOpt(float64(req.Temperature))
	}

	return params
}

func mapResponse(msg *anthropicsdk.Message) domain.InvokeResponse {
	content := ""
	if len(msg.Content) > 0 {
		content = msg.Content[0].Text
	}
	return domain.InvokeResponse{
		Content:    content,
		StopReason: string(msg.StopReason),
		Usage: domain.TokenUsage{
			InputTokens:  int(msg.Usage.InputTokens),
			OutputTokens: int(msg.Usage.OutputTokens),
		},
	}
}
