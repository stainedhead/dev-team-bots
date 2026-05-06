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

// --- validateEmbedderProvider tests ------------------------------------------

// TestValidateEmbedderProvider_OpenAI verifies that openai type is accepted.
func TestValidateEmbedderProvider_OpenAI(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		Memory: config.MemoryConfig{Embedder: "my-embedder"},
		Models: config.ModelsConfig{
			Providers: []config.ProviderConfig{
				{Name: "my-embedder", Type: "openai", ModelID: "text-embedding-3-small"},
			},
		},
	}
	if err := team.ValidateEmbedderProvider(cfg); err != nil {
		t.Errorf("expected nil error for openai embedder, got %v", err)
	}
}

// TestValidateEmbedderProvider_AnthropicFails verifies that anthropic type is rejected.
func TestValidateEmbedderProvider_AnthropicFails(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		Memory: config.MemoryConfig{Embedder: "my-embedder"},
		Models: config.ModelsConfig{
			Providers: []config.ProviderConfig{
				{Name: "my-embedder", Type: "anthropic", ModelID: "claude-haiku-4-5-20251001"},
			},
		},
	}
	err := team.ValidateEmbedderProvider(cfg)
	if err == nil {
		t.Fatal("expected error for anthropic embedder, got nil")
	}
	if !strings.Contains(err.Error(), "does not support embeddings") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestValidateEmbedderProvider_BedrockFails verifies that bedrock type is rejected.
func TestValidateEmbedderProvider_BedrockFails(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		Memory: config.MemoryConfig{Embedder: "my-embedder"},
		Models: config.ModelsConfig{
			Providers: []config.ProviderConfig{
				{Name: "my-embedder", Type: "bedrock", ModelID: "amazon.titan-embed-text-v1"},
			},
		},
	}
	err := team.ValidateEmbedderProvider(cfg)
	if err == nil {
		t.Fatal("expected error for bedrock embedder, got nil")
	}
	if !strings.Contains(err.Error(), "does not support embeddings") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestValidateEmbedderProvider_NotFound verifies that a missing provider name returns an error.
func TestValidateEmbedderProvider_NotFound(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		Memory: config.MemoryConfig{Embedder: "nonexistent-embedder"},
		Models: config.ModelsConfig{
			Providers: []config.ProviderConfig{
				{Name: "other", Type: "openai", ModelID: "text-embedding-3-small"},
			},
		},
	}
	err := team.ValidateEmbedderProvider(cfg)
	if err == nil {
		t.Fatal("expected error for missing embedder provider, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}
