package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
)

// fakeDiagProvider is a domain.SecretProvider test double for diag_test.go.
type fakeDiagProvider struct {
	name  string
	value string
	ok    bool
	err   error
}

func (f *fakeDiagProvider) Name() string { return f.name }

func (f *fakeDiagProvider) Lookup(_ context.Context, _ domain.SecretRef) (string, bool, error) {
	return f.value, f.ok, f.err
}

// TestDiagnoseSecrets_ReportsResolvingProviderName verifies FR-050: the
// diagnostic reports, per secret, which provider resolved it, by name only.
func TestDiagnoseSecrets_ReportsResolvingProviderName(t *testing.T) {
	providers := []domain.SecretProvider{
		&fakeDiagProvider{name: "env", ok: false},
		&fakeDiagProvider{name: "systemd", ok: false},
		&fakeDiagProvider{name: "keystore", value: "sk-live", ok: true},
		&fakeDiagProvider{name: "file", ok: false},
	}
	refs := []domain.SecretRef{{Name: "anthropic_api_key"}}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	entries := diagnoseSecrets(context.Background(), providers, refs, logger)

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if !e.Resolved {
		t.Fatal("expected entry to be resolved")
	}
	if e.Provider != "keystore" {
		t.Errorf("Provider: got %q, want keystore", e.Provider)
	}
}

// TestDiagnoseSecrets_UnresolvedListsConsultedProviders verifies that an
// unresolved secret still names every provider consulted, matching the
// SecretStore's own FR-053 spirit at the diagnostic layer.
func TestDiagnoseSecrets_UnresolvedListsConsultedProviders(t *testing.T) {
	providers := []domain.SecretProvider{
		&fakeDiagProvider{name: "env", ok: false},
		&fakeDiagProvider{name: "systemd", ok: false, err: errors.New("boom")},
		&fakeDiagProvider{name: "keystore", ok: false},
		&fakeDiagProvider{name: "file", ok: false},
	}
	refs := []domain.SecretRef{{Name: "buzz_private_key", Bot: "buzzbot"}}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	entries := diagnoseSecrets(context.Background(), providers, refs, logger)

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Resolved {
		t.Fatal("expected entry to be unresolved")
	}
	want := []string{"env", "systemd", "keystore", "file"}
	if len(e.Consulted) != len(want) {
		t.Fatalf("Consulted: got %v, want %v", e.Consulted, want)
	}
	for i, name := range want {
		if e.Consulted[i] != name {
			t.Errorf("Consulted[%d]: got %q, want %q", i, e.Consulted[i], name)
		}
	}
}

// TestSecretRefsForDiagnostics_IncludesSlackWhenConfigured verifies the set
// of refs a diagnostic run checks: the two migrated C1 secrets always, plus
// the two Slack secrets only when a Slack bot name is configured.
func TestSecretRefsForDiagnostics_IncludesSlackWhenConfigured(t *testing.T) {
	cfg := config.Config{}
	refs := secretRefsForDiagnostics(cfg)
	if len(refs) != 2 {
		t.Fatalf("got %d refs with no Slack config, want 2", len(refs))
	}

	cfg.Slack.BotName = "mybot"
	refs = secretRefsForDiagnostics(cfg)
	if len(refs) != 4 {
		t.Fatalf("got %d refs with Slack configured, want 4", len(refs))
	}
	found := map[string]bool{}
	for _, r := range refs {
		found[r.Name] = true
		if (r.Name == "slack_bot_token" || r.Name == "slack_app_token") && r.Bot != "mybot" {
			t.Errorf("ref %q: Bot = %q, want mybot", r.Name, r.Bot)
		}
	}
	for _, want := range []string{"anthropic_api_key", "boabot_backup_token", "slack_bot_token", "slack_app_token"} {
		if !found[want] {
			t.Errorf("expected ref %q to be present", want)
		}
	}
}

// TestPrintSecretDiagnostics_NeverPrintsSecretValue is the sentinel test for
// the diagnostic half of FR-051: a resolved secret's value must never
// appear in the diagnostic's printed output, only the provider name.
func TestPrintSecretDiagnostics_NeverPrintsSecretValue(t *testing.T) {
	const sentinel = "TOTALLY-SECRET-SENTINEL-VALUE-9f8a"
	providers := []domain.SecretProvider{
		&fakeDiagProvider{name: "env", value: sentinel, ok: true},
	}
	refs := []domain.SecretRef{{Name: "anthropic_api_key"}}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	entries := diagnoseSecrets(context.Background(), providers, refs, logger)

	var out bytes.Buffer
	printSecretDiagnostics(&out, entries)

	if strings.Contains(out.String(), sentinel) {
		t.Errorf("diagnostic stdout output contained the secret value: %q", out.String())
	}
	if strings.Contains(logBuf.String(), sentinel) {
		t.Errorf("diagnostic log output contained the secret value: %q", logBuf.String())
	}
}
