// Package sns provides an SNS-backed implementation of domain.Broadcaster,
// with extended support for operational notifications and viability reports.
package sns

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/notification"
)

// SNSClient is the subset of the AWS SNS SDK client used by Broadcaster.
// Consumers inject a concrete *awssns.Client or a test mock.
type SNSClient interface {
	Publish(ctx context.Context, params *awssns.PublishInput, optFns ...func(*awssns.Options)) (*awssns.PublishOutput, error)
}

// Broadcaster is an SNS-backed broadcaster implementing domain.Broadcaster and
// providing additional publish methods for notifications and viability reports.
type Broadcaster struct {
	client   SNSClient
	topicARN string
}

// NewBroadcaster creates a Broadcaster using the supplied SNSClient.
func NewBroadcaster(client SNSClient, topicARN string) *Broadcaster {
	return &Broadcaster{client: client, topicARN: topicARN}
}

// Broadcast marshals msg to JSON and publishes it to the configured SNS topic.
// This satisfies domain.Broadcaster.
func (b *Broadcaster) Broadcast(ctx context.Context, msg domain.Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("sns broadcast: marshal message: %w", err)
	}
	_, err = b.client.Publish(ctx, &awssns.PublishInput{
		TopicArn: aws.String(b.topicARN),
		Message:  aws.String(string(body)),
	})
	if err != nil {
		return fmt.Errorf("sns broadcast: %w", err)
	}
	return nil
}

// PublishNotification maps a domain Notification to an SNS Publish call,
// setting Subject and MessageAttributes for downstream filtering.
func (b *Broadcaster) PublishNotification(ctx context.Context, n notification.Notification) error {
	attrs := make(map[string]snstypes.MessageAttributeValue, len(n.Metadata)+1)
	attrs["notif_type"] = snstypes.MessageAttributeValue{
		DataType:    aws.String("String"),
		StringValue: aws.String(string(n.Type)),
	}
	for k, v := range n.Metadata {
		vCopy := v
		attrs[k] = snstypes.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(vCopy),
		}
	}

	topic := b.topicARN
	if n.RecipientARN != "" {
		topic = n.RecipientARN
	}

	_, err := b.client.Publish(ctx, &awssns.PublishInput{
		TopicArn:          aws.String(topic),
		Subject:           aws.String(n.Subject),
		Message:           aws.String(n.Body),
		MessageAttributes: attrs,
	})
	if err != nil {
		return fmt.Errorf("sns publish-notification: %w", err)
	}
	return nil
}

// PublishViabilityReport serialises a ViabilityReport to JSON and publishes
// it to the configured SNS topic with a report_type attribute.
func (b *Broadcaster) PublishViabilityReport(ctx context.Context, report metrics.ViabilityReport) error {
	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("sns publish-viability-report: marshal: %w", err)
	}
	_, err = b.client.Publish(ctx, &awssns.PublishInput{
		TopicArn: aws.String(b.topicARN),
		Subject:  aws.String("ViabilityReport"),
		Message:  aws.String(string(body)),
		MessageAttributes: map[string]snstypes.MessageAttributeValue{
			"report_type": {
				DataType:    aws.String("String"),
				StringValue: aws.String("viability_report"),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("sns publish-viability-report: %w", err)
	}
	return nil
}
