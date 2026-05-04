package sns

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

type Broadcaster struct {
	client   *awssns.Client
	topicARN string
}

func New(cfg aws.Config, topicARN string) *Broadcaster {
	return &Broadcaster{client: awssns.NewFromConfig(cfg), topicARN: topicARN}
}

func (b *Broadcaster) Broadcast(ctx context.Context, msg domain.Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	_, err = b.client.Publish(ctx, &awssns.PublishInput{
		TopicArn: aws.String(b.topicARN),
		Message:  aws.String(string(body)),
	})
	return err
}
