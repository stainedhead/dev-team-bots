// Package secret implements domain.SecretStore as an ordered provider
// chain: the first configured provider to resolve a domain.SecretRef wins.
//
// The default caller-facing chain (FR-040) is env → systemd → keystore →
// file, but Store itself is order-agnostic — it simply tries the
// domain.SecretProvider slice it was constructed with, in order, and the
// order and membership of that slice are entirely up to the caller (any
// provider is omissible).
package secret

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// defaultProviderTimeout bounds how long Store.Get waits on any single
// provider before treating it as a miss and moving to the next one. It is
// a per-provider deadline, not a whole-chain deadline (architecture.md
// §Edge Cases: "one deadline per provider, not one for the whole chain").
const defaultProviderTimeout = 2 * time.Second

// Option configures a Store.
type Option func(*Store)

// WithProviderTimeout overrides the default 2s per-provider timeout.
func WithProviderTimeout(d time.Duration) Option {
	return func(s *Store) { s.timeout = d }
}

// WithLogger overrides the logger used for provider-error and
// provider-timeout diagnostics. Neither ever includes a secret value
// (FR-051) — only the provider name and the reference name are logged.
func WithLogger(l *slog.Logger) Option {
	return func(s *Store) { s.logger = l }
}

// Store implements domain.SecretStore as an ordered provider chain.
type Store struct {
	providers []domain.SecretProvider
	timeout   time.Duration
	logger    *slog.Logger
}

var _ domain.SecretStore = (*Store)(nil)

// New returns a Store that tries providers in the given order, first hit
// wins.
func New(providers []domain.SecretProvider, opts ...Option) *Store {
	s := &Store{
		providers: providers,
		timeout:   defaultProviderTimeout,
		logger:    slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Get resolves ref by consulting each configured provider in order and
// returning the first hit. A provider miss or a provider error is not
// fatal to the chain (FR-039) — resolution moves on to the next provider.
// A provider that does not return within the per-provider timeout is
// treated the same way: as a miss, logged distinctly from a genuine
// not-found. If no provider resolves ref, the returned error names the
// reference and enumerates every provider consulted (FR-053).
func (s *Store) Get(ctx context.Context, ref domain.SecretRef) (string, error) {
	names := make([]string, 0, len(s.providers))
	var errs []error

	for _, p := range s.providers {
		name := p.Name()
		names = append(names, name)

		v, ok, timedOut, err := s.callProvider(ctx, p, ref)
		switch {
		case timedOut:
			s.logger.Warn("secret provider timeout", "provider", name, "ref", ref.Name)
			continue
		case err != nil:
			s.logger.Warn("secret provider error", "provider", name, "ref", ref.Name, "err", err)
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		case ok:
			return v, nil
		}
	}

	return "", &NotFoundError{Ref: ref, Providers: names, Errs: errs}
}

// callProvider runs p.Lookup under a per-provider timeout, enforced at this
// call boundary rather than relying on the provider itself to observe
// context cancellation — a provider backed by a blocking, non-cancellable
// call (e.g. a subprocess or an unresponsive D-Bus round trip) still cannot
// block the chain past the timeout.
func (s *Store) callProvider(ctx context.Context, p domain.SecretProvider, ref domain.SecretRef) (value string, ok bool, timedOut bool, err error) {
	pctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	type result struct {
		value string
		ok    bool
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		v, ok, err := p.Lookup(pctx, ref)
		ch <- result{v, ok, err}
	}()

	select {
	case r := <-ch:
		return r.value, r.ok, false, r.err
	case <-pctx.Done():
		return "", false, true, nil
	}
}

// NotFoundError is returned by Store.Get when no provider in the chain
// resolves ref. It names the reference and every provider consulted
// (FR-053) and never includes a secret value (FR-051). Errs holds the
// non-nil errors (if any) encountered along the way, in provider order, so
// callers can inspect underlying causes with errors.Is/errors.As via
// Unwrap.
type NotFoundError struct {
	Ref       domain.SecretRef
	Providers []string
	Errs      []error
}

func (e *NotFoundError) Error() string {
	msg := fmt.Sprintf("secret: no provider resolved reference %q (bot=%q); providers consulted: %s",
		e.Ref.Name, e.Ref.Bot, strings.Join(e.Providers, ", "))
	if len(e.Errs) == 0 {
		return msg
	}
	parts := make([]string, len(e.Errs))
	for i, err := range e.Errs {
		parts[i] = err.Error()
	}
	return fmt.Sprintf("%s; provider errors: %s", msg, strings.Join(parts, "; "))
}

// Unwrap supports errors.Is/errors.As against the underlying per-provider
// errors collected during resolution.
func (e *NotFoundError) Unwrap() []error { return e.Errs }

var _ error = (*NotFoundError)(nil)
