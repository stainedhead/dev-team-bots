package buzz

import (
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
)

// NewDMKeyer returns a nostr.Keyer over sk, satisfying the Signer+Cipher
// contract every nip17 DM function requires (nostr.Keyer's doc comment:
// sign events, and NIP-44 encrypt/decrypt for a recipient/sender).
//
// It wraps the vendored library's own in-memory KeySigner
// (fiatjaf.com/nostr/keyer.NewPlainKeySigner) rather than reimplementing
// NIP-44 encryption or Schnorr signing here -- the security-sensitive
// crypto stays entirely inside the vendored, audited library (research.md
// RQ2's resolution: "the library already does the correct thing").
//
// sk is the exact nostr.SecretKey LoadKeypair already resolves per
// persona for channel participation (FR-201: "using the same relay
// connection and identity") -- this is not a new secret type, just a
// different in-memory wrapper around the same key material already held
// by the calling code (cmd/boabot/main.go's buildBuzzMonitor).
func NewDMKeyer(sk nostr.SecretKey) nostr.Keyer {
	return keyer.NewPlainKeySigner(sk)
}
