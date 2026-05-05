package bedrock

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

type Provider struct {
	client  *bedrockruntime.Client
	modelID string
}

func New(cfg aws.Config, modelID string) *Provider {
	return &Provider{client: bedrockruntime.NewFromConfig(cfg), modelID: modelID}
}

func (p *Provider) Invoke(ctx context.Context, req domain.InvokeRequest) (domain.InvokeResponse, error) {
	body, err := marshalRequest(req)
	if err != nil {
		return domain.InvokeResponse{}, fmt.Errorf("marshal bedrock request: %w", err)
	}

	out, err := p.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(p.modelID),
		Body:        body,
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return domain.InvokeResponse{}, fmt.Errorf("bedrock invoke: %w", err)
	}

	return unmarshalResponse(out.Body)
}

type bedrockRequest struct {
	System    string           `json:"system,omitempty"`
	Messages  []bedrockMessage `json:"messages"`
	MaxTokens int              `json:"max_tokens"`
}

type bedrockMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type bedrockResponse struct {
	Content    []struct{ Text string } `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func marshalRequest(req domain.InvokeRequest) ([]byte, error) {
	msgs := make([]bedrockMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = bedrockMessage{Role: m.Role, Content: m.Content}
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	return json.Marshal(bedrockRequest{
		System:    req.SystemPrompt,
		Messages:  msgs,
		MaxTokens: maxTokens,
	})
}

func unmarshalResponse(body []byte) (domain.InvokeResponse, error) {
	var r bedrockResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return domain.InvokeResponse{}, fmt.Errorf("unmarshal bedrock response: %w", err)
	}
	content := ""
	if len(r.Content) > 0 {
		content = r.Content[0].Text
	}
	return domain.InvokeResponse{
		Content:    content,
		StopReason: r.StopReason,
		Usage: domain.TokenUsage{
			InputTokens:  r.Usage.InputTokens,
			OutputTokens: r.Usage.OutputTokens,
		},
	}, nil
}
