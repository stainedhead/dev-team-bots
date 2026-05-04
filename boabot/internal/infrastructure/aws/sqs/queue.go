package sqs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

type Queue struct {
	client   *awssqs.Client
	queueURL string
}

func New(cfg aws.Config, queueURL string) *Queue {
	return &Queue{client: awssqs.NewFromConfig(cfg), queueURL: queueURL}
}

func (q *Queue) Send(ctx context.Context, targetQueueURL string, msg domain.Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	_, err = q.client.SendMessage(ctx, &awssqs.SendMessageInput{
		QueueUrl:    aws.String(targetQueueURL),
		MessageBody: aws.String(string(body)),
	})
	return err
}

func (q *Queue) Receive(ctx context.Context) ([]domain.ReceivedMessage, error) {
	out, err := q.client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
		QueueUrl:            aws.String(q.queueURL),
		MaxNumberOfMessages: 10,
		WaitTimeSeconds:     20,
	})
	if err != nil {
		return nil, fmt.Errorf("receive: %w", err)
	}

	msgs := make([]domain.ReceivedMessage, 0, len(out.Messages))
	for _, m := range out.Messages {
		var msg domain.Message
		if err := json.Unmarshal([]byte(aws.ToString(m.Body)), &msg); err != nil {
			continue
		}
		msgs = append(msgs, domain.ReceivedMessage{
			Message:       msg,
			ReceiptHandle: aws.ToString(m.ReceiptHandle),
		})
	}
	return msgs, nil
}

func (q *Queue) Delete(ctx context.Context, receiptHandle string) error {
	_, err := q.client.DeleteMessage(ctx, &awssqs.DeleteMessageInput{
		QueueUrl:      aws.String(q.queueURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
	return err
}
