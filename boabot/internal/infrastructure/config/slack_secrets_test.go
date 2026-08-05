package config_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
)

// fakeSecretStore is a minimal domain.SecretStore test double: it resolves a
// fixed set of refs and reports a NotFoundError-shaped miss (via a plain
// error, since callers only branch on err == nil) otherwise.
type fakeSecretStore struct {
	values map[domain.SecretRef]string
	calls  []domain.SecretRef
}

func (f *fakeSecretStore) Get(_ context.Context, ref domain.SecretRef) (string, error) {
	f.calls = append(f.calls, ref)
	if v, ok := f.values[ref]; ok {
		return v, nil
	}
	return "", errNotFound
}

var errNotFound = errString("not found")

type errString string

func (e errString) Error() string { return string(e) }

func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

// TestSlackConfig_ResolveSecrets_InlineFieldsWin verifies FR-047's "existing
// inline fields keep working" requirement: when bot_token/app_token are
// already set in config.yaml, they are used as-is and a deprecation warning
// naming the alternative (credentials file / keystore) is logged — this also
// covers C3's warn-only clause of FR-048.
func TestSlackConfig_ResolveSecrets_InlineFieldsWin(t *testing.T) {
	store := &fakeSecretStore{values: map[domain.SecretRef]string{
		{Name: "slack_bot_token", Bot: "mybot"}: "should-not-be-used",
		{Name: "slack_app_token", Bot: "mybot"}: "should-not-be-used",
	}}
	logger, buf := newTestLogger()

	s := &config.SlackConfig{BotToken: "xoxb-inline", AppToken: "xapp-inline", BotName: "mybot"}
	s.ResolveSecrets(context.Background(), store, logger)

	if s.BotToken != "xoxb-inline" {
		t.Errorf("BotToken: got %q, want xoxb-inline (inline value must win)", s.BotToken)
	}
	if s.AppToken != "xapp-inline" {
		t.Errorf("AppToken: got %q, want xapp-inline (inline value must win)", s.AppToken)
	}
	logOutput := buf.String()
	if !strings.Contains(logOutput, "deprecated") {
		t.Errorf("expected a deprecation warning to be logged, got: %q", logOutput)
	}
	if !strings.Contains(logOutput, "credentials") && !strings.Contains(logOutput, "keystore") {
		t.Errorf("expected deprecation warning to name the alternative (credentials file / keystore), got: %q", logOutput)
	}
	if len(store.calls) != 0 {
		t.Errorf("expected no SecretStore.Get calls when inline fields are set, got %d", len(store.calls))
	}
}

// TestSlackConfig_ResolveSecrets_ResolvesFromStoreWhenInlineEmpty verifies
// the PRD AC: "Slack bot_token/app_token resolve from the keystore with
// neither present in config.yaml."
func TestSlackConfig_ResolveSecrets_ResolvesFromStoreWhenInlineEmpty(t *testing.T) {
	store := &fakeSecretStore{values: map[domain.SecretRef]string{
		{Name: "slack_bot_token", Bot: "mybot"}: "xoxb-from-store",
		{Name: "slack_app_token", Bot: "mybot"}: "xapp-from-store",
	}}
	logger, buf := newTestLogger()

	s := &config.SlackConfig{BotName: "mybot"}
	s.ResolveSecrets(context.Background(), store, logger)

	if s.BotToken != "xoxb-from-store" {
		t.Errorf("BotToken: got %q, want xoxb-from-store", s.BotToken)
	}
	if s.AppToken != "xapp-from-store" {
		t.Errorf("AppToken: got %q, want xapp-from-store", s.AppToken)
	}
	if strings.Contains(buf.String(), "deprecated") {
		t.Errorf("no deprecation warning expected when inline fields are empty, got: %q", buf.String())
	}
}

// TestSlackConfig_ResolveSecrets_MissLeavesFieldsEmpty verifies that a
// SecretStore miss is not fatal: the fields are simply left empty, matching
// today's behaviour of "monitor only activates when all three are present."
func TestSlackConfig_ResolveSecrets_MissLeavesFieldsEmpty(t *testing.T) {
	store := &fakeSecretStore{values: map[domain.SecretRef]string{}}
	logger, _ := newTestLogger()

	s := &config.SlackConfig{BotName: "mybot"}
	s.ResolveSecrets(context.Background(), store, logger)

	if s.BotToken != "" {
		t.Errorf("BotToken: got %q, want empty on miss", s.BotToken)
	}
	if s.AppToken != "" {
		t.Errorf("AppToken: got %q, want empty on miss", s.AppToken)
	}
}

// TestSlackConfig_ResolveSecrets_NoBotNameSkipsStore verifies that when
// BotName is empty (Slack cannot possibly activate — see main.go's
// activation gate requiring BotToken, AppToken, and BotName all non-empty),
// ResolveSecrets does not consult the store at all, avoiding an unnecessary
// keystore/systemd round trip on every startup of a non-Slack bot.
func TestSlackConfig_ResolveSecrets_NoBotNameSkipsStore(t *testing.T) {
	store := &fakeSecretStore{values: map[domain.SecretRef]string{}}
	logger, _ := newTestLogger()

	s := &config.SlackConfig{}
	s.ResolveSecrets(context.Background(), store, logger)

	if len(store.calls) != 0 {
		t.Errorf("expected no SecretStore.Get calls when BotName is empty, got %d", len(store.calls))
	}
}
