package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret"
)

// fakeSecretStore is a minimal domain.SecretStore test double.
type fakeSecretStore struct {
	values map[domain.SecretRef]string
}

func (f *fakeSecretStore) Get(_ context.Context, ref domain.SecretRef) (string, error) {
	if v, ok := f.values[ref]; ok {
		return v, nil
	}
	return "", errors.New("not found")
}

// TestApplyCredentialFromStore_SetsEnvOnHit verifies that a resolved secret
// is applied to the named environment variable.
func TestApplyCredentialFromStore_SetsEnvOnHit(t *testing.T) {
	store := &fakeSecretStore{values: map[domain.SecretRef]string{
		{Name: "anthropic_api_key"}: "sk-from-store",
	}}
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	t.Cleanup(func() { _ = os.Unsetenv("ANTHROPIC_API_KEY") })

	applyCredentialFromStore(context.Background(), store, "anthropic_api_key", "ANTHROPIC_API_KEY")

	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "sk-from-store" {
		t.Errorf("ANTHROPIC_API_KEY: got %q, want sk-from-store", got)
	}
}

// TestApplyCredentialFromStore_LeavesEnvUnchangedOnMiss verifies that a
// SecretStore miss is a no-op (FR-046: no observable behaviour change).
func TestApplyCredentialFromStore_LeavesEnvUnchangedOnMiss(t *testing.T) {
	store := &fakeSecretStore{values: map[domain.SecretRef]string{}}
	_ = os.Unsetenv("BOABOT_BACKUP_TOKEN")

	applyCredentialFromStore(context.Background(), store, "boabot_backup_token", "BOABOT_BACKUP_TOKEN")

	if got := os.Getenv("BOABOT_BACKUP_TOKEN"); got != "" {
		t.Errorf("BOABOT_BACKUP_TOKEN: got %q, want empty", got)
	}
}

// TestResolveEnvCredentials_CallsBothSecrets verifies that both migrated
// applyCredential call sites (ANTHROPIC_API_KEY, BOABOT_BACKUP_TOKEN) now go
// through SecretStore.Get (FR-046).
func TestResolveEnvCredentials_CallsBothSecrets(t *testing.T) {
	store := &fakeSecretStore{values: map[domain.SecretRef]string{
		{Name: "anthropic_api_key"}:   "sk-x",
		{Name: "boabot_backup_token"}: "ghp-x",
	}}
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("BOABOT_BACKUP_TOKEN")
	t.Cleanup(func() {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("BOABOT_BACKUP_TOKEN")
	})

	resolveEnvCredentials(context.Background(), store)

	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "sk-x" {
		t.Errorf("ANTHROPIC_API_KEY: got %q, want sk-x", got)
	}
	if got := os.Getenv("BOABOT_BACKUP_TOKEN"); got != "ghp-x" {
		t.Errorf("BOABOT_BACKUP_TOKEN: got %q, want ghp-x", got)
	}
}

// TestBuildSecretProviders_DefaultOrder verifies the default chain order
// (FR-040): env, systemd, keystore, file — with a resolvable credentials
// path and no world-readable file present.
func TestBuildSecretProviders_DefaultOrder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	providers, err := buildSecretProviders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"env", "systemd", "keystore", "file"}
	if len(providers) != len(want) {
		t.Fatalf("got %d providers, want %d", len(providers), len(want))
	}
	for i, p := range providers {
		if p.Name() != want[i] {
			t.Errorf("provider[%d]: got %q, want %q", i, p.Name(), want[i])
		}
	}
}

// TestBuildSecretProviders_WorldReadableCredentialsFileIsFatal verifies
// FR-046's "no observable behaviour change" AC: a world-readable
// ~/.boabot/credentials remains fatal, with the pre-existing error message
// preserved.
func TestBuildSecretProviders_WorldReadableCredentialsFileIsFatal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	credDir := filepath.Join(dir, ".boabot")
	if err := os.MkdirAll(credDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	credPath := filepath.Join(credDir, "credentials")
	if err := os.WriteFile(credPath, []byte("[default]\nanthropic_api_key=x\n"), 0644); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	_, err := buildSecretProviders()
	if err == nil {
		t.Fatal("expected an error for a world-readable credentials file, got nil")
	}
	got := err.Error()
	if !strings.Contains(got, "world-readable") || !strings.Contains(got, "chmod 600") {
		t.Errorf("error message: got %q, want it to preserve the existing world-readable/chmod hint", got)
	}
}

// TestEndToEnd_EnvOnlyDeployment exercises the real provider chain (not a
// fake) for the FR-046 PRD acceptance criterion: "an existing deployment
// using only env vars ... starts with a byte-identical config to before
// this change." A fake domain.SecretStore would bypass the two seams most
// likely to silently break this (env.Provider's ref-name-to-env-var-name
// derivation, and the historical env-var names themselves), so this test
// goes through buildSecretProviders + secret.New + resolveEnvCredentials
// exactly as run() does.
func TestEndToEnd_EnvOnlyDeployment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // no ~/.boabot/credentials present under here
	t.Setenv("ANTHROPIC_API_KEY", "sk-env")
	_ = os.Unsetenv("BOABOT_BACKUP_TOKEN")
	t.Cleanup(func() { _ = os.Unsetenv("BOABOT_BACKUP_TOKEN") })

	providers, err := buildSecretProviders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolveEnvCredentials(context.Background(), secret.New(providers))

	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "sk-env" {
		t.Errorf("ANTHROPIC_API_KEY: got %q, want sk-env (env var must be left exactly as set)", got)
	}
	if got := os.Getenv("BOABOT_BACKUP_TOKEN"); got != "" {
		t.Errorf("BOABOT_BACKUP_TOKEN: got %q, want empty (no source configures it)", got)
	}
}

// TestEndToEnd_CredentialsFileOnlyDeployment exercises the real provider
// chain for the FR-046 PRD acceptance criterion's second half: "one using
// only ~/.boabot/credentials ... starts with a byte-identical config to
// before this change." This asserts the file provider's key convention
// (bare ref.Name when Bot is empty) still matches the credentials-file keys
// the pre-migration applyCredential(creds, "anthropic_api_key", ...) call
// used to read.
func TestEndToEnd_CredentialsFileOnlyDeployment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("BOABOT_BACKUP_TOKEN")
	t.Cleanup(func() {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
		_ = os.Unsetenv("BOABOT_BACKUP_TOKEN")
	})

	credDir := filepath.Join(dir, ".boabot")
	if err := os.MkdirAll(credDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	credPath := filepath.Join(credDir, "credentials")
	content := "[default]\nanthropic_api_key=sk-file\nboabot_backup_token=ghp-file\n"
	if err := os.WriteFile(credPath, []byte(content), 0600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	providers, err := buildSecretProviders()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolveEnvCredentials(context.Background(), secret.New(providers))

	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "sk-file" {
		t.Errorf("ANTHROPIC_API_KEY: got %q, want sk-file", got)
	}
	if got := os.Getenv("BOABOT_BACKUP_TOKEN"); got != "ghp-file" {
		t.Errorf("BOABOT_BACKUP_TOKEN: got %q, want ghp-file", got)
	}
}
