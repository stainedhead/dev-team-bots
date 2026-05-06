package team

// This file exposes internal types and functions for use in tests.
// It is only compiled when running tests (package team, not team_test).

import (
	"context"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/watchdog"
)

// ProviderFactoryForTest wraps the unexported localProviderFactory so that
// tests in the team_test package can exercise it.
type ProviderFactoryForTest struct {
	f *localProviderFactory
}

func NewProviderFactoryForTest(cfgs []config.ProviderConfig) *ProviderFactoryForTest {
	return &ProviderFactoryForTest{f: newLocalProviderFactory(cfgs)}
}

func (p *ProviderFactoryForTest) Get(name string) (domain.ModelProvider, error) {
	return p.f.Get(name)
}

// SetBotRunner replaces the bot runner function on tm for testing, avoiding
// real file I/O and network calls in TeamManager lifecycle tests.
func SetBotRunner(tm *TeamManager, fn func(ctx context.Context, entry BotEntry, orchestratorName string) error) {
	tm.botRunner = fn
}

// BotEntryForTest re-exports BotEntry so package-level tests can construct values.
type BotEntryForTest = BotEntry

// ValidateEmbedderProvider exposes the unexported validateEmbedderProvider for
// testing.
func ValidateEmbedderProvider(botCfg config.Config) error {
	return validateEmbedderProvider(botCfg)
}

// WatchdogConfigForTest constructs a watchdog.Config for testing the watchdog
// wiring in TeamManager.
func WatchdogConfigForTest(sampleInterval time.Duration, warnMB, hardMB int) watchdog.Config {
	return watchdog.Config{
		SampleInterval: sampleInterval,
		WarnMB:         warnMB,
		HardMB:         hardMB,
	}
}
