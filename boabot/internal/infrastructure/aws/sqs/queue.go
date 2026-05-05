// Package sqs provides an SQS-backed implementation of domain.MessageQueue,
// with A2A envelope support, batch operations, and DLQ depth monitoring.
package sqs

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// SQSClient is the subset of the AWS SQS SDK client used by Queue.
// Consumers inject a concrete *awssqs.Client or a test mock.
type SQSClient interface {
	SendMessage(ctx context.Context, params *awssqs.SendMessageInput, optFns ...func(*awssqs.Options)) (*awssqs.SendMessageOutput, error)
	ReceiveMessage(ctx context.Context, params *awssqs.ReceiveMessageInput, optFns ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *awssqs.DeleteMessageInput, optFns ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error)
	DeleteMessageBatch(ctx context.Context, params *awssqs.DeleteMessageBatchInput, optFns ...func(*awssqs.Options)) (*awssqs.DeleteMessageBatchOutput, error)
	GetQueueAttributes(ctx context.Context, params *awssqs.GetQueueAttributesInput, optFns ...func(*awssqs.Options)) (*awssqs.GetQueueAttributesOutput, error)
}

// a2aEnvelope is the wire format wrapped around every outgoing message.
type a2aEnvelope struct {
	A2AVersion    string          `json:"a2a_version"`
	Sender        string          `json:"sender"`
	Recipient     string          `json:"recipient"`
	Type          string          `json:"type"`
	Payload       json.RawMessage `json:"payload"`
	CorrelationID string          `json:"correlation_id"`
	Timestamp     string          `json:"timestamp"`
}

// Queue is an SQS-backed message queue with A2A envelope wrapping.
type Queue struct {
	client   SQSClient
	queueURL string
	dlqURL   string
	botID    string
}

// NewQueue creates a Queue using the supplied SQSClient.
// queueURL is the URL this bot reads from; dlqURL is monitored via DLQDepth.
func NewQueue(client SQSClient, queueURL, dlqURL, botID string) *Queue {
	return &Queue{client: client, queueURL: queueURL, dlqURL: dlqURL, botID: botID}
}

// --- domain.MessageQueue implementation ---

// Send marshals msg into an A2A envelope and publishes it to targetQueueURL.
func (q *Queue) Send(ctx context.Context, targetQueueURL string, msg domain.Message) error {
	body, err := q.wrapEnvelope(msg, "")
	if err != nil {
		return fmt.Errorf("sqs send: wrap envelope: %w", err)
	}
	_, err = q.client.SendMessage(ctx, &awssqs.SendMessageInput{
		QueueUrl:    aws.String(targetQueueURL),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		return fmt.Errorf("sqs send: %w", err)
	}
	return nil
}

// Receive polls the queue and unwraps each A2A envelope.
func (q *Queue) Receive(ctx context.Context) ([]domain.ReceivedMessage, error) {
	return q.ReceiveBatch(ctx, 10)
}

// Delete removes a single message by receipt handle.
func (q *Queue) Delete(ctx context.Context, receiptHandle string) error {
	_, err := q.client.DeleteMessage(ctx, &awssqs.DeleteMessageInput{
		QueueUrl:      aws.String(q.queueURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
	if err != nil {
		return fmt.Errorf("sqs delete: %w", err)
	}
	return nil
}

// --- Extended operations ---

// SendWithAttributes sends msg wrapped in an A2A envelope with additional
// SQS message attributes.
func (q *Queue) SendWithAttributes(ctx context.Context, msg domain.ReceivedMessage, attributes map[string]string) error {
	body, err := q.wrapEnvelope(msg.Message, msg.ReceiptHandle)
	if err != nil {
		return fmt.Errorf("sqs send-with-attrs: wrap envelope: %w", err)
	}
	msgAttrs := make(map[string]types.MessageAttributeValue, len(attributes))
	for k, v := range attributes {
		vCopy := v
		msgAttrs[k] = types.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(vCopy),
		}
	}
	_, err = q.client.SendMessage(ctx, &awssqs.SendMessageInput{
		QueueUrl:          aws.String(q.queueURL),
		MessageBody:       aws.String(string(body)),
		MessageAttributes: msgAttrs,
	})
	if err != nil {
		return fmt.Errorf("sqs send-with-attrs: %w", err)
	}
	return nil
}

// ReceiveBatch polls up to maxMessages (capped at 10) from the queue and
// unwraps each A2A envelope.
func (q *Queue) ReceiveBatch(ctx context.Context, maxMessages int32) ([]domain.ReceivedMessage, error) {
	if maxMessages > 10 {
		maxMessages = 10
	}
	out, err := q.client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
		QueueUrl:            aws.String(q.queueURL),
		MaxNumberOfMessages: maxMessages,
		WaitTimeSeconds:     20,
	})
	if err != nil {
		return nil, fmt.Errorf("sqs receive-batch: %w", err)
	}

	msgs := make([]domain.ReceivedMessage, 0, len(out.Messages))
	for _, m := range out.Messages {
		rm, err := q.unwrapEnvelope(aws.ToString(m.Body), aws.ToString(m.ReceiptHandle))
		if err != nil {
			// Skip malformed messages; they will sit in the queue until
			// visibility timeout and eventually land in the DLQ.
			continue
		}
		msgs = append(msgs, rm)
	}
	return msgs, nil
}

// DeleteBatch removes multiple messages by their receipt handles in a single
// API call. SQS allows at most 10 entries per batch.
func (q *Queue) DeleteBatch(ctx context.Context, handles []string) error {
	entries := make([]types.DeleteMessageBatchRequestEntry, len(handles))
	for i, h := range handles {
		hCopy := h
		id := strconv.Itoa(i)
		entries[i] = types.DeleteMessageBatchRequestEntry{
			Id:            aws.String(id),
			ReceiptHandle: aws.String(hCopy),
		}
	}
	_, err := q.client.DeleteMessageBatch(ctx, &awssqs.DeleteMessageBatchInput{
		QueueUrl: aws.String(q.queueURL),
		Entries:  entries,
	})
	if err != nil {
		return fmt.Errorf("sqs delete-batch: %w", err)
	}
	return nil
}

// DLQDepth returns the approximate number of messages currently in the DLQ.
func (q *Queue) DLQDepth(ctx context.Context) (int64, error) {
	if q.dlqURL == "" {
		return 0, fmt.Errorf("sqs dlq-depth: no DLQ URL configured")
	}
	out, err := q.client.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(q.dlqURL),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameApproximateNumberOfMessages},
	})
	if err != nil {
		return 0, fmt.Errorf("sqs dlq-depth: get attributes: %w", err)
	}
	raw, ok := out.Attributes[string(types.QueueAttributeNameApproximateNumberOfMessages)]
	if !ok {
		return 0, nil
	}
	depth, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("sqs dlq-depth: parse depth: %w", err)
	}
	return depth, nil
}

// --- helpers ---

func (q *Queue) wrapEnvelope(msg domain.Message, correlationID string) ([]byte, error) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	env := a2aEnvelope{
		A2AVersion:    "1.0",
		Sender:        q.botID,
		Recipient:     msg.To,
		Type:          string(msg.Type),
		Payload:       json.RawMessage(payload),
		CorrelationID: correlationID,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
	return json.Marshal(env)
}

func (q *Queue) unwrapEnvelope(body, receiptHandle string) (domain.ReceivedMessage, error) {
	var env a2aEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		return domain.ReceivedMessage{}, fmt.Errorf("unmarshal envelope: %w", err)
	}
	var msg domain.Message
	if err := json.Unmarshal(env.Payload, &msg); err != nil {
		return domain.ReceivedMessage{}, fmt.Errorf("unmarshal payload: %w", err)
	}
	return domain.ReceivedMessage{
		Message:       msg,
		ReceiptHandle: receiptHandle,
	}, nil
}
