package buzz

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/secret"
)

// APITokenSecretName is the domain.SecretRef.Name used to resolve
// BUZZ_API_TOKEN through domain.SecretStore, per FR-010. The default
// provider chain resolves this from the BUZZ_API_TOKEN environment
// variable, falling back to a buzz_api_token credentials-file entry --
// the same resolution path as PrivateKeySecretName (FR-002).
const APITokenSecretName = "buzz_api_token"

// LoadAPIToken resolves BUZZ_API_TOKEN through store, scoped to botName.
// Unlike LoadKeypair, an all-provider-miss resolution is not itself an
// error: the token is optional unless the relay requires it
// (WithAPIToken's `required` flag, enforced at Connect time), so
// LoadAPIToken reports a clean miss via found=false rather than failing
// the caller.
//
// A genuine provider failure is different from a clean miss (FR-053
// exists precisely so an operator can tell them apart) and is NOT
// swallowed here: it is distinguished via errors.As against
// *secret.NotFoundError -- the sentinel-ish error internal/infrastructure/
// secret.Store.Get returns specifically for "every provider was
// consulted and none had this reference" (see internal/infrastructure/
// secret/store.go). Any other error (a provider timeout surfaced as part
// of a wrapped chain error, a malformed value, etc.) is returned to the
// caller to log/act on, per FR-053's intent.
func LoadAPIToken(ctx context.Context, store domain.SecretStore, botName string) (token string, found bool, err error) {
	raw, err := store.Get(ctx, domain.SecretRef{Name: APITokenSecretName, Bot: botName})
	if err != nil {
		var notFound *secret.NotFoundError
		if errors.As(err, &notFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("buzz: failed to resolve API token: %w", err)
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false, nil
	}
	return trimmed, true, nil
}

// AuthTagSecretName is the domain.SecretRef.Name used to resolve an
// optional NIP-OA "auth" tag through domain.SecretStore, per FR-001 of the
// buzz-support-auto-review PRD. The default provider chain resolves this
// the same way as PrivateKeySecretName/APITokenSecretName -- environment
// variable, systemd credential, OS keystore, or ~/.boabot/credentials,
// first hit wins.
//
// The resolved value is a single pipe-delimited string,
// "owner_pubkey_hex|conditions|sig_hex" (research.md's OQ-R1 resolution),
// matching SignAuthTag's own tag-construction shape (tag[1], tag[2],
// tag[3]) exactly -- boabotctl treats it as an opaque string like every
// other secret; boabot itself is the only thing that parses it, in
// LoadAuthTag below. Pipe-delimiting is safe: ValidateConditions rejects
// any whitespace in <conditions>, and every valid clause is anchored to
// kind=<decimal>, created_at<<decimal>, or created_at><decimal> joined by
// '&' -- a literal '|' can never appear inside a valid conditions string,
// and the other two fields are hex-encoded.
const AuthTagSecretName = "buzz_auth_tag"

// errAuthTagFieldCount is returned when the resolved buzz_auth_tag secret
// does not split into exactly three pipe-delimited fields. strings.Split
// (not SplitN) is used deliberately: a stray '|' inside what was meant to
// be the signature field is a hard error here, not silently absorbed into
// it.
var errAuthTagFieldCount = errors.New("buzz: auth tag secret must have exactly three pipe-delimited fields (owner_pubkey_hex|conditions|sig_hex)")

// LoadAuthTag resolves an optional NIP-OA "auth" tag (FR-001) through
// store, scoped to botName, parses its pipe-delimited
// owner_pubkey_hex|conditions|sig_hex format into a 4-element ["auth", ...]
// tag, and validates it against agentPubkeyHex via StaticAuthTagFunc
// before returning it -- so a misconfigured tag is caught here, at
// startup, rather than silently on every AUTH attempt.
//
// Like LoadAPIToken, an all-provider-miss resolution is not itself an
// error: the tag is optional -- a bot that only needs to act as an
// explicitly-enrolled channel member legitimately has none configured, so
// LoadAuthTag reports a clean miss via found=false rather than failing the
// caller, matching FR-001's Green guidance: "log and continue without the
// tag ... not fail closed."
//
// A genuine provider failure, a malformed value (wrong field count), or a
// value that fails StaticAuthTagFunc's own validation (bad pubkey, invalid
// conditions grammar, non-verifying signature, owner == agent) is distinct
// from a clean miss and is returned as a non-nil error for the caller to
// log and act on -- the caller is expected to continue without the tag
// rather than fail Buzz activation, the same "optional secret" treatment
// LoadAPIToken already gives every other resolution failure.
func LoadAuthTag(ctx context.Context, store domain.SecretStore, botName, agentPubkeyHex string) (AuthTagFunc, bool, error) {
	raw, err := store.Get(ctx, domain.SecretRef{Name: AuthTagSecretName, Bot: botName})
	if err != nil {
		var notFound *secret.NotFoundError
		if errors.As(err, &notFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("buzz: failed to resolve auth tag: %w", err)
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false, nil
	}

	parts := strings.Split(trimmed, "|")
	if len(parts) != 3 {
		return nil, false, fmt.Errorf("%w: got %d field(s)", errAuthTagFieldCount, len(parts))
	}

	tag := []string{"auth", parts[0], parts[1], parts[2]}
	fn, err := StaticAuthTagFunc(tag, agentPubkeyHex)
	if err != nil {
		return nil, false, fmt.Errorf("buzz: configured auth tag failed validation: %w", err)
	}
	return fn, true, nil
}
