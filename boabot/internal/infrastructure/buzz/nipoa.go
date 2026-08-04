// Package buzz -- this file implements NIP-OA (Owner Attestation) preimage
// construction and Schnorr sign/verify (Phase E, FR-005-FR-009). See the
// PRD's "NIP-OA in detail" section for the byte-exact specification this
// code is written against.
package buzz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"fiatjaf.com/nostr"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

// authTagPreimagePrefix is the literal, fixed prefix of the NIP-OA signing
// preimage. Per the PRD: exactly "nostr:agent-auth:" ‖ event.pubkey ‖ ":" ‖
// <conditions>, UTF-8 bytes, no additional separators.
const authTagPreimagePrefix = "nostr:agent-auth:"

// maxConditionKind is the maximum valid Nostr event kind a "kind=" clause
// may name. NIP-01 kinds are a 16-bit field (0-65535) -- the same range
// fiatjaf.com/nostr's own nostr.Kind (uint16) and this package's
// translate.go already enforce elsewhere.
const maxConditionKind = 65535

// Sentinel errors for NIP-OA auth-tag validation (FR-007). Each names a
// distinct rejection reason so tests -- and, later, operator-facing logs --
// can tell which specific rule an invalid tag violated, rather than
// collapsing every failure into one generic "invalid auth tag" error.
var (
	// ErrAuthTagElementCount is returned when a tag does not have exactly
	// four elements ("auth", owner-pubkey-hex, conditions, sig-hex).
	ErrAuthTagElementCount = errors.New("buzz: nip-oa: auth tag must have exactly four elements")

	// ErrAuthTagWrongName is returned when tag[0] is not literally "auth".
	ErrAuthTagWrongName = errors.New(`buzz: nip-oa: tag name is not "auth"`)

	// ErrAuthTagOwnerIsAgent is returned when the tag's owner pubkey equals
	// the agent's own pubkey (event.pubkey) -- explicitly invalid per the
	// PRD: "owner-pubkey == event.pubkey ⇒ invalid."
	ErrAuthTagOwnerIsAgent = errors.New("buzz: nip-oa: owner pubkey must not equal the agent's own pubkey")

	// ErrAuthTagInvalidConditions is returned when the <conditions> string
	// fails NIP-OA's clause grammar: whitespace, leading/trailing/doubled
	// '&', a clause that isn't kind=/created_at</created_at>, a
	// non-canonical decimal, or an out-of-range value.
	ErrAuthTagInvalidConditions = errors.New("buzz: nip-oa: conditions string is malformed")

	// ErrAuthTagInvalidPubkey is returned when the owner pubkey hex does
	// not decode to a valid secp256k1 curve point.
	ErrAuthTagInvalidPubkey = errors.New("buzz: nip-oa: owner pubkey is not a valid hex-encoded curve point")

	// ErrAuthTagInvalidSignature is returned when the signature hex is
	// malformed, or decodes but does not verify against the preimage hash
	// (this also covers a conditions string that was reordered/altered
	// after signing -- the hash no longer matches, so verification fails
	// with this same error).
	ErrAuthTagInvalidSignature = errors.New("buzz: nip-oa: owner signature does not verify")

	// ErrNoAuthTag indicates no usable auth tag was found on an event's
	// tags: either none is present, or -- per the PRD: "More than one
	// `auth` tag on an event ⇒ treat as having none" -- more than one is
	// present, which is treated identically to none rather than an
	// arbitrary tag being picked.
	ErrNoAuthTag = errors.New("buzz: nip-oa: no usable auth tag (absent, or more than one present)")
)

// Preimage returns the exact UTF-8 bytes of the NIP-OA signing preimage:
// "nostr:agent-auth:" ‖ agentPubkeyHex ‖ ":" ‖ conditions. conditions is
// used verbatim -- this function never reorders, deduplicates, or
// normalizes it, even if the caller passes something that "looks like" it
// should be canonicalized. Clause order is part of the signed message.
func Preimage(agentPubkeyHex, conditions string) []byte {
	var b strings.Builder
	b.Grow(len(authTagPreimagePrefix) + len(agentPubkeyHex) + 1 + len(conditions))
	b.WriteString(authTagPreimagePrefix)
	b.WriteString(agentPubkeyHex)
	b.WriteByte(':')
	b.WriteString(conditions)
	return []byte(b.String())
}

// PreimageHash returns SHA-256(Preimage(agentPubkeyHex, conditions)) -- the
// message a NIP-OA "auth" tag's signature is computed and verified over.
func PreimageHash(agentPubkeyHex, conditions string) [32]byte {
	return sha256.Sum256(Preimage(agentPubkeyHex, conditions))
}

// conditionClauseRe matches a single valid NIP-OA condition clause:
// kind=<decimal>, created_at<<ts>, or created_at><ts>. Capture groups
// distinguish which form matched so ValidateConditions can apply the
// kind-specific range check.
var conditionClauseRe = regexp.MustCompile(`^(?:kind=([0-9]+)|created_at<([0-9]+)|created_at>([0-9]+))$`)

// ValidateConditions validates a NIP-OA <conditions> string against the
// PRD's exact grammar: empty, or clause[&clause...] where each clause is
// kind=<decimal>, created_at<<ts>, or created_at><ts>, with no whitespace,
// no leading/trailing/doubled '&', and canonical base-10 values.
//
// This function only validates syntax -- it never rewrites, sorts, or
// deduplicates the string. Callers (Preimage, SignAuthTag, ValidateAuthTag)
// always use the original, unmodified conditions value.
func ValidateConditions(conditions string) error {
	if conditions == "" {
		return nil
	}

	if strings.IndexFunc(conditions, isASCIIOrUnicodeSpace) >= 0 {
		return fmt.Errorf("%w: whitespace is not allowed in conditions %q", ErrAuthTagInvalidConditions, conditions)
	}
	if strings.HasPrefix(conditions, "&") {
		return fmt.Errorf("%w: leading '&' in conditions %q", ErrAuthTagInvalidConditions, conditions)
	}
	if strings.HasSuffix(conditions, "&") {
		return fmt.Errorf("%w: trailing '&' in conditions %q", ErrAuthTagInvalidConditions, conditions)
	}
	if strings.Contains(conditions, "&&") {
		return fmt.Errorf("%w: doubled '&' in conditions %q", ErrAuthTagInvalidConditions, conditions)
	}

	for _, clause := range strings.Split(conditions, "&") {
		if err := validateClause(clause); err != nil {
			return err
		}
	}
	return nil
}

func isASCIIOrUnicodeSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

func validateClause(clause string) error {
	m := conditionClauseRe.FindStringSubmatch(clause)
	if m == nil {
		return fmt.Errorf("%w: unrecognized clause %q", ErrAuthTagInvalidConditions, clause)
	}

	kindDecimal, ltDecimal, gtDecimal := m[1], m[2], m[3]
	decimal := kindDecimal
	if decimal == "" {
		decimal = ltDecimal
	}
	if decimal == "" {
		decimal = gtDecimal
	}

	if !isCanonicalDecimal(decimal) {
		return fmt.Errorf("%w: non-canonical decimal %q in clause %q", ErrAuthTagInvalidConditions, decimal, clause)
	}

	if kindDecimal != "" {
		v, err := strconv.ParseUint(kindDecimal, 10, 64)
		if err != nil || v > maxConditionKind {
			return fmt.Errorf("%w: kind %q out of range [0,%d] in clause %q", ErrAuthTagInvalidConditions, kindDecimal, maxConditionKind, clause)
		}
	} else {
		if _, err := strconv.ParseUint(decimal, 10, 64); err != nil {
			return fmt.Errorf("%w: created_at timestamp %q out of range in clause %q", ErrAuthTagInvalidConditions, decimal, clause)
		}
	}
	return nil
}

// isCanonicalDecimal reports whether s (already known, by conditionClauseRe,
// to be one or more ASCII digits) is canonical base-10: no leading zero
// unless the whole value is "0".
func isCanonicalDecimal(s string) bool {
	if s == "0" {
		return true
	}
	return s[0] != '0'
}

// SignAuthTag builds and signs a NIP-OA "auth" tag: the owner (ownerSK)
// attests that agentPubkeyHex may act under conditions. It validates
// conditions and rejects owner-pubkey == agent-pubkey before signing, so a
// caller never has to separately validate a tag this function just
// produced.
func SignAuthTag(ownerSK nostr.SecretKey, agentPubkeyHex, conditions string) ([]string, error) {
	if err := ValidateConditions(conditions); err != nil {
		return nil, err
	}

	ownerPK := ownerSK.Public()
	if strings.EqualFold(ownerPK.Hex(), agentPubkeyHex) {
		return nil, ErrAuthTagOwnerIsAgent
	}

	hash := PreimageHash(agentPubkeyHex, conditions)

	btcSK, _ := btcec.PrivKeyFromBytes(ownerSK[:])
	sig, err := schnorr.Sign(btcSK, hash[:])
	if err != nil {
		return nil, fmt.Errorf("buzz: nip-oa: sign: %w", err)
	}

	return []string{"auth", ownerPK.Hex(), conditions, hex.EncodeToString(sig.Serialize())}, nil
}

// ValidateAuthTag validates tag as a NIP-OA "auth" tag issued for the agent
// identified by agentPubkeyHex (i.e. the "event.pubkey" the tag would ride
// on), per FR-007: exactly four elements, tag name "auth", owner pubkey !=
// agent pubkey, syntactically valid conditions (used verbatim, never
// reordered/deduplicated/normalized), a well-formed owner pubkey, and a
// verifying Schnorr signature over SHA256(preimage).
func ValidateAuthTag(tag []string, agentPubkeyHex string) error {
	if len(tag) != 4 {
		return fmt.Errorf("%w: got %d element(s)", ErrAuthTagElementCount, len(tag))
	}
	if tag[0] != "auth" {
		return fmt.Errorf("%w: got %q", ErrAuthTagWrongName, tag[0])
	}

	ownerHex, conditions, sigHex := tag[1], tag[2], tag[3]

	if strings.EqualFold(ownerHex, agentPubkeyHex) {
		return ErrAuthTagOwnerIsAgent
	}

	if err := ValidateConditions(conditions); err != nil {
		return err
	}

	ownerPK, err := nostr.PubKeyFromHex(ownerHex)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthTagInvalidPubkey, err)
	}
	ownerBTCPub, err := schnorr.ParsePubKey(ownerPK[:])
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthTagInvalidPubkey, err)
	}

	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return fmt.Errorf("%w: signature is not valid hex: %v", ErrAuthTagInvalidSignature, err)
	}
	sig, err := schnorr.ParseSignature(sigBytes)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthTagInvalidSignature, err)
	}

	hash := PreimageHash(agentPubkeyHex, conditions)
	if !sig.Verify(hash[:], ownerBTCPub) {
		return ErrAuthTagInvalidSignature
	}
	return nil
}

// FindAuthTag scans tags for a tag named "auth" and returns it. Per the
// PRD: "More than one `auth` tag on an event ⇒ treat as having none" -- so
// zero or two-or-more matches both return (nil, ErrNoAuthTag), never an
// arbitrarily-chosen tag.
func FindAuthTag(tags nostr.Tags) ([]string, error) {
	var found []string
	count := 0
	for _, t := range tags {
		if len(t) > 0 && t[0] == "auth" {
			count++
			found = []string(t)
		}
	}
	if count != 1 {
		return nil, ErrNoAuthTag
	}
	return found, nil
}

// StaticAuthTagFunc returns an AuthTagFunc (D5's extension point, see
// relay_client.go) that always returns tag unchanged. tag is validated once
// up front against agentPubkeyHex (FR-007: "validate a configured auth tag
// locally at startup before use") so a misconfigured tag fails fast at
// construction time rather than silently on every AUTH attempt.
//
// The NIP-OA tag is a reusable capability, not a per-event artifact -- the
// PRD: "the same tag may appear on many events by the same agent key,
// provided each satisfies the conditions" -- so returning the same
// pre-validated tag on every call is correct; there is nothing to
// recompute per AUTH attempt.
func StaticAuthTagFunc(tag []string, agentPubkeyHex string) (AuthTagFunc, error) {
	if err := ValidateAuthTag(tag, agentPubkeyHex); err != nil {
		return nil, fmt.Errorf("buzz: nip-oa: invalid configured auth tag: %w", err)
	}
	frozen := append([]string(nil), tag...)
	return func(_ context.Context) ([]string, error) {
		return frozen, nil
	}, nil
}
