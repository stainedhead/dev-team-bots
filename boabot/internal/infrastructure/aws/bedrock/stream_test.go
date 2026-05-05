// Package bedrock (white-box tests for the streaming pipeline).
package bedrock

import (
	"context"
	"encoding/json"
	"testing"

	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/aws/bedrock/mocks"
)

func makeChunkBytesInternal(text string) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"delta": map[string]string{
			"type": "text_delta",
			"text": text,
		},
	})
	return b
}

func TestStreamEvents_EmitsTokens(t *testing.T) {
	reader := mocks.NewStaticResponseStreamReader(
		&brtypes.ResponseStreamMemberChunk{Value: brtypes.PayloadPart{Bytes: makeChunkBytesInternal("Hello")}},
		&brtypes.ResponseStreamMemberChunk{Value: brtypes.PayloadPart{Bytes: makeChunkBytesInternal(" world")}},
	)

	p := &Provider{}
	ch := p.streamEvents(context.Background(), reader)

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d: %v", len(tokens), tokens)
	}
	if tokens[0] != "Hello" || tokens[1] != " world" {
		t.Errorf("unexpected tokens: %v", tokens)
	}
}

func TestStreamEvents_EmptyReader(t *testing.T) {
	reader := mocks.NewStaticResponseStreamReader()
	p := &Provider{}
	ch := p.streamEvents(context.Background(), reader)

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(tokens))
	}
}

func TestStreamEvents_SkipsMalformedChunks(t *testing.T) {
	reader := mocks.NewStaticResponseStreamReader(
		&brtypes.ResponseStreamMemberChunk{Value: brtypes.PayloadPart{Bytes: []byte("not-json")}},
		&brtypes.ResponseStreamMemberChunk{Value: brtypes.PayloadPart{Bytes: makeChunkBytesInternal("good")}},
	)
	p := &Provider{}
	ch := p.streamEvents(context.Background(), reader)

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}
	if len(tokens) != 1 || tokens[0] != "good" {
		t.Errorf("expected only 'good' token, got: %v", tokens)
	}
}

func TestStreamEvents_SkipsNonTextDelta(t *testing.T) {
	nonText, _ := json.Marshal(map[string]interface{}{
		"delta": map[string]string{
			"type": "input_json_delta",
			"text": "ignored",
		},
	})
	reader := mocks.NewStaticResponseStreamReader(
		&brtypes.ResponseStreamMemberChunk{Value: brtypes.PayloadPart{Bytes: nonText}},
		&brtypes.ResponseStreamMemberChunk{Value: brtypes.PayloadPart{Bytes: makeChunkBytesInternal("real")}},
	)
	p := &Provider{}
	ch := p.streamEvents(context.Background(), reader)

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}
	if len(tokens) != 1 || tokens[0] != "real" {
		t.Errorf("expected only 'real' token, got: %v", tokens)
	}
}

func TestStreamEvents_SkipsUnknownEventTypes(t *testing.T) {
	reader := mocks.NewStaticResponseStreamReader(
		// UnknownUnionMember satisfies ResponseStream but is not a chunk
		&brtypes.UnknownUnionMember{Tag: "unknown"},
	)
	p := &Provider{}
	ch := p.streamEvents(context.Background(), reader)

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(tokens))
	}
}

func TestStreamEvents_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	reader := mocks.NewStaticResponseStreamReader(
		&brtypes.ResponseStreamMemberChunk{Value: brtypes.PayloadPart{Bytes: makeChunkBytesInternal("dropped")}},
	)
	p := &Provider{}
	ch := p.streamEvents(ctx, reader)

	// Channel should close quickly; we may or may not receive the token
	for range ch {
		// drain
	}
}

func TestRateLimitError_ErrorAndUnwrap(t *testing.T) {
	import_err := &RateLimitError{RetryAfter: 30, Cause: context.DeadlineExceeded}
	if import_err.Error() == "" {
		t.Error("expected non-empty error string")
	}
	if import_err.Unwrap() != context.DeadlineExceeded {
		t.Error("Unwrap should return the cause")
	}
}

func TestMapError_NilReturnsNil(t *testing.T) {
	p := &Provider{}
	err := p.mapError(nil)
	if err != nil {
		t.Errorf("expected nil for nil input, got: %v", err)
	}
}

func TestUnmarshalResponse_EmptyContent(t *testing.T) {
	// Zero-content response should still parse without error.
	body := []byte(`{"content":[],"stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}}`)
	resp, err := unmarshalResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("expected end_turn, got %q", resp.StopReason)
	}
}

func TestUnmarshalResponse_InvalidJSON(t *testing.T) {
	_, err := unmarshalResponse([]byte("not-json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
