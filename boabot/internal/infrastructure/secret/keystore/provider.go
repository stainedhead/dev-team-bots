// Package keystore implements a domain.SecretProvider over the OS-native
// keystore (macOS Keychain, Windows Credential Manager, Linux Secret
// Service via D-Bus) using github.com/zalando/go-keyring.
//
// Key convention (FR-045 — stable, since it becomes the on-disk contract in
// users' keychains): the keyring "service" is always the constant
// "boabot"; the "user"/account name is "<bot>/<name>" when SecretRef.Bot is
// set, or the bare "<name>" for a global/shared secret (empty Bot). Bot
// namespaces on bot *name*, not bot type, per PRD OQ-9 — renaming a bot
// orphans its keychain entries; there is no migration path for that today.
//
// FR-052 (no secret as a subprocess argument): this package never
// constructs a subprocess itself — TestProvider_SourceNeverConstructsASubprocessDirectly
// guards that mechanically. The only backend that shells out is
// zalando/go-keyring's darwin implementation (keyring_darwin.go), which
// writes via `security -i` and an stdin pipe, never as a `-w` command-line
// argument — verified by reading that file in the module cache
// (github.com/zalando/go-keyring@v0.2.8/keyring_darwin.go) during the B1
// research spike; see implementation-notes.md. This MUST be re-verified on
// any go-keyring upgrade.
//
// FR-041 (per-platform validation): whether the darwin backend's implicit
// keychain search list reaches the System keychain from a root LaunchDaemon
// is unverified in this codebase and is out of scope for unit tests — see
// the //go:build integration stubs and implementation-notes.md's manual
// verification checklist.
package keystore

import (
	"context"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

const (
	providerName = "keystore"
	serviceName  = "boabot"
)

// backend abstracts the zalando/go-keyring package-level functions so
// Provider can be unit-tested against the library's own MockInit/
// MockInitWithError seam without ever touching a real OS keystore or
// spawning a subprocess.
type backend interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type libBackend struct{}

func (libBackend) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (libBackend) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (libBackend) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

// Provider resolves secrets from the OS-native keystore.
type Provider struct {
	b backend
}

// New returns a new keystore Provider backed by the real OS keystore.
func New() *Provider {
	return &Provider{b: libBackend{}}
}

// Name returns the provider name used in diagnostics and error messages.
func (p *Provider) Name() string {
	return providerName
}

// Lookup resolves ref from the OS keystore. A not-found entry is a miss
// (ok=false, err=nil); any other backend error is reported, naming the
// reference only — never the secret value (FR-051).
func (p *Provider) Lookup(_ context.Context, ref domain.SecretRef) (string, bool, error) {
	v, err := p.b.Get(serviceName, accountName(ref))
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("keystore: lookup %s: %w", ref.Name, err)
	}
	return v, true, nil
}

// Set stores value in the OS keystore under ref's convention. Errors never
// include the value (FR-051); the value never reaches a subprocess argument
// list (FR-052) — see package doc.
func (p *Provider) Set(_ context.Context, ref domain.SecretRef, value string) error {
	if err := p.b.Set(serviceName, accountName(ref), value); err != nil {
		return fmt.Errorf("keystore: set %s: %w", ref.Name, err)
	}
	return nil
}

// Delete removes ref's entry from the OS keystore. Deleting an entry that
// does not exist is not an error.
func (p *Provider) Delete(_ context.Context, ref domain.SecretRef) error {
	if err := p.b.Delete(serviceName, accountName(ref)); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("keystore: delete %s: %w", ref.Name, err)
	}
	return nil
}

// accountName returns the keyring "user" for ref, per the package's key
// convention.
func accountName(ref domain.SecretRef) string {
	if ref.Bot != "" {
		return ref.Bot + "/" + ref.Name
	}
	return ref.Name
}
