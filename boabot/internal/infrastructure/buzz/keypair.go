package buzz

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// PrivateKeySecretName is the domain.SecretRef.Name used to resolve the
// Buzz agent's private key (nsec) through domain.SecretStore, per FR-002.
// The default provider chain (Phase B/C) resolves this from the
// BUZZ_PRIVATE_KEY environment variable, falling back to a
// buzz_private_key entry in ~/.boabot/credentials.
const PrivateKeySecretName = "buzz_private_key"

// errMalformedPrivateKey is returned (never wrapping a library error) when
// a resolved secret value is not a valid nsec1... bech32 string or 64-char
// hex secret key. It deliberately never echoes the offending value: some
// underlying parse errors (e.g. hex.Decode's "%q is not valid hex") embed
// the input verbatim, which would leak the secret into logs the moment a
// caller logs this error (FR-002, FR-051).
var errMalformedPrivateKey = errors.New("buzz: value is not a valid nsec1… bech32 string or 64-character hex secret key")

// errZeroPrivateKey is returned when the resolved key decodes to the
// all-zero scalar, which is not a valid secp256k1 private key.
var errZeroPrivateKey = errors.New("buzz: private key is the all-zero value, which is not a valid secp256k1 key")

// errPubkeyDerivationFailed is returned when a key parses syntactically but
// its derived public key is not a valid curve point.
var errPubkeyDerivationFailed = errors.New("buzz: private key does not derive a valid public key")

// LoadKeypair resolves the Buzz agent's secp256k1 keypair (FR-001) through
// store, scoped to botName's namespace (per FR-045/OQ-9), and derives its
// public key.
//
// It fails closed (FR-003): a missing secret, a value that is not a valid
// nsec1... bech32 string or 64-char hex secret key, an all-zero key, or a
// key whose public key fails to derive/validate all return a non-nil
// error and the zero value for both keys. The returned error never
// contains the raw secret value, so it is always safe for a caller to log
// (FR-002, FR-051) -- callers are expected to log the returned error and
// then decline to start the Buzz monitor, leaving every other channel
// monitor unaffected, per FR-003.
//
// This is a standalone function, not wired to any monitor lifecycle: that
// wiring ("log the error and continue starting other channels") is a
// later phase's job (see tasks.md Phase H).
func LoadKeypair(ctx context.Context, store domain.SecretStore, botName string) (nostr.SecretKey, nostr.PubKey, error) {
	var zeroSK nostr.SecretKey
	var zeroPK nostr.PubKey

	raw, err := store.Get(ctx, domain.SecretRef{Name: PrivateKeySecretName, Bot: botName})
	if err != nil {
		return zeroSK, zeroPK, fmt.Errorf("buzz: failed to resolve private key: %w", err)
	}

	sk, err := parseSecretKey(raw)
	if err != nil {
		return zeroSK, zeroPK, err
	}

	if sk == zeroSK {
		return zeroSK, zeroPK, errZeroPrivateKey
	}

	pk := sk.Public()
	if _, verr := nostr.PubKeyFromHex(pk.Hex()); verr != nil {
		return zeroSK, zeroPK, errPubkeyDerivationFailed
	}

	return sk, pk, nil
}

// parseSecretKey accepts either a bech32 nsec1... string (NIP-19, the
// format buzz-admin generate-key prints) or a raw 64-char hex secret key.
// It never returns an error that embeds raw, even indirectly through an
// underlying library error message.
func parseSecretKey(raw string) (nostr.SecretKey, error) {
	var zero nostr.SecretKey

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return zero, errMalformedPrivateKey
	}

	if strings.HasPrefix(trimmed, "nsec1") {
		prefix, value, err := nip19.Decode(trimmed)
		if err != nil || prefix != "nsec" {
			return zero, errMalformedPrivateKey
		}
		sk, ok := value.(nostr.SecretKey)
		if !ok {
			return zero, errMalformedPrivateKey
		}
		return sk, nil
	}

	sk, err := nostr.SecretKeyFromHex(trimmed)
	if err != nil {
		return zero, errMalformedPrivateKey
	}
	return sk, nil
}
