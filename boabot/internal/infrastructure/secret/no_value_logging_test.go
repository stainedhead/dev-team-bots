package secret_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	secretpkg "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret/env"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret/file"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret/keystore"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret/systemd"
)

// sentinel is a value that must never appear in any log line or returned
// error string produced by any of the four secret providers, on any code
// path (FR-051, provider half — the diagnostic-command half is C4).
const sentinel = "sk-super-secret-sentinel-do-not-log-me"

// mockInitWithError installs a failing keyring backend for the duration of
// t and restores a clean MockInit() backend afterward. keyring.provider is
// process-global state, so leaving a failing backend installed after a test
// would leak into whatever test runs next in this binary.
func mockInitWithError(t *testing.T, err error) {
	t.Helper()
	keyring.MockInitWithError(err)
	t.Cleanup(keyring.MockInit)
}

// TestStore_NoSecretValueEverLogged_ChainErrorPath drives all four real
// providers through a Store where every one of them fails or misses while
// handling data containing the sentinel value, and asserts the sentinel
// never appears in the captured log output or in the returned error.
func TestStore_NoSecretValueEverLogged_ChainErrorPath(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	ref := domain.SecretRef{Name: "buzz_private_key", Bot: "buzzy"}

	// env: unset, so it misses cleanly (nothing to leak).

	// systemd: point CREDENTIALS_DIRECTORY at a directory where the
	// expected credential name is itself a directory (not a file), forcing
	// a read error rather than a clean miss.
	credDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(credDir, "buzzy_buzz_private_key"), 0o700); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", credDir)

	// keystore: backend errors on every call.
	mockInitWithError(t, errors.New("keystore backend unavailable"))

	// file: world-readable credentials file containing the sentinel as a
	// value, forcing the fatal world-readable error path.
	credsDir := t.TempDir()
	credsPath := filepath.Join(credsDir, "credentials")
	contents := "[default]\nbuzzy_buzz_private_key = " + sentinel + "\n"
	if err := os.WriteFile(credsPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.Chmod(credsPath, 0o644); err != nil {
		t.Fatalf("chmod fixture: %v", err)
	}

	providers := []domain.SecretProvider{env.New(), systemd.New(), keystore.New(), file.New(credsPath)}
	store := secretpkg.New(providers, secretpkg.WithLogger(logger))

	_, err := store.Get(context.Background(), ref)
	if err == nil {
		t.Fatal("expected an error (every provider errors or misses)")
	}

	if strings.Contains(buf.String(), sentinel) {
		t.Fatalf("secret sentinel leaked into log output:\n%s", buf.String())
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("secret sentinel leaked into returned error: %v", err)
	}
}

// TestProviders_HitPath_NoSecretValueLoggedByStore drives a successful
// resolution (env hit) through Store with the sentinel as the real secret
// value, and asserts the value that flows through the chain is never
// written to the log, even though resolution succeeds.
func TestProviders_HitPath_NoSecretValueLoggedByStore(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	t.Setenv("BUZZ_PRIVATE_KEY", sentinel)

	providers := []domain.SecretProvider{env.New()}
	store := secretpkg.New(providers, secretpkg.WithLogger(logger))

	got, err := store.Get(context.Background(), domain.SecretRef{Name: "buzz_private_key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != sentinel {
		t.Fatalf("resolved value = %q, want the sentinel itself (this assertion is not the leak check)", got)
	}
	if strings.Contains(buf.String(), sentinel) {
		t.Fatalf("secret sentinel leaked into log output on the hit path:\n%s", buf.String())
	}
}

// TestProviders_ReturnedErrors_NeverContainSecretValue is a consolidated,
// cross-provider regression test (complementing each provider package's own
// version) asserting every provider's error-path Error() string is free of
// a secret value that was in play during the failing call.
func TestProviders_ReturnedErrors_NeverContainSecretValue(t *testing.T) {
	ref := domain.SecretRef{Name: "buzz_private_key", Bot: "buzzy"}

	t.Run("keystore Lookup", func(t *testing.T) {
		mockInitWithError(t, errors.New("boom"))
		_, _, err := keystore.New().Lookup(context.Background(), ref)
		requireNoSentinel(t, err)
	})

	t.Run("keystore Set", func(t *testing.T) {
		mockInitWithError(t, errors.New("boom"))
		err := keystore.New().Set(context.Background(), ref, sentinel)
		requireNoSentinel(t, err)
	})

	t.Run("file Lookup world-readable", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "credentials")
		contents := "[default]\nbuzzy_buzz_private_key = " + sentinel + "\n"
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("chmod fixture: %v", err)
		}
		_, _, err := file.New(path).Lookup(context.Background(), ref)
		requireNoSentinel(t, err)
	})

	t.Run("systemd Lookup read error", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "buzzy_buzz_private_key"), 0o700); err != nil {
			t.Fatalf("mkdir fixture: %v", err)
		}
		t.Setenv("CREDENTIALS_DIRECTORY", dir)
		_, _, err := systemd.New().Lookup(context.Background(), ref)
		requireNoSentinel(t, err)
	})
}

func requireNoSentinel(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a non-nil error")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("error leaked secret value: %v", err)
	}
}
