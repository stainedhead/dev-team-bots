// Package mocks provides hand-written test doubles for the Bedrock
// infrastructure interfaces.
package mocks

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// BedrockRuntimeClient is a hand-written mock of the bedrock.BedrockRuntimeClient interface.
type BedrockRuntimeClient struct {
	InvokeModelFn                      func(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
	InvokeModelWithResponseStreamFn    func(ctx context.Context, params *bedrockruntime.InvokeModelWithResponseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelWithResponseStreamOutput, error)
	InvokeModelCalls                   []*bedrockruntime.InvokeModelInput
	InvokeModelWithResponseStreamCalls []*bedrockruntime.InvokeModelWithResponseStreamInput
}

func (m *BedrockRuntimeClient) InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
	m.InvokeModelCalls = append(m.InvokeModelCalls, params)
	if m.InvokeModelFn != nil {
		return m.InvokeModelFn(ctx, params, optFns...)
	}
	return &bedrockruntime.InvokeModelOutput{}, nil
}

func (m *BedrockRuntimeClient) InvokeModelWithResponseStream(ctx context.Context, params *bedrockruntime.InvokeModelWithResponseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelWithResponseStreamOutput, error) {
	m.InvokeModelWithResponseStreamCalls = append(m.InvokeModelWithResponseStreamCalls, params)
	if m.InvokeModelWithResponseStreamFn != nil {
		return m.InvokeModelWithResponseStreamFn(ctx, params, optFns...)
	}
	return &bedrockruntime.InvokeModelWithResponseStreamOutput{}, nil
}

// StaticResponseStreamReader is a ResponseStreamReader that yields a fixed
// sequence of events and then closes.
type StaticResponseStreamReader struct {
	events chan brtypes.ResponseStream
}

// NewStaticResponseStreamReader creates a reader that will yield the given
// events and then close.
func NewStaticResponseStreamReader(events ...brtypes.ResponseStream) *StaticResponseStreamReader {
	ch := make(chan brtypes.ResponseStream, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return &StaticResponseStreamReader{events: ch}
}

func (r *StaticResponseStreamReader) Events() <-chan brtypes.ResponseStream {
	return r.events
}

func (r *StaticResponseStreamReader) Close() error { return nil }

func (r *StaticResponseStreamReader) Err() error { return nil }
