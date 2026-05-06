//go:build integration

package anthropic_test

import (
	"context"
	"os"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	anthropicpkg "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/anthropic"
)

func TestProvider_Integration_RealAPI(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skipf("ANTHROPIC_API_KEY not set; skipping integration test")
	}

	p, err := anthropicpkg.NewFromEnv("claude-haiku-4-5")
	if err != nil {
		t.Fatalf("NewFromEnv: %v", err)
	}

	req := domain.InvokeRequest{
		SystemPrompt: "You are a helpful assistant. Be brief.",
		Messages: []domain.ProviderMessage{
			{Role: "user", Content: "Say exactly: 'integration test ok'"},
		},
		MaxTokens: 64,
	}

	resp, err := p.Invoke(context.Background(), req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.Content == "" {
		t.Error("expected non-empty content from real API")
	}
	if resp.StopReason == "" {
		t.Error("expected non-empty stop reason")
	}
	t.Logf("response: %q (stop=%s, in=%d, out=%d)",
		resp.Content, resp.StopReason, resp.Usage.InputTokens, resp.Usage.OutputTokens)
}
