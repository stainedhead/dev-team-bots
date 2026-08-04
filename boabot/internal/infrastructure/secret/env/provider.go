// Package env implements a domain.SecretProvider backed by process
// environment variables.
//
// Per FR-044, this provider is intentionally process-global: it does NOT
// namespace lookups by SecretRef.Bot. An explicitly-set environment
// variable always wins over every other provider in the chain, matching
// BaoBot's pre-existing precedence (an env var has always overridden the
// credentials file — see internal/infrastructure/credentials). Namespacing
// an env var by bot would be surprising given env vars are inherently
// scoped to the whole process, not to one bot's identity within it.
//
// The env var name is derived from SecretRef.Name by upper-casing it: the
// logical name "buzz_private_key" resolves the environment variable
// BUZZ_PRIVATE_KEY. An env var that is unset, or set to the empty string,
// is treated as a miss — an operator who clears a var is assumed to want
// resolution to fall through to the next provider, not to pin an empty
// secret.
package env

import (
	"context"
	"os"
	"strings"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

const providerName = "env"

// Provider resolves secrets from environment variables.
type Provider struct{}

// New returns a new env Provider.
func New() *Provider {
	return &Provider{}
}

// Name returns the provider name used in diagnostics and error messages.
func (p *Provider) Name() string {
	return providerName
}

// Lookup resolves ref against the process environment. See the package doc
// for the naming convention and the empty-value-is-a-miss rule.
func (p *Provider) Lookup(_ context.Context, ref domain.SecretRef) (string, bool, error) {
	v := os.Getenv(envVarName(ref))
	if v == "" {
		return "", false, nil
	}
	return v, true, nil
}

// envVarName derives the environment variable name for ref. Bot is
// intentionally ignored — see package doc.
func envVarName(ref domain.SecretRef) string {
	return strings.ToUpper(ref.Name)
}
