package sqs_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/aws/sqs"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/aws/sqs/mocks"
)

const (
	testQueueURL = "https://sqs.us-east-1.amazonaws.com/123456789/test-queue"
	testDLQURL   = "https://sqs.us-east-1.amazonaws.com/123456789/test-dlq"
	testBotID    = "bao-orchestrator"
)

func newTestQueue(client *mocks.SQSClient) *sqs.Queue {
	return sqs.NewQueue(client, testQueueURL, testDLQURL, testBotID)
}

func buildEnvelopeBody(t *testing.T, msg domain.Message) string {
	t.Helper()
	payload, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	type env struct {
		A2AVersion    string          `json:"a2a_version"`
		Sender        string          `json:"sender"`
		Recipient     string          `json:"recipient"`
		Type          string          `json:"type"`
		Payload       json.RawMessage `json:"payload"`
		CorrelationID string          `json:"correlation_id"`
		Timestamp     string          `json:"timestamp"`
	}
	e := env{
		A2AVersion: "1.0",
		Sender:     testBotID,
		Recipient:  msg.To,
		Type:       string(msg.Type),
		Payload:    json.RawMessage(payload),
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Send
// ---------------------------------------------------------------------------

func TestQueue_Send_WrapsA2AEnvelope(t *testing.T) {
	mock := &mocks.SQSClient{}
	q := newTestQueue(mock)

	msg := domain.Message{
		ID:   "msg-1",
		Type: domain.MessageTypeTask,
		From: testBotID,
		To:   "bao-coder",
	}

	err := q.Send(context.Background(), testQueueURL, msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.SendMessageCalls) != 1 {
		t.Fatalf("expected 1 SendMessage call, got %d", len(mock.SendMessageCalls))
	}

	body := aws.ToString(mock.SendMessageCalls[0].MessageBody)
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("invalid envelope JSON: %v", err)
	}
	if string(env["a2a_version"]) != `"1.0"` {
		t.Errorf("expected a2a_version 1.0, got %s", env["a2a_version"])
	}
	if string(env["sender"]) != `"`+testBotID+`"` {
		t.Errorf("expected sender %q, got %s", testBotID, env["sender"])
	}
	if string(env["recipient"]) != `"bao-coder"` {
		t.Errorf("expected recipient bao-coder, got %s", env["recipient"])
	}
	if string(env["type"]) != `"task"` {
		t.Errorf("expected type task, got %s", env["type"])
	}
}

func TestQueue_Send_ClientError(t *testing.T) {
	sentinel := errors.New("sqs down")
	mock := &mocks.SQSClient{
		SendMessageFn: func(_ context.Context, _ *awssqs.SendMessageInput, _ ...func(*awssqs.Options)) (*awssqs.SendMessageOutput, error) {
			return nil, sentinel
		},
	}
	q := newTestQueue(mock)
	err := q.Send(context.Background(), testQueueURL, domain.Message{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Receive / ReceiveBatch
// ---------------------------------------------------------------------------

func TestQueue_Receive_UnwrapsA2AEnvelope(t *testing.T) {
	msg := domain.Message{
		ID:   "msg-42",
		Type: domain.MessageTypeHeartbeat,
		From: "bao-coder",
		To:   testBotID,
	}
	body := buildEnvelopeBody(t, msg)

	mock := &mocks.SQSClient{
		ReceiveMessageFn: func(_ context.Context, _ *awssqs.ReceiveMessageInput, _ ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error) {
			return &awssqs.ReceiveMessageOutput{
				Messages: []sqstypes.Message{
					{Body: aws.String(body), ReceiptHandle: aws.String("rh-1")},
				},
			}, nil
		},
	}
	q := newTestQueue(mock)
	msgs, err := q.Receive(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Message.ID != "msg-42" {
		t.Errorf("expected ID msg-42, got %s", msgs[0].Message.ID)
	}
	if msgs[0].ReceiptHandle != "rh-1" {
		t.Errorf("expected receipt handle rh-1, got %s", msgs[0].ReceiptHandle)
	}
}

func TestQueue_Receive_SkipsMalformedMessages(t *testing.T) {
	mock := &mocks.SQSClient{
		ReceiveMessageFn: func(_ context.Context, _ *awssqs.ReceiveMessageInput, _ ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error) {
			return &awssqs.ReceiveMessageOutput{
				Messages: []sqstypes.Message{
					{Body: aws.String("not-json"), ReceiptHandle: aws.String("rh-bad")},
				},
			}, nil
		},
	}
	q := newTestQueue(mock)
	msgs, err := q.Receive(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages (malformed skipped), got %d", len(msgs))
	}
}

func TestQueue_ReceiveBatch_CapsAt10(t *testing.T) {
	mock := &mocks.SQSClient{}
	q := newTestQueue(mock)
	_, _ = q.ReceiveBatch(context.Background(), 50)
	if mock.ReceiveMessageCalls[0].MaxNumberOfMessages != 10 {
		t.Errorf("expected MaxNumberOfMessages=10, got %d", mock.ReceiveMessageCalls[0].MaxNumberOfMessages)
	}
}

func TestQueue_ReceiveBatch_ClientError(t *testing.T) {
	sentinel := errors.New("receive failed")
	mock := &mocks.SQSClient{
		ReceiveMessageFn: func(_ context.Context, _ *awssqs.ReceiveMessageInput, _ ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error) {
			return nil, sentinel
		},
	}
	q := newTestQueue(mock)
	_, err := q.ReceiveBatch(context.Background(), 5)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DeleteBatch
// ---------------------------------------------------------------------------

func TestQueue_DeleteBatch_SendsCorrectEntries(t *testing.T) {
	mock := &mocks.SQSClient{}
	q := newTestQueue(mock)

	handles := []string{"rh-a", "rh-b", "rh-c"}
	err := q.DeleteBatch(context.Background(), handles)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.DeleteMessageBatchCalls) != 1 {
		t.Fatalf("expected 1 DeleteMessageBatch call, got %d", len(mock.DeleteMessageBatchCalls))
	}
	entries := mock.DeleteMessageBatchCalls[0].Entries
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
	for i, e := range entries {
		if aws.ToString(e.ReceiptHandle) != handles[i] {
			t.Errorf("entry %d: expected handle %q, got %q", i, handles[i], aws.ToString(e.ReceiptHandle))
		}
	}
}

func TestQueue_DeleteBatch_ClientError(t *testing.T) {
	sentinel := errors.New("delete batch failed")
	mock := &mocks.SQSClient{
		DeleteMessageBatchFn: func(_ context.Context, _ *awssqs.DeleteMessageBatchInput, _ ...func(*awssqs.Options)) (*awssqs.DeleteMessageBatchOutput, error) {
			return nil, sentinel
		},
	}
	q := newTestQueue(mock)
	err := q.DeleteBatch(context.Background(), []string{"rh-x"})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DLQDepth
// ---------------------------------------------------------------------------

func TestQueue_DLQDepth_ReturnsCount(t *testing.T) {
	mock := &mocks.SQSClient{
		GetQueueAttributesFn: func(_ context.Context, _ *awssqs.GetQueueAttributesInput, _ ...func(*awssqs.Options)) (*awssqs.GetQueueAttributesOutput, error) {
			return &awssqs.GetQueueAttributesOutput{
				Attributes: map[string]string{
					"ApproximateNumberOfMessages": "7",
				},
			}, nil
		},
	}
	q := newTestQueue(mock)
	depth, err := q.DLQDepth(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if depth != 7 {
		t.Errorf("expected depth 7, got %d", depth)
	}
	// Verify correct queue URL was passed
	if aws.ToString(mock.GetQueueAttributesCalls[0].QueueUrl) != testDLQURL {
		t.Errorf("expected DLQ URL %s, got %s", testDLQURL, aws.ToString(mock.GetQueueAttributesCalls[0].QueueUrl))
	}
}

func TestQueue_DLQDepth_NoDLQURL(t *testing.T) {
	mock := &mocks.SQSClient{}
	q := sqs.NewQueue(mock, testQueueURL, "", testBotID)
	_, err := q.DLQDepth(context.Background())
	if err == nil {
		t.Fatal("expected error when no DLQ URL configured")
	}
}

func TestQueue_DLQDepth_ClientError(t *testing.T) {
	sentinel := errors.New("get attributes failed")
	mock := &mocks.SQSClient{
		GetQueueAttributesFn: func(_ context.Context, _ *awssqs.GetQueueAttributesInput, _ ...func(*awssqs.Options)) (*awssqs.GetQueueAttributesOutput, error) {
			return nil, sentinel
		},
	}
	q := newTestQueue(mock)
	_, err := q.DLQDepth(context.Background())
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

func TestQueue_DLQDepth_MissingAttribute(t *testing.T) {
	mock := &mocks.SQSClient{
		GetQueueAttributesFn: func(_ context.Context, _ *awssqs.GetQueueAttributesInput, _ ...func(*awssqs.Options)) (*awssqs.GetQueueAttributesOutput, error) {
			return &awssqs.GetQueueAttributesOutput{
				Attributes: map[string]string{},
			}, nil
		},
	}
	q := newTestQueue(mock)
	depth, err := q.DLQDepth(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if depth != 0 {
		t.Errorf("expected depth 0 when attribute absent, got %d", depth)
	}
}

// ---------------------------------------------------------------------------
// SendWithAttributes
// ---------------------------------------------------------------------------

func TestQueue_SendWithAttributes_IncludesAttributes(t *testing.T) {
	mock := &mocks.SQSClient{}
	q := newTestQueue(mock)

	msg := domain.ReceivedMessage{
		Message:       domain.Message{ID: "msg-3", Type: domain.MessageTypeTask, From: testBotID, To: "bao-qa"},
		ReceiptHandle: "rh-orig",
	}
	attrs := map[string]string{"priority": "high", "source": "orchestrator"}

	err := q.SendWithAttributes(context.Background(), msg, attrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.SendMessageCalls) != 1 {
		t.Fatalf("expected 1 SendMessage call, got %d", len(mock.SendMessageCalls))
	}
	call := mock.SendMessageCalls[0]
	if len(call.MessageAttributes) != 2 {
		t.Errorf("expected 2 message attributes, got %d", len(call.MessageAttributes))
	}
	if aws.ToString(call.MessageAttributes["priority"].StringValue) != "high" {
		t.Errorf("expected priority=high, got %s", aws.ToString(call.MessageAttributes["priority"].StringValue))
	}
}

func TestQueue_Delete_CallsClient(t *testing.T) {
	mock := &mocks.SQSClient{}
	q := newTestQueue(mock)
	err := q.Delete(context.Background(), "rh-del")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.DeleteMessageCalls) != 1 {
		t.Fatalf("expected 1 DeleteMessage call, got %d", len(mock.DeleteMessageCalls))
	}
	if aws.ToString(mock.DeleteMessageCalls[0].ReceiptHandle) != "rh-del" {
		t.Errorf("expected receipt handle rh-del")
	}
}

func TestQueue_Delete_ClientError(t *testing.T) {
	sentinel := errors.New("delete failed")
	mock := &mocks.SQSClient{
		DeleteMessageFn: func(_ context.Context, _ *awssqs.DeleteMessageInput, _ ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error) {
			return nil, sentinel
		},
	}
	q := newTestQueue(mock)
	err := q.Delete(context.Background(), "rh-del")
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

func TestQueue_SendWithAttributes_ClientError(t *testing.T) {
	sentinel := errors.New("send failed")
	mock := &mocks.SQSClient{
		SendMessageFn: func(_ context.Context, _ *awssqs.SendMessageInput, _ ...func(*awssqs.Options)) (*awssqs.SendMessageOutput, error) {
			return nil, sentinel
		},
	}
	q := newTestQueue(mock)
	msg := domain.ReceivedMessage{
		Message: domain.Message{ID: "msg-err", Type: domain.MessageTypeTask},
	}
	err := q.SendWithAttributes(context.Background(), msg, map[string]string{})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

func TestQueue_DLQDepth_InvalidAttributeValue(t *testing.T) {
	mock := &mocks.SQSClient{
		GetQueueAttributesFn: func(_ context.Context, _ *awssqs.GetQueueAttributesInput, _ ...func(*awssqs.Options)) (*awssqs.GetQueueAttributesOutput, error) {
			return &awssqs.GetQueueAttributesOutput{
				Attributes: map[string]string{
					"ApproximateNumberOfMessages": "not-a-number",
				},
			}, nil
		},
	}
	q := newTestQueue(mock)
	_, err := q.DLQDepth(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid attribute value")
	}
}
