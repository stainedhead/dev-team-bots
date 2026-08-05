//go:build integration

// This file holds Phase I's (tasks.md I2) `//go:build integration` stub for
// the Secret-storage PRD AC's compound second half (line 585): "Slack
// bot_token and app_token resolve from the keystore with neither present
// in config.yaml, AND the bot connects to Slack." The resolution half is
// already proven by real, non-integration-tagged unit tests
// (internal/infrastructure/config/slack_secrets_test.go's
// TestSlackConfig_ResolveSecrets_ResolvesFromStoreWhenInlineEmpty); the
// "connects to Slack" half needs a real Slack workspace/app and is
// therefore live-infrastructure-gated here.
//
// Environment contract:
//
//	BUZZ_TEST_SLACK_KEYSTORE_BOT   bot name the tokens are namespaced under
//	                                 in the OS keystore (SecretRef.Bot),
//	                                 written ahead of time via
//	                                 `boabotctl secret set slack_bot_token
//	                                 --bot <name>` /
//	                                 `... slack_app_token --bot <name>`.
package slack

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	slackgo "github.com/slack-go/slack"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret/keystore"
)

// TestLiveSlack_TokensResolveFromKeystoreAndConnect resolves bot_token/
// app_token purely from the OS keystore (no config.yaml, no
// ~/.boabot/credentials) and confirms the resulting tokens authenticate
// against the real Slack API (auth.test) -- the literal "connects to
// Slack" half of the AC, without needing to drive a full Socket Mode
// session in a test.
func TestLiveSlack_TokensResolveFromKeystoreAndConnect(t *testing.T) {
	botName := os.Getenv("BUZZ_TEST_SLACK_KEYSTORE_BOT")
	if botName == "" {
		t.Skip("BUZZ_TEST_SLACK_KEYSTORE_BOT not set; this test requires slack_bot_token/slack_app_token pre-provisioned in the OS keystore under this bot name (see boabotctl secret set)")
	}

	// The keystore alone -- deliberately not env/systemd/file -- so a hit
	// here can only have come from the OS keystore, matching the AC's
	// "with neither present in config.yaml" framing (config.yaml is never
	// consulted by ResolveSecrets in the first place; this additionally
	// proves the value isn't coming from an env var or credentials file
	// left over in the test environment).
	store := secret.New([]domain.SecretProvider{keystore.New()})

	cfg := config.SlackConfig{BotName: botName} // deliberately empty inline tokens
	cfg.ResolveSecrets(context.Background(), store, slog.Default())

	if cfg.BotToken == "" || cfg.AppToken == "" {
		t.Fatalf("expected both bot_token and app_token to resolve from the keystore for bot %q, got bot_token empty=%v app_token empty=%v",
			botName, cfg.BotToken == "", cfg.AppToken == "")
	}

	api := slackgo.New(cfg.BotToken)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := api.AuthTestContext(ctx)
	if err != nil {
		t.Fatalf("Slack auth.test with the keystore-resolved bot_token failed -- token did not connect: %v", err)
	}
	t.Logf("connected to Slack as %s (team %s) using keystore-resolved tokens only", resp.User, resp.Team)
}
