package buzz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
)

// testOwnerSK / testAgentSK are the NIP-OA published test-vector secrets
// the PRD names: owner secret 0x…01, agent secret 0x…02 (32-byte scalars,
// all-zero except the final byte). nostr.KeyOne is exactly the 0x…01 key
// (it's a named constant in fiatjaf.com/nostr, used for this same purpose
// upstream); testAgentSK is its 0x…02 counterpart, built the same way since
// the library does not export a "KeyTwo".
//
// The PRD quotes the NIP-OA spec's secrets but not a full precomputed
// signature-hex table, so these tests assert the sign/verify round trip
// (and every negative case the PRD's Acceptance Criteria enumerate)
// directly against these exact secrets and their derived pubkeys, which is
// the concrete, reproducible form of "assert against the published test
// vectors" available from what the PRD actually publishes.
var (
	testOwnerSK = nostr.KeyOne
	testAgentSK = mustAgentSK()
)

func mustAgentSK() nostr.SecretKey {
	sk, err := nostr.SecretKeyFromHex(strings.Repeat("0", 62) + "02")
	if err != nil {
		panic(err)
	}
	return sk
}

// hexPK64 returns a syntactically-valid-looking 64-char hex string starting
// with prefix, padded with zeros. Used where tests need a distinct-looking
// pubkey hex but don't need it to be a real curve point.
func hexPK64(prefix string) string {
	return prefix + strings.Repeat("0", 64-len(prefix))
}

// --- E1: preimage construction ---------------------------------------------

// TestPreimage_ExactBytes pins the byte-exact NIP-OA preimage layout quoted
// in the PRD: "nostr:agent-auth:" ‖ event.pubkey ‖ ":" ‖ <conditions>, UTF-8,
// no additional separators.
func TestPreimage_ExactBytes(t *testing.T) {
	agentPubkeyHex := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	conditions := "kind=1&created_at<1700000000"

	got := Preimage(agentPubkeyHex, conditions)
	want := []byte("nostr:agent-auth:" + agentPubkeyHex + ":" + conditions)

	if !bytes.Equal(got, want) {
		t.Fatalf("Preimage() = %q, want %q", got, want)
	}
}

// TestPreimage_EmptyConditions confirms the trailing separator is present
// even when <conditions> is the empty string (per NIP-OA, empty conditions
// is valid), since the preimage grammar has no "omit trailing colon" case.
func TestPreimage_EmptyConditions(t *testing.T) {
	agentPubkeyHex := hexPK64("bb")
	got := Preimage(agentPubkeyHex, "")
	want := []byte("nostr:agent-auth:" + agentPubkeyHex + ":")
	if !bytes.Equal(got, want) {
		t.Fatalf("Preimage() = %q, want %q", got, want)
	}
}

// TestPreimage_NeverReordersOrNormalizesConditions is E1's central
// requirement: the conditions string must be used verbatim. A conditions
// string that "looks like" it should be sorted or deduplicated must still
// appear byte-for-byte in the preimage.
func TestPreimage_NeverReordersOrNormalizesConditions(t *testing.T) {
	agentPubkeyHex := hexPK64("cc")
	// Deliberately "unsorted" and would collapse under naive dedup if that
	// were (wrongly) applied -- kind=2 before kind=1, and repeated.
	conditions := "kind=2&kind=1&kind=1"

	got := Preimage(agentPubkeyHex, conditions)
	if !bytes.Contains(got, []byte(conditions)) {
		t.Fatalf("Preimage() = %q, expected verbatim conditions %q embedded unchanged", got, conditions)
	}
}

func TestPreimageHash_IsSHA256OfPreimage(t *testing.T) {
	agentPubkeyHex := hexPK64("dd")
	conditions := "kind=9"

	got := PreimageHash(agentPubkeyHex, conditions)
	want := sha256.Sum256(Preimage(agentPubkeyHex, conditions))
	if got != want {
		t.Fatalf("PreimageHash() = %x, want %x", got, want)
	}
}

// --- E2: owner Schnorr sign/verify against the published test vectors ------

// TestSignAndValidateAuthTag_RoundTrip is the positive case: SignAuthTag
// (owner secret 0x…01) over agent pubkey (derived from secret 0x…02)
// produces a tag ValidateAuthTag accepts.
func TestSignAndValidateAuthTag_RoundTrip(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	conditions := "kind=9&created_at<2000000000"

	tag, err := SignAuthTag(testOwnerSK, agentPK, conditions)
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}
	if len(tag) != 4 {
		t.Fatalf("expected 4-element tag, got %d: %v", len(tag), tag)
	}
	if tag[0] != "auth" {
		t.Errorf("tag[0] = %q, want %q", tag[0], "auth")
	}
	if tag[1] != testOwnerSK.Public().Hex() {
		t.Errorf("tag[1] (owner pubkey) = %q, want %q", tag[1], testOwnerSK.Public().Hex())
	}
	if tag[2] != conditions {
		t.Errorf("tag[2] (conditions) = %q, want verbatim %q", tag[2], conditions)
	}

	if err := ValidateAuthTag(tag, agentPK); err != nil {
		t.Fatalf("ValidateAuthTag rejected a tag SignAuthTag just produced: %v", err)
	}
}

// TestSignAndValidateAuthTag_EmptyConditions covers the "conditions is
// empty" case the PRD calls out as valid.
func TestSignAndValidateAuthTag_EmptyConditions(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	tag, err := SignAuthTag(testOwnerSK, agentPK, "")
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}
	if err := ValidateAuthTag(tag, agentPK); err != nil {
		t.Fatalf("ValidateAuthTag: %v", err)
	}
}

// TestValidateAuthTag_WrongAgentPubkeyFailsVerification proves the
// signature is bound to the specific agent pubkey named in the preimage:
// validating the same tag against a different agent pubkey must fail.
func TestValidateAuthTag_WrongAgentPubkeyFailsVerification(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	tag, err := SignAuthTag(testOwnerSK, agentPK, "kind=1")
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}

	otherAgentPK := hexPK64("ee")
	if err := ValidateAuthTag(tag, otherAgentPK); !errors.Is(err, ErrAuthTagInvalidSignature) {
		t.Fatalf("ValidateAuthTag against the wrong agent pubkey: got %v, want ErrAuthTagInvalidSignature", err)
	}
}

// --- E2 negative cases (PRD Acceptance Criteria, quoted verbatim) ----------
//
// "NIP-OA sign and verify pass against the published test vectors,
// including negative cases: five-element tag, two `auth` tags, `owner ==
// agent` pubkey, whitespace in conditions, leading/trailing/doubled `&`,
// non-canonical decimal, out-of-range `kind`, reordered conditions
// string." Each gets its own test below, matching that enumeration
// one-for-one so it's unambiguous which rule a failure violates.

// TestNegative_FiveElementTag: "five-element tag".
func TestNegative_FiveElementTag(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	tag, err := SignAuthTag(testOwnerSK, agentPK, "kind=1")
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}
	fiveElement := append(append([]string(nil), tag...), "unexpected-fifth-element")

	if err := ValidateAuthTag(fiveElement, agentPK); !errors.Is(err, ErrAuthTagElementCount) {
		t.Fatalf("five-element tag: got %v, want ErrAuthTagElementCount", err)
	}
}

// TestNegative_TwoAuthTags: "two `auth` tags". Per the PRD, this is not
// merely rejected -- it must be *treated as having none*, which FindAuthTag
// implements by returning ErrNoAuthTag for both zero and multiple matches.
// This test targets the multiple-tags case specifically (see
// TestFindAuthTag_NoTagPresent for the "none" case as a distinct scenario).
func TestNegative_TwoAuthTags(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	tag, err := SignAuthTag(testOwnerSK, agentPK, "kind=1")
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}
	otherTag, err := SignAuthTag(testOwnerSK, agentPK, "kind=9")
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}

	tags := nostr.Tags{nostr.Tag(tag), nostr.Tag(otherTag)}
	got, err := FindAuthTag(tags)
	if !errors.Is(err, ErrNoAuthTag) {
		t.Fatalf("two auth tags: got (%v, %v), want (nil, ErrNoAuthTag)", got, err)
	}
	if got != nil {
		t.Errorf("two auth tags: expected nil tag, got %v", got)
	}
}

// TestFindAuthTag_NoTagPresent is the "none present" scenario, kept
// distinct from TestNegative_TwoAuthTags even though both currently return
// the same sentinel, because the PRD calls them out as separate cases.
func TestFindAuthTag_NoTagPresent(t *testing.T) {
	tags := nostr.Tags{{"relay", "wss://example"}, {"challenge", "abc"}}
	if _, err := FindAuthTag(tags); !errors.Is(err, ErrNoAuthTag) {
		t.Fatalf("no auth tag present: got %v, want ErrNoAuthTag", err)
	}
}

// TestFindAuthTag_ExactlyOneTagPresent is the positive control for
// FindAuthTag, so the "treat as none" tests above are contrasted against a
// case that must succeed.
func TestFindAuthTag_ExactlyOneTagPresent(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	tag, err := SignAuthTag(testOwnerSK, agentPK, "kind=1")
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}
	tags := nostr.Tags{{"relay", "wss://example"}, nostr.Tag(tag)}

	got, err := FindAuthTag(tags)
	if err != nil {
		t.Fatalf("FindAuthTag: %v", err)
	}
	if len(got) != 4 || got[0] != "auth" {
		t.Fatalf("FindAuthTag returned %v, want the auth tag", got)
	}
}

// TestNegative_OwnerEqualsAgentPubkey: "`owner == agent` pubkey".
func TestNegative_OwnerEqualsAgentPubkey(t *testing.T) {
	samePK := testOwnerSK.Public().Hex()
	if _, err := SignAuthTag(testOwnerSK, samePK, "kind=1"); !errors.Is(err, ErrAuthTagOwnerIsAgent) {
		t.Fatalf("SignAuthTag with owner==agent: got %v, want ErrAuthTagOwnerIsAgent", err)
	}

	// Also exercise ValidateAuthTag directly, in case a tag with this
	// defect arrives from somewhere other than SignAuthTag (e.g. a
	// hand-authored config value).
	hostileTag := []string{"auth", samePK, "kind=1", "00"}
	if err := ValidateAuthTag(hostileTag, samePK); !errors.Is(err, ErrAuthTagOwnerIsAgent) {
		t.Fatalf("ValidateAuthTag with owner==agent: got %v, want ErrAuthTagOwnerIsAgent", err)
	}
}

// TestNegative_WhitespaceInConditions: "whitespace in conditions".
func TestNegative_WhitespaceInConditions(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	if _, err := SignAuthTag(testOwnerSK, agentPK, "kind=1 &kind=2"); !errors.Is(err, ErrAuthTagInvalidConditions) {
		t.Fatalf("whitespace in conditions: got %v, want ErrAuthTagInvalidConditions", err)
	}
}

// TestNegative_LeadingAmpersand: "leading ... `&`".
func TestNegative_LeadingAmpersand(t *testing.T) {
	if err := ValidateConditions("&kind=1"); !errors.Is(err, ErrAuthTagInvalidConditions) {
		t.Fatalf("leading '&': got %v, want ErrAuthTagInvalidConditions", err)
	}
}

// TestNegative_TrailingAmpersand: "trailing ... `&`".
func TestNegative_TrailingAmpersand(t *testing.T) {
	if err := ValidateConditions("kind=1&"); !errors.Is(err, ErrAuthTagInvalidConditions) {
		t.Fatalf("trailing '&': got %v, want ErrAuthTagInvalidConditions", err)
	}
}

// TestNegative_DoubledAmpersand: "doubled `&`".
func TestNegative_DoubledAmpersand(t *testing.T) {
	if err := ValidateConditions("kind=1&&kind=2"); !errors.Is(err, ErrAuthTagInvalidConditions) {
		t.Fatalf("doubled '&': got %v, want ErrAuthTagInvalidConditions", err)
	}
}

// TestNegative_NonCanonicalDecimal: "non-canonical decimal" (leading zero).
func TestNegative_NonCanonicalDecimal(t *testing.T) {
	if err := ValidateConditions("kind=01"); !errors.Is(err, ErrAuthTagInvalidConditions) {
		t.Fatalf("non-canonical decimal: got %v, want ErrAuthTagInvalidConditions", err)
	}
}

// TestNegative_OutOfRangeKind: "out-of-range `kind`" (> 65535, NIP-01's
// 16-bit kind field).
func TestNegative_OutOfRangeKind(t *testing.T) {
	if err := ValidateConditions("kind=65536"); !errors.Is(err, ErrAuthTagInvalidConditions) {
		t.Fatalf("out-of-range kind: got %v, want ErrAuthTagInvalidConditions", err)
	}
}

// TestNegative_ReorderedConditionsString: "reordered conditions string".
// Signing over one clause order and then validating against a reordering
// of the *same clauses* must fail -- proving the implementation never
// canonicalizes/reorders before hashing, since if it did, this would
// wrongly succeed.
func TestNegative_ReorderedConditionsString(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	signed := "kind=1&created_at<1700000000"
	reordered := "created_at<1700000000&kind=1"

	tag, err := SignAuthTag(testOwnerSK, agentPK, signed)
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}

	tampered := append([]string(nil), tag...)
	tampered[2] = reordered

	if err := ValidateAuthTag(tampered, agentPK); !errors.Is(err, ErrAuthTagInvalidSignature) {
		t.Fatalf("reordered conditions string: got %v, want ErrAuthTagInvalidSignature", err)
	}
}

// --- Additional ValidateConditions coverage (not in the PRD's negative-case
// enumeration, but part of NIP-OA's grammar and worth covering directly). --

func TestValidateConditions_ValidCases(t *testing.T) {
	for _, c := range []string{
		"",
		"kind=1",
		"kind=0",
		"created_at<1700000000",
		"created_at>0",
		"kind=1&created_at<1700000000&created_at>1600000000",
	} {
		if err := ValidateConditions(c); err != nil {
			t.Errorf("ValidateConditions(%q): unexpected error %v", c, err)
		}
	}
}

func TestValidateConditions_UnrecognizedClause(t *testing.T) {
	if err := ValidateConditions("bogus=1"); !errors.Is(err, ErrAuthTagInvalidConditions) {
		t.Fatalf("unrecognized clause: got %v, want ErrAuthTagInvalidConditions", err)
	}
}

func TestValidateConditions_EmptyClause(t *testing.T) {
	// Not directly "doubled &" (that's a dedicated PRD case above) but the
	// same underlying rule catches it: a run that produces an empty clause.
	if err := ValidateConditions("kind=1&&"); !errors.Is(err, ErrAuthTagInvalidConditions) {
		t.Fatalf("empty clause via trailing run: got %v, want ErrAuthTagInvalidConditions", err)
	}
}

// --- ValidateAuthTag: additional malformed-tag coverage --------------------

func TestValidateAuthTag_WrongTagName(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	tag := []string{"not-auth", testOwnerSK.Public().Hex(), "kind=1", "00"}
	if err := ValidateAuthTag(tag, agentPK); !errors.Is(err, ErrAuthTagWrongName) {
		t.Fatalf("wrong tag name: got %v, want ErrAuthTagWrongName", err)
	}
}

func TestValidateAuthTag_TooFewElements(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	tag := []string{"auth", testOwnerSK.Public().Hex(), "kind=1"}
	if err := ValidateAuthTag(tag, agentPK); !errors.Is(err, ErrAuthTagElementCount) {
		t.Fatalf("three-element tag: got %v, want ErrAuthTagElementCount", err)
	}
}

func TestValidateAuthTag_MalformedOwnerPubkey(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	tag := []string{"auth", "not-hex", "kind=1", "00"}
	if err := ValidateAuthTag(tag, agentPK); !errors.Is(err, ErrAuthTagInvalidPubkey) {
		t.Fatalf("malformed owner pubkey: got %v, want ErrAuthTagInvalidPubkey", err)
	}
}

func TestValidateAuthTag_MalformedSignatureHex(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	tag := []string{"auth", testOwnerSK.Public().Hex(), "kind=1", "not-hex"}
	if err := ValidateAuthTag(tag, agentPK); !errors.Is(err, ErrAuthTagInvalidSignature) {
		t.Fatalf("malformed signature hex: got %v, want ErrAuthTagInvalidSignature", err)
	}
}

// --- StaticAuthTagFunc (E3 wiring helper) -----------------------------------

func TestStaticAuthTagFunc_ReturnsValidatedTagUnchanged(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	tag, err := SignAuthTag(testOwnerSK, agentPK, "kind=9")
	if err != nil {
		t.Fatalf("SignAuthTag: %v", err)
	}

	fn, err := StaticAuthTagFunc(tag, agentPK)
	if err != nil {
		t.Fatalf("StaticAuthTagFunc: %v", err)
	}

	got, err := fn(context.Background())
	if err != nil {
		t.Fatalf("fn: %v", err)
	}
	if len(got) != 4 || got[2] != "kind=9" {
		t.Fatalf("fn() = %v, want the signed tag unchanged", got)
	}
}

func TestStaticAuthTagFunc_RejectsInvalidTagAtConstruction(t *testing.T) {
	agentPK := testAgentSK.Public().Hex()
	badTag := []string{"auth", testOwnerSK.Public().Hex(), "kind=1 ", "00"} // whitespace
	if _, err := StaticAuthTagFunc(badTag, agentPK); err == nil {
		t.Fatal("expected StaticAuthTagFunc to reject an invalid tag at construction, not defer failure to first use")
	}
}
