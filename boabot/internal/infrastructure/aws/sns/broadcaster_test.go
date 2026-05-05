package sns_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/aws/sns"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/aws/sns/mocks"
)

const testTopicARN = "arn:aws:sns:us-east-1:123456789:baobot-events"

func newTestBroadcaster(client *mocks.SNSClient) *sns.Broadcaster {
	return sns.NewBroadcaster(client, testTopicARN)
}

// ---------------------------------------------------------------------------
// Broadcast
// ---------------------------------------------------------------------------

func TestBroadcaster_Broadcast_PublishesJSON(t *testing.T) {
	mock := &mocks.SNSClient{}
	b := newTestBroadcaster(mock)

	msg := domain.Message{
		ID:   "msg-1",
		Type: domain.MessageTypeTask,
		From: "bao-orchestrator",
		To:   "bao-coder",
	}

	err := b.Broadcast(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.PublishCalls) != 1 {
		t.Fatalf("expected 1 Publish call, got %d", len(mock.PublishCalls))
	}

	call := mock.PublishCalls[0]
	if aws.ToString(call.TopicArn) != testTopicARN {
		t.Errorf("expected topicARN %s, got %s", testTopicARN, aws.ToString(call.TopicArn))
	}

	var decoded domain.Message
	if err := json.Unmarshal([]byte(aws.ToString(call.Message)), &decoded); err != nil {
		t.Fatalf("message body is not valid domain.Message JSON: %v", err)
	}
	if decoded.ID != "msg-1" {
		t.Errorf("expected ID msg-1, got %s", decoded.ID)
	}
}

func TestBroadcaster_Broadcast_ClientError(t *testing.T) {
	sentinel := errors.New("sns down")
	mock := &mocks.SNSClient{
		PublishFn: func(_ context.Context, _ *awssns.PublishInput, _ ...func(*awssns.Options)) (*awssns.PublishOutput, error) {
			return nil, sentinel
		},
	}
	b := newTestBroadcaster(mock)
	err := b.Broadcast(context.Background(), domain.Message{})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PublishNotification
// ---------------------------------------------------------------------------

func TestBroadcaster_PublishNotification_SetsSubjectAndAttributes(t *testing.T) {
	mock := &mocks.SNSClient{}
	b := newTestBroadcaster(mock)

	n := notification.Notification{
		Type:    notification.NotifCostSpike,
		Subject: "Cost Spike Detected",
		Body:    "Bot bao-coder exceeded spike threshold",
		Metadata: map[string]string{
			"bot_id": "bao-coder",
		},
	}

	err := b.PublishNotification(context.Background(), n)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.PublishCalls) != 1 {
		t.Fatalf("expected 1 Publish call, got %d", len(mock.PublishCalls))
	}

	call := mock.PublishCalls[0]
	if aws.ToString(call.Subject) != "Cost Spike Detected" {
		t.Errorf("expected subject 'Cost Spike Detected', got %q", aws.ToString(call.Subject))
	}
	if aws.ToString(call.Message) != n.Body {
		t.Errorf("expected body %q, got %q", n.Body, aws.ToString(call.Message))
	}

	notifType, ok := call.MessageAttributes["notif_type"]
	if !ok {
		t.Fatal("expected notif_type attribute")
	}
	if aws.ToString(notifType.StringValue) != string(notification.NotifCostSpike) {
		t.Errorf("expected notif_type %q, got %q", notification.NotifCostSpike, aws.ToString(notifType.StringValue))
	}

	botAttr, ok := call.MessageAttributes["bot_id"]
	if !ok {
		t.Fatal("expected bot_id attribute")
	}
	if aws.ToString(botAttr.StringValue) != "bao-coder" {
		t.Errorf("expected bot_id bao-coder, got %q", aws.ToString(botAttr.StringValue))
	}
}

func TestBroadcaster_PublishNotification_UsesRecipientARNWhenSet(t *testing.T) {
	mock := &mocks.SNSClient{}
	b := newTestBroadcaster(mock)

	customARN := "arn:aws:sns:us-east-1:123456789:ops-alerts"
	n := notification.Notification{
		Type:         notification.NotifFailure,
		RecipientARN: customARN,
		Subject:      "Failure",
		Body:         "something failed",
	}

	err := b.PublishNotification(context.Background(), n)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	call := mock.PublishCalls[0]
	if aws.ToString(call.TopicArn) != customARN {
		t.Errorf("expected topic ARN %s, got %s", customARN, aws.ToString(call.TopicArn))
	}
}

func TestBroadcaster_PublishNotification_UsesDefaultTopicWhenNoRecipient(t *testing.T) {
	mock := &mocks.SNSClient{}
	b := newTestBroadcaster(mock)

	n := notification.Notification{
		Type:    notification.NotifSuccess,
		Subject: "Done",
		Body:    "task complete",
	}

	err := b.PublishNotification(context.Background(), n)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aws.ToString(mock.PublishCalls[0].TopicArn) != testTopicARN {
		t.Errorf("expected default topicARN, got %s", aws.ToString(mock.PublishCalls[0].TopicArn))
	}
}

func TestBroadcaster_PublishNotification_ClientError(t *testing.T) {
	sentinel := errors.New("publish failed")
	mock := &mocks.SNSClient{
		PublishFn: func(_ context.Context, _ *awssns.PublishInput, _ ...func(*awssns.Options)) (*awssns.PublishOutput, error) {
			return nil, sentinel
		},
	}
	b := newTestBroadcaster(mock)
	err := b.PublishNotification(context.Background(), notification.Notification{})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PublishViabilityReport
// ---------------------------------------------------------------------------

func TestBroadcaster_PublishViabilityReport_SetsReportTypeAttribute(t *testing.T) {
	mock := &mocks.SNSClient{}
	b := newTestBroadcaster(mock)

	report := metrics.ViabilityReport{
		Period:      metrics.DateRange{Start: "2026-05-01", End: "2026-05-05"},
		GeneratedAt: time.Now().UTC(),
		BotReports: []metrics.BotReport{
			{BotID: "bao-orchestrator", Throughput: 10, CostPerTask: 0.05},
		},
	}

	err := b.PublishViabilityReport(context.Background(), report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.PublishCalls) != 1 {
		t.Fatalf("expected 1 Publish call, got %d", len(mock.PublishCalls))
	}

	call := mock.PublishCalls[0]
	if aws.ToString(call.Subject) != "ViabilityReport" {
		t.Errorf("expected subject ViabilityReport, got %q", aws.ToString(call.Subject))
	}

	rtAttr, ok := call.MessageAttributes["report_type"]
	if !ok {
		t.Fatal("expected report_type attribute")
	}
	if aws.ToString(rtAttr.StringValue) != "viability_report" {
		t.Errorf("expected report_type=viability_report, got %q", aws.ToString(rtAttr.StringValue))
	}

	// Verify the body is valid JSON of the report
	var decoded metrics.ViabilityReport
	if err := json.Unmarshal([]byte(aws.ToString(call.Message)), &decoded); err != nil {
		t.Fatalf("message body is not valid ViabilityReport JSON: %v", err)
	}
	if len(decoded.BotReports) != 1 {
		t.Errorf("expected 1 bot report, got %d", len(decoded.BotReports))
	}
}

func TestBroadcaster_PublishViabilityReport_ClientError(t *testing.T) {
	sentinel := errors.New("publish failed")
	mock := &mocks.SNSClient{
		PublishFn: func(_ context.Context, _ *awssns.PublishInput, _ ...func(*awssns.Options)) (*awssns.PublishOutput, error) {
			return nil, sentinel
		},
	}
	b := newTestBroadcaster(mock)
	err := b.PublishViabilityReport(context.Background(), metrics.ViabilityReport{})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}
