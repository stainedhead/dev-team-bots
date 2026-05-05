// Package mocks provides hand-written test doubles for the SNS infrastructure
// interfaces.
package mocks

import (
	"context"

	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
)

// SNSClient is a hand-written mock of the sns.SNSClient interface.
type SNSClient struct {
	PublishFn    func(ctx context.Context, params *awssns.PublishInput, optFns ...func(*awssns.Options)) (*awssns.PublishOutput, error)
	PublishCalls []*awssns.PublishInput
}

func (m *SNSClient) Publish(ctx context.Context, params *awssns.PublishInput, optFns ...func(*awssns.Options)) (*awssns.PublishOutput, error) {
	m.PublishCalls = append(m.PublishCalls, params)
	if m.PublishFn != nil {
		return m.PublishFn(ctx, params, optFns...)
	}
	return &awssns.PublishOutput{}, nil
}
