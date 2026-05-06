// Package bedrock provides a Bedrock-backed implementation of domain.ModelProvider,
// with rate-limit error mapping, streaming support, and injectable client for
// testability.
package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// BedrockRuntimeClient is the subset of the AWS Bedrock Runtime SDK client
// used by Provider. Consumers inject a concrete *bedrockruntime.Client or a
// test mock.
type BedrockRuntimeClient interface {
	InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
	InvokeModelWithResponseStream(ctx context.Context, params *bedrockruntime.InvokeModelWithResponseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelWithResponseStreamOutput, error)
}

// RateLimitError is returned when Bedrock signals throttling or temporary
// unavailability. Callers should inspect RetryAfter for the suggested wait.
type RateLimitError struct {
	RetryAfter time.Duration
	Cause      error
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("bedrock: rate limited (retry after %s): %v", e.RetryAfter, e.Cause)
}

func (e *RateLimitError) Unwrap() error { return e.Cause }

// ErrRateLimit is the sentinel that callers can match via errors.Is.
var ErrRateLimit = errors.New("bedrock: rate limit")

func (e *RateLimitError) Is(target error) bool { return target == ErrRateLimit }

// Provider is a Bedrock-backed ModelProvider.
type Provider struct {
	client  BedrockRuntimeClient
	modelID string
}

// NewProvider creates a Provider using the supplied BedrockRuntimeClient.
func NewProvider(client BedrockRuntimeClient, modelID string) *Provider {
	return &Provider{client: client, modelID: modelID}
}

// Invoke calls InvokeModel and maps the response to domain types.
// If Bedrock returns a ThrottlingException or ServiceUnavailableException the
// error is wrapped in a RateLimitError (RetryAfter defaults to 60s).
func (p *Provider) Invoke(ctx context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
	body, err := marshalRequest(req)
	if err != nil {
		return domain.InvokeResponse{}, fmt.Errorf("bedrock invoke: marshal request: %w", err)
	}

	out, err := p.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(p.modelID),
		Body:        body,
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return domain.InvokeResponse{}, p.mapError(err)
	}

	return unmarshalResponse(out.Body)
}

// ResponseStreamReader is the subset of the Bedrock event stream reader needed
// for streaming token output. It is satisfied by the SDK's
// InvokeModelWithResponseStreamEventStream.
type ResponseStreamReader interface {
	Events() <-chan brtypes.ResponseStream
	Close() error
}

// InvokeStream calls InvokeModelWithResponseStream and returns a channel that
// emits text tokens as they arrive. The channel is closed when the stream ends
// or an error occurs; callers should drain the channel and check for errors via
// the returned error value. If rate-limited, a RateLimitError is returned
// immediately and no channel is produced.
func (p *Provider) InvokeStream(ctx context.Context, req domain.InvokeRequest) (<-chan string, error) {
	body, err := marshalRequest(req)
	if err != nil {
		return nil, fmt.Errorf("bedrock stream: marshal request: %w", err)
	}

	out, err := p.client.InvokeModelWithResponseStream(ctx, &bedrockruntime.InvokeModelWithResponseStreamInput{
		ModelId:     aws.String(p.modelID),
		Body:        body,
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return nil, p.mapError(err)
	}

	return p.streamEvents(ctx, out.GetStream()), nil
}

// streamEvents drains a ResponseStreamReader and emits text_delta tokens on
// the returned channel. It is separated from InvokeStream so that tests can
// call it directly with a mock reader.
func (p *Provider) streamEvents(ctx context.Context, reader ResponseStreamReader) <-chan string {
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		defer reader.Close() //nolint:errcheck
		for event := range reader.Events() {
			chunk, ok := event.(*brtypes.ResponseStreamMemberChunk)
			if !ok {
				continue
			}
			var partial struct {
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if jsonErr := json.Unmarshal(chunk.Value.Bytes, &partial); jsonErr != nil {
				continue
			}
			if partial.Delta.Type == "text_delta" && partial.Delta.Text != "" {
				select {
				case <-ctx.Done():
					return
				case ch <- partial.Delta.Text:
				}
			}
		}
	}()
	return ch
}

// mapError converts AWS SDK errors to domain-level errors where appropriate.
func (p *Provider) mapError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "ThrottlingException") || strings.Contains(msg, "ServiceUnavailableException") {
		return &RateLimitError{RetryAfter: 60 * time.Second, Cause: err}
	}
	return fmt.Errorf("bedrock: %w", err)
}

// --- internal serialisation helpers ---

type bedrockRequest struct {
	System    string           `json:"system,omitempty"`
	Messages  []bedrockMessage `json:"messages"`
	MaxTokens int              `json:"max_tokens"`
}

type bedrockMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type bedrockResponse struct {
	Content    []struct{ Text string } `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func marshalRequest(req domain.InvokeRequest) ([]byte, error) {
	msgs := make([]bedrockMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = bedrockMessage{Role: m.Role, Content: m.Content}
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	return json.Marshal(bedrockRequest{
		System:    req.SystemPrompt,
		Messages:  msgs,
		MaxTokens: maxTokens,
	})
}

func unmarshalResponse(body []byte) (domain.InvokeResponse, error) {
	var r bedrockResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return domain.InvokeResponse{}, fmt.Errorf("bedrock: unmarshal response: %w", err)
	}
	content := ""
	if len(r.Content) > 0 {
		content = r.Content[0].Text
	}
	return domain.InvokeResponse{
		Content:    content,
		StopReason: r.StopReason,
		Usage: domain.TokenUsage{
			InputTokens:  r.Usage.InputTokens,
			OutputTokens: r.Usage.OutputTokens,
		},
	}, nil
}
