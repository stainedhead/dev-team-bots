// Package systemd implements a domain.SecretProvider backed by systemd's
// "System and Service Credentials" mechanism
// (https://systemd.io/CREDENTIALS/): LoadCredentialEncrypted=/
// SetCredentialEncrypted= materialise one file per credential under a
// directory named by the $CREDENTIALS_DIRECTORY environment variable, on
// tmpfs, readable only by the service's user, and removed when the unit
// stops.
//
// Per FR-042, this provider is inert (a miss, never an error) when
// $CREDENTIALS_DIRECTORY is unset, so it costs nothing on non-Linux
// platforms and on Linux outside systemd (interactive sessions, containers
// without the directive, etc.) — no D-Bus, no syscalls beyond an env var
// read and, only if the directory is present, a file read.
//
// Per FR-045/B8, lookups are namespaced by SecretRef.Bot using the filename
// convention "<bot>_<name>" (matching the file provider's key convention),
// falling back to the bare "<name>" file for a global/shared secret (empty
// Bot).
//
// systemd's own documentation does not guarantee a specific trailing-byte
// convention for credential file content. This provider defensively strips
// a single trailing newline, since credentials are commonly authored by
// hand or by `echo` into SetCredentialEncrypted= inputs.
package systemd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

const (
	providerName         = "systemd"
	credentialsDirEnvVar = "CREDENTIALS_DIRECTORY"
)

// Provider resolves secrets from systemd's $CREDENTIALS_DIRECTORY.
type Provider struct{}

// New returns a new systemd Provider.
func New() *Provider {
	return &Provider{}
}

// Name returns the provider name used in diagnostics and error messages.
func (p *Provider) Name() string {
	return providerName
}

// Lookup resolves ref against $CREDENTIALS_DIRECTORY/<credentialName>. See
// the package doc for the inert-when-unset rule and the naming convention.
func (p *Provider) Lookup(_ context.Context, ref domain.SecretRef) (string, bool, error) {
	dir := os.Getenv(credentialsDirEnvVar)
	if dir == "" {
		return "", false, nil
	}

	path := filepath.Join(dir, credentialName(ref))
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		// The wrapped error may include the path, never the secret value
		// (FR-051) — os.ReadFile's error never contains file content.
		return "", false, err
	}

	val := strings.TrimSuffix(string(data), "\n")
	if val == "" {
		return "", false, nil
	}
	return val, true, nil
}

// credentialName returns the $CREDENTIALS_DIRECTORY filename for ref,
// matching the file provider's "<bot>_<name>" / "<name>" convention.
func credentialName(ref domain.SecretRef) string {
	if ref.Bot != "" {
		return ref.Bot + "_" + ref.Name
	}
	return ref.Name
}
