package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/config"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret"
)

// secretDiagEntry reports, for one configured secret, which provider
// resolved it — never the value (FR-050, FR-051 diagnostic half).
type secretDiagEntry struct {
	Ref       domain.SecretRef
	Resolved  bool
	Provider  string   // name of the provider that resolved it; empty if unresolved
	Consulted []string // provider names consulted, in order
}

// runDiagSecrets implements the "boabot --diag-secrets" diagnostic command
// (FR-050): for each secret this bot's config could need, it reports which
// provider resolved it, by name only, then exits. It is surfaced as a flag
// on the existing boabot binary rather than a separate subcommand or
// binary, since main.go already uses the stdlib flag package (no cobra
// dependency exists in this module) and the diagnostic needs the same
// config file and provider wiring as a normal run.
func runDiagSecrets(cfg config.Config, w io.Writer) error {
	providers, err := buildSecretProviders()
	if err != nil {
		return err
	}
	refs := secretRefsForDiagnostics(cfg)
	entries := diagnoseSecrets(context.Background(), providers, refs, slog.Default())
	printSecretDiagnostics(w, entries)
	return nil
}

// secretRefsForDiagnostics returns the set of domain.SecretRef this bot's
// config could resolve: the two secrets migrated in C1 (always), plus the
// two Slack tokens (only when a Slack bot name is configured, since Slack
// can never activate without one — see cfg.Slack.ResolveSecrets).
func secretRefsForDiagnostics(cfg config.Config) []domain.SecretRef {
	refs := []domain.SecretRef{
		{Name: "anthropic_api_key"},
		{Name: "boabot_backup_token"},
	}
	if cfg.Slack.BotName != "" {
		refs = append(refs,
			domain.SecretRef{Name: "slack_bot_token", Bot: cfg.Slack.BotName},
			domain.SecretRef{Name: "slack_app_token", Bot: cfg.Slack.BotName},
		)
	}
	return refs
}

// diagnoseSecrets resolves each ref against providers, one provider at a
// time in order, reporting only which provider (if any) resolved it — the
// secret value itself is discarded immediately and never returned.
//
// Each provider is tried through its own single-provider secret.Store
// rather than by calling provider.Lookup directly, so the diagnostic
// inherits Store's per-provider timeout and non-halting-on-error semantics
// exactly (a hung keystore/D-Bus provider cannot hang the diagnostic any
// longer than a normal secret resolution would) without needing any change
// to the already-merged Phase B store.go.
func diagnoseSecrets(ctx context.Context, providers []domain.SecretProvider, refs []domain.SecretRef, logger *slog.Logger) []secretDiagEntry {
	entries := make([]secretDiagEntry, 0, len(refs))
	for _, ref := range refs {
		entry := secretDiagEntry{Ref: ref}
		for _, p := range providers {
			entry.Consulted = append(entry.Consulted, p.Name())
			s := secret.New([]domain.SecretProvider{p}, secret.WithLogger(logger))
			if _, err := s.Get(ctx, ref); err == nil {
				entry.Resolved = true
				entry.Provider = p.Name()
				break
			}
			// Miss or provider error: single-provider Store.Get returns a
			// *secret.NotFoundError in that case — move on to the next
			// provider in the chain.
		}
		entries = append(entries, entry)
	}
	return entries
}

// printSecretDiagnostics writes one line per entry to w, naming only the
// secret reference and the resolving provider (or the providers consulted,
// on a miss) — the secret value is never available to this function to
// begin with, by construction (FR-050, FR-051 diagnostic half).
func printSecretDiagnostics(w io.Writer, entries []secretDiagEntry) {
	for _, e := range entries {
		label := e.Ref.Name
		if e.Ref.Bot != "" {
			label = fmt.Sprintf("%s (bot=%s)", e.Ref.Name, e.Ref.Bot)
		}
		if e.Resolved {
			fmt.Fprintf(w, "%s: resolved by %s\n", label, e.Provider) //nolint:errcheck
		} else {
			fmt.Fprintf(w, "%s: unresolved (checked: %s)\n", label, strings.Join(e.Consulted, ", ")) //nolint:errcheck
		}
	}
}
