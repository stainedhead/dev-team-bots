// Package file implements a domain.SecretProvider that wraps the existing
// ~/.boabot/credentials INI loader
// (internal/infrastructure/credentials.Load) unchanged.
//
// Per FR-043, the world-readable-file check in credentials.Load remains
// fatal: this provider surfaces that error verbatim rather than downgrading
// it to a miss or a warning. Per FR-045/B8, lookups are namespaced by
// SecretRef.Bot: when Bot is set, only the bot-scoped key "<bot>_<name>" is
// consulted; when Bot is empty, only the bare "<name>" key is consulted —
// see BotKey/GlobalKey below for the exact convention, which is stable and
// becomes the on-disk contract for operators' ~/.boabot/credentials files.
// There is deliberately no fallback from a bot-scoped lookup to the global
// key: a per-bot secret (e.g. a Buzz nsec) that is absent under its
// bot-scoped key must be a miss, not a silent match against a differently-
// scoped entry — two bots must never end up sharing one private key because
// an operator only ever provisioned the unscoped key. This matches the
// systemd and keystore providers, which are likewise strict-match, not
// fallback.
package file

import (
	"context"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/credentials"
)

const providerName = "file"

// Provider resolves secrets from the ~/.boabot/credentials INI file.
type Provider struct {
	path string
}

// New returns a new file Provider reading the credentials file at path.
func New(path string) *Provider {
	return &Provider{path: path}
}

// Name returns the provider name used in diagnostics and error messages.
func (p *Provider) Name() string {
	return providerName
}

// Lookup resolves ref against the credentials file. A missing file, or a
// file missing the requested key, is a miss (ok=false, err=nil). A
// world-readable file is a fatal error (FR-043), propagated unchanged from
// credentials.Load. See the package doc for the strict-match (no
// bot-to-global fallback) namespacing rule.
func (p *Provider) Lookup(_ context.Context, ref domain.SecretRef) (string, bool, error) {
	creds, err := credentials.Load(p.path)
	if err != nil {
		// credentials.Load's own error already never includes any secret
		// value — it only names the file path and its mode (FR-051).
		return "", false, err
	}

	key := GlobalKey(ref)
	if ref.Bot != "" {
		key = BotKey(ref)
	}

	if v, ok := creds[key]; ok && v != "" {
		return v, true, nil
	}

	return "", false, nil
}

// BotKey returns the bot-namespaced credentials-file key for ref:
// "<bot>_<name>".
func BotKey(ref domain.SecretRef) string {
	return ref.Bot + "_" + ref.Name
}

// GlobalKey returns the un-namespaced credentials-file key for ref: "<name>".
func GlobalKey(ref domain.SecretRef) string {
	return ref.Name
}
