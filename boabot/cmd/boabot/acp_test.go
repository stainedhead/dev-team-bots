package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
)

// fakeProviderFactory is a minimal domain.ProviderFactory for tests that
// need to exercise real chat-provider-selection wiring (buildACPWorker)
// without constructing real anthropic/openai/bedrock clients.
type fakeProviderFactory map[string]domain.ModelProvider

func (f fakeProviderFactory) Get(name string) (domain.ModelProvider, error) {
	if p, ok := f[name]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("fakeProviderFactory: unknown provider %q", name)
}

func TestResolveACPConfigPath_ExplicitConfigWins(t *testing.T) {
	got := resolveACPConfigPath(true, "/explicit/path/config.yaml", "/bots", "tech-lead")
	want := "/explicit/path/config.yaml"
	if got != want {
		t.Errorf("resolveACPConfigPath() = %q, want %q", got, want)
	}
}

func TestResolveACPConfigPath_AgentNameResolvesUnderBotsDir(t *testing.T) {
	got := resolveACPConfigPath(false, "/ignored/default/config.yaml", "/bots", "tech-lead")
	want := filepath.Join("/bots", "tech-lead", "config.yaml")
	if got != want {
		t.Errorf("resolveACPConfigPath() = %q, want %q", got, want)
	}
}

func TestResolveACPConfigPath_DefaultsToOrchestrator(t *testing.T) {
	// Mirrors main()'s flag.String("agent", "orchestrator", ...) default --
	// this test exercises resolveACPConfigPath directly with that same
	// default value, since the flag default itself is exercised by the
	// flag package, not this function.
	got := resolveACPConfigPath(false, "/ignored/default/config.yaml", "/bots", "orchestrator")
	want := filepath.Join("/bots", "orchestrator", "config.yaml")
	if got != want {
		t.Errorf("resolveACPConfigPath() = %q, want %q", got, want)
	}
}

func writeACPTestPersona(t *testing.T, configYAML string) (configPath string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("You are a test persona."), 0o600); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}
	configPath = filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return configPath
}

func TestBuildACPAgent_ValidPersonaSucceeds(t *testing.T) {
	configPath := writeACPTestPersona(t, `
bot:
  name: test-bot
  type: test-bot
models:
  default: test-provider
  providers:
    - name: test-provider
      type: anthropic
      model_id: claude-x
`)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	agent, err := buildACPAgent(configPath)
	if err != nil {
		t.Fatalf("buildACPAgent returned error: %v", err)
	}
	if agent == nil {
		t.Fatal("buildACPAgent returned a nil Agent with no error")
	}
}

func TestBuildACPAgent_WithWorkDirsSucceeds(t *testing.T) {
	// RT3/FR-004 (auto-review): wiring WithRulesTracker when
	// orchestrator.work_dirs is set must not break construction.
	configPath := writeACPTestPersona(t, `
bot:
  name: test-bot
  type: test-bot
orchestrator:
  work_dirs:
    - /tmp
models:
  default: test-provider
  providers:
    - name: test-provider
      type: anthropic
      model_id: claude-x
`)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	agent, err := buildACPAgent(configPath)
	if err != nil {
		t.Fatalf("buildACPAgent returned error: %v", err)
	}
	if agent == nil {
		t.Fatal("buildACPAgent returned a nil Agent with no error")
	}
}

func TestBuildACPAgent_MissingSOULFails(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
bot:
  name: test-bot
  type: test-bot
models:
  default: test-provider
  providers:
    - name: test-provider
      type: anthropic
      model_id: claude-x
`), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	if _, err := buildACPAgent(configPath); err == nil {
		t.Fatal("expected an error when SOUL.md is missing, got nil")
	}
}

func TestBuildACPAgent_InvalidKeepAliveIntervalFails(t *testing.T) {
	configPath := writeACPTestPersona(t, `
bot:
  name: test-bot
  type: test-bot
models:
  default: test-provider
  providers:
    - name: test-provider
      type: anthropic
      model_id: claude-x
`)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("BOABOT_ACP_KEEPALIVE_INTERVAL", "not-a-duration")

	if _, err := buildACPAgent(configPath); err == nil {
		t.Fatal("expected an error for an invalid BOABOT_ACP_KEEPALIVE_INTERVAL, got nil")
	}
}

// TestBuildACPWorker_ChatProviderUsedForACPSource verifies FR-401's
// acceptance criterion end-to-end through the real production wiring
// function (buildACPWorker, extracted from buildACPAgent so it's directly
// unit-testable per AGENTS.md's "cmd/ is wiring only" rule) -- not just
// that isConversationalSource's string match changed, but that an
// ACP-sourced task constructed via cfg.Models.chat_provider actually
// invokes the chat provider, mirroring team_manager.go:1047-1056's exact
// gating condition.
func TestBuildACPWorker_ChatProviderUsedForACPSource(t *testing.T) {
	chatCalled := false
	chatProvider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			chatCalled = true
			return domain.InvokeResponse{Content: "chat response", StopReason: "stop"}, nil
		},
	}
	defaultProvider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			return domain.InvokeResponse{Content: "default response", StopReason: "stop"}, nil
		},
	}
	pf := fakeProviderFactory{"default": defaultProvider, "chat": chatProvider}

	cfg := config.Config{
		Bot:    config.BotConfig{Name: "test-bot", BotType: "tech-lead"},
		Models: config.ModelsConfig{Default: "default", ChatProvider: "chat"},
	}

	worker := buildACPWorker(cfg, t.TempDir(), "soul prompt", pf, defaultProvider,
		&mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})

	_, err := worker.Execute(context.Background(), domain.Task{ID: "t-acp-1", Source: "acp", Instruction: "hello"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !chatCalled {
		t.Error("expected chat provider to be invoked for an acp-sourced task when models.chat_provider is configured")
	}
}

// TestBuildACPWorker_ChatProviderUnresolvable_FallsBackToDefault verifies
// NFR-Reliability: a bad/unresolvable models.chat_provider degrades
// gracefully (falls back to the default provider) rather than blocking
// worker construction, mirroring team_manager.go's own log-and-continue
// treatment of a failed pf.Get call.
func TestBuildACPWorker_ChatProviderUnresolvable_FallsBackToDefault(t *testing.T) {
	defaultCalled := false
	defaultProvider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			defaultCalled = true
			return domain.InvokeResponse{Content: "default response", StopReason: "stop"}, nil
		},
	}
	pf := fakeProviderFactory{"default": defaultProvider}

	cfg := config.Config{
		Bot:    config.BotConfig{Name: "test-bot", BotType: "tech-lead"},
		Models: config.ModelsConfig{Default: "default", ChatProvider: "does-not-exist"},
	}

	worker := buildACPWorker(cfg, t.TempDir(), "soul prompt", pf, defaultProvider,
		&mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})

	_, err := worker.Execute(context.Background(), domain.Task{ID: "t-acp-2", Source: "acp", Instruction: "hello"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !defaultCalled {
		t.Error("expected default provider to be invoked when chat_provider is unresolvable")
	}
}

// TestBuildACPWorker_NoChatProvider_UsesDefault verifies that with no
// models.chat_provider configured at all, the default provider handles
// acp-sourced tasks (the pre-existing, unconfigured-case behavior).
func TestBuildACPWorker_NoChatProvider_UsesDefault(t *testing.T) {
	defaultCalled := false
	defaultProvider := &mocks.ModelProvider{
		InvokeFn: func(_ context.Context, _ domain.InvokeRequest) (domain.InvokeResponse, error) {
			defaultCalled = true
			return domain.InvokeResponse{Content: "default response", StopReason: "stop"}, nil
		},
	}
	pf := fakeProviderFactory{"default": defaultProvider}

	cfg := config.Config{
		Bot:    config.BotConfig{Name: "test-bot", BotType: "tech-lead"},
		Models: config.ModelsConfig{Default: "default"},
	}

	worker := buildACPWorker(cfg, t.TempDir(), "soul prompt", pf, defaultProvider,
		&mocks.MemoryStore{}, &mocks.Embedder{}, &mocks.VectorStore{})

	_, err := worker.Execute(context.Background(), domain.Task{ID: "t-acp-3", Source: "acp", Instruction: "hello"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !defaultCalled {
		t.Error("expected default provider to be invoked when chat_provider is not configured")
	}
}

func TestBuildACPAgent_UnknownProviderFails(t *testing.T) {
	configPath := writeACPTestPersona(t, `
bot:
  name: test-bot
  type: test-bot
models:
  default: does-not-exist
  providers: []
`)

	if _, err := buildACPAgent(configPath); err == nil {
		t.Fatal("expected an error for an unresolvable default provider, got nil")
	}
}

// TestBuildACPAgent_SharedStateOwnerMismatch_DegradesGracefully verifies
// FR-501's NFR-Reliability requirement end-to-end: a shared-state directory
// already claimed by a different identity (e.g. because this persona was
// renamed, or its memory.path was misconfigured to collide with another
// persona's directory) must log a warning, not block agent construction --
// mirroring the existing degrade-gracefully pattern for board/plugin store
// construction failures (buildACPMCPOptions).
func TestBuildACPAgent_SharedStateOwnerMismatch_DegradesGracefully(t *testing.T) {
	memRoot := t.TempDir()
	memPath := filepath.Join(memRoot, "test-bot")
	if err := os.MkdirAll(memPath, 0o755); err != nil {
		t.Fatalf("mkdir memPath: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memPath, ".shared-state-owner"), []byte(`{"owner":"a-different-persona"}`), 0o644); err != nil {
		t.Fatalf("seed shared-state marker: %v", err)
	}

	configPath := writeACPTestPersona(t, fmt.Sprintf(`
bot:
  name: test-bot
  type: test-bot
memory:
  path: %s
models:
  default: test-provider
  providers:
    - name: test-provider
      type: anthropic
      model_id: claude-x
`, memRoot))
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	agent, err := buildACPAgent(configPath)
	if err != nil {
		t.Fatalf("buildACPAgent should degrade gracefully on a shared-state owner mismatch, got error: %v", err)
	}
	if agent == nil {
		t.Fatal("buildACPAgent returned a nil Agent with no error")
	}
}
