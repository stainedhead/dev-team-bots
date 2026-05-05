// Package mocks provides hand-written test doubles for the SQS infrastructure
// interfaces.
package mocks

import (
	"context"

	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
)

// SQSClient is a hand-written mock of the sqs.SQSClient interface.
type SQSClient struct {
	SendMessageFn        func(ctx context.Context, params *awssqs.SendMessageInput, optFns ...func(*awssqs.Options)) (*awssqs.SendMessageOutput, error)
	ReceiveMessageFn     func(ctx context.Context, params *awssqs.ReceiveMessageInput, optFns ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error)
	DeleteMessageFn      func(ctx context.Context, params *awssqs.DeleteMessageInput, optFns ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error)
	DeleteMessageBatchFn func(ctx context.Context, params *awssqs.DeleteMessageBatchInput, optFns ...func(*awssqs.Options)) (*awssqs.DeleteMessageBatchOutput, error)
	GetQueueAttributesFn func(ctx context.Context, params *awssqs.GetQueueAttributesInput, optFns ...func(*awssqs.Options)) (*awssqs.GetQueueAttributesOutput, error)

	SendMessageCalls        []*awssqs.SendMessageInput
	ReceiveMessageCalls     []*awssqs.ReceiveMessageInput
	DeleteMessageCalls      []*awssqs.DeleteMessageInput
	DeleteMessageBatchCalls []*awssqs.DeleteMessageBatchInput
	GetQueueAttributesCalls []*awssqs.GetQueueAttributesInput
}

func (m *SQSClient) SendMessage(ctx context.Context, params *awssqs.SendMessageInput, optFns ...func(*awssqs.Options)) (*awssqs.SendMessageOutput, error) {
	m.SendMessageCalls = append(m.SendMessageCalls, params)
	if m.SendMessageFn != nil {
		return m.SendMessageFn(ctx, params, optFns...)
	}
	return &awssqs.SendMessageOutput{}, nil
}

func (m *SQSClient) ReceiveMessage(ctx context.Context, params *awssqs.ReceiveMessageInput, optFns ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error) {
	m.ReceiveMessageCalls = append(m.ReceiveMessageCalls, params)
	if m.ReceiveMessageFn != nil {
		return m.ReceiveMessageFn(ctx, params, optFns...)
	}
	return &awssqs.ReceiveMessageOutput{}, nil
}

func (m *SQSClient) DeleteMessage(ctx context.Context, params *awssqs.DeleteMessageInput, optFns ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error) {
	m.DeleteMessageCalls = append(m.DeleteMessageCalls, params)
	if m.DeleteMessageFn != nil {
		return m.DeleteMessageFn(ctx, params, optFns...)
	}
	return &awssqs.DeleteMessageOutput{}, nil
}

func (m *SQSClient) DeleteMessageBatch(ctx context.Context, params *awssqs.DeleteMessageBatchInput, optFns ...func(*awssqs.Options)) (*awssqs.DeleteMessageBatchOutput, error) {
	m.DeleteMessageBatchCalls = append(m.DeleteMessageBatchCalls, params)
	if m.DeleteMessageBatchFn != nil {
		return m.DeleteMessageBatchFn(ctx, params, optFns...)
	}
	return &awssqs.DeleteMessageBatchOutput{}, nil
}

func (m *SQSClient) GetQueueAttributes(ctx context.Context, params *awssqs.GetQueueAttributesInput, optFns ...func(*awssqs.Options)) (*awssqs.GetQueueAttributesOutput, error) {
	m.GetQueueAttributesCalls = append(m.GetQueueAttributesCalls, params)
	if m.GetQueueAttributesFn != nil {
		return m.GetQueueAttributesFn(ctx, params, optFns...)
	}
	return &awssqs.GetQueueAttributesOutput{}, nil
}
