// Package mocks provides hand-written test doubles for the Anthropic
// infrastructure interfaces.
package mocks

import (
	"context"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// MessagesClient is a hand-written mock of the anthropic.MessagesClient interface.
type MessagesClient struct {
	NewFn    func(ctx context.Context, params anthropicsdk.MessageNewParams, opts ...option.RequestOption) (*anthropicsdk.Message, error)
	NewCalls []anthropicsdk.MessageNewParams
}

func (m *MessagesClient) New(ctx context.Context, params anthropicsdk.MessageNewParams, opts ...option.RequestOption) (*anthropicsdk.Message, error) {
	m.NewCalls = append(m.NewCalls, params)
	if m.NewFn != nil {
		return m.NewFn(ctx, params, opts...)
	}
	return &anthropicsdk.Message{}, nil
}
