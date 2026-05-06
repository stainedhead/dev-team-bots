package team_test

import (
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/team"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
)

// buildTestFactory is a thin helper to keep test bodies concise.
func buildTestFactory(cfgs []config.ProviderConfig) *team.ProviderFactoryForTest {
	return team.NewProviderFactoryForTest(cfgs)
}

func TestLocalProviderFactory_Bedrock(t *testing.T) {
	t.Parallel()
	cfgs := []config.ProviderConfig{
		{Name: "br", Type: "bedrock", ModelID: "anthropic.claude-3-5-sonnet-20240620-v1:0"},
	}
	_, err := buildTestFactory(cfgs).Get("br")
	if err == nil {
		t.Fatal("expected error for bedrock provider in local mode, got nil")
	}
	if !strings.Contains(err.Error(), "bedrock provider requires AWS SDK setup") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLocalProviderFactory_OpenAI(t *testing.T) {
	t.Parallel()
	cfgs := []config.ProviderConfig{
		{Name: "oai", Type: "openai", ModelID: "gpt-4o"},
	}
	_, err := buildTestFactory(cfgs).Get("oai")
	if err == nil {
		t.Fatal("expected error for openai provider, got nil")
	}
	if !strings.Contains(err.Error(), "openai provider not yet implemented") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLocalProviderFactory_Unknown(t *testing.T) {
	t.Parallel()
	cfgs := []config.ProviderConfig{
		{Name: "x", Type: "unknown-type", ModelID: "foo"},
	}
	_, err := buildTestFactory(cfgs).Get("x")
	if err == nil {
		t.Fatal("expected error for unknown provider type, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported provider type") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLocalProviderFactory_GetUnregisteredName(t *testing.T) {
	t.Parallel()
	cfgs := []config.ProviderConfig{}
	_, err := buildTestFactory(cfgs).Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown provider name, got nil")
	}
	if !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLocalProviderFactory_AnthropicNoKey(t *testing.T) {
	// Cannot call t.Parallel() with t.Setenv — env mutation is not safe under parallel execution.
	t.Setenv("ANTHROPIC_API_KEY", "")
	cfgs := []config.ProviderConfig{
		{Name: "ac", Type: "anthropic", ModelID: "claude-3-haiku-20240307"},
	}
	_, err := buildTestFactory(cfgs).Get("ac")
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is empty, got nil")
	}
}

func TestLocalProviderFactory_AnthropicWithFakeKey(t *testing.T) {
	// Set a fake key — the SDK accepts any non-empty string at construction time.
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-fake-key-for-unit-testing")
	cfgs := []config.ProviderConfig{
		{Name: "ac", Type: "anthropic", ModelID: "claude-haiku-4-5-20251001"},
	}
	p, err := buildTestFactory(cfgs).Get("ac")
	if err != nil {
		t.Fatalf("unexpected error with fake key: %v", err)
	}
	if p == nil {
		t.Error("expected non-nil provider with fake key")
	}
}
