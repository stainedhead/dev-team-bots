package domain

import "context"

// SecretRef identifies a secret to resolve.
//
// Name is the logical secret name (e.g. "buzz_private_key",
// "anthropic_api_key") — not tied to any one provider's naming convention.
// Each SecretProvider implementation derives its own backend-specific key
// (environment variable name, systemd credential filename, keystore
// service/user pair, credentials-file key) from Name and Bot.
//
// Bot is the optional per-bot namespace, keyed on the bot's name (not its
// type — see PRD OQ-9). An empty Bot means a global/shared secret not scoped
// to any particular bot. Per FR-044, the environment-variable provider
// ignores Bot entirely: env vars are inherently process-global, so an
// explicit env var always wins regardless of namespacing.
type SecretRef struct {
	Name string
	Bot  string
}

// SecretProvider resolves secrets from a single backend (an environment
// variable, a systemd credentials directory, an OS keystore, or a
// credentials file).
//
// Lookup returns (value, true, nil) on a hit, ("", false, nil) when the
// provider has no entry for ref — a miss is not an error and MUST NOT halt a
// SecretStore's provider chain (FR-039) — and ("", false, err) only when the
// provider itself failed (e.g. a hung D-Bus call, a malformed credentials
// file). Implementations MUST NOT include the secret value in any returned
// error, log line, or other diagnostic output on any code path, including
// error paths (FR-051).
type SecretProvider interface {
	// Name identifies the provider for diagnostics and error messages
	// (e.g. "env", "systemd", "keystore", "file").
	Name() string
	Lookup(ctx context.Context, ref SecretRef) (string, bool, error)
}

// SecretStore resolves a SecretRef by trying an ordered chain of
// SecretProvider implementations and returning the first hit. Neither
// SecretStore nor SecretProvider may be implemented by, or depend on, any
// keystore, D-Bus, or other OS-specific package — those live behind
// implementations in internal/infrastructure/secret/ (FR-038).
type SecretStore interface {
	Get(ctx context.Context, ref SecretRef) (string, error)
}
