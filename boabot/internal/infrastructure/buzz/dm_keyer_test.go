package buzz

import (
	"context"
	"testing"

	"fiatjaf.com/nostr"
)

// TestNewDMKeyer_GetPublicKey_MatchesDerivedPubKey verifies the adapter's
// GetPublicKey returns exactly the pubkey the underlying secret key
// derives, i.e. it is backed by the given key material and not some other
// identity (P2.1).
func TestNewDMKeyer_GetPublicKey_MatchesDerivedPubKey(t *testing.T) {
	sk := nostr.Generate()
	kr := NewDMKeyer(sk)

	pk, err := kr.GetPublicKey(context.Background())
	if err != nil {
		t.Fatalf("GetPublicKey: %v", err)
	}
	if pk != sk.Public() {
		t.Fatalf("expected pubkey %s, got %s", sk.Public().Hex(), pk.Hex())
	}
}

// TestNewDMKeyer_SignEvent_ProducesValidSignature verifies the adapter can
// sign an event and the result verifies against the derived pubkey --
// required for nip17.PrepareMessage's seal/gift-wrap signing steps.
func TestNewDMKeyer_SignEvent_ProducesValidSignature(t *testing.T) {
	sk := nostr.Generate()
	kr := NewDMKeyer(sk)

	evt := nostr.Event{Kind: 1, Content: "hello", CreatedAt: nostr.Now()}
	if err := kr.SignEvent(context.Background(), &evt); err != nil {
		t.Fatalf("SignEvent: %v", err)
	}
	if evt.PubKey != sk.Public() {
		t.Fatalf("expected signed event pubkey %s, got %s", sk.Public().Hex(), evt.PubKey.Hex())
	}
	if !evt.VerifySignature() {
		t.Fatal("expected a valid signature")
	}
}

// TestNewDMKeyer_EncryptDecrypt_RoundTrips verifies the NIP-44
// encrypt/decrypt round-trip between two independently-constructed
// adapters, exactly the operation nip17.PrepareMessage/GiftUnwrap perform
// on the sealed rumor and the gift-wrap ciphertext.
func TestNewDMKeyer_EncryptDecrypt_RoundTrips(t *testing.T) {
	skA := nostr.Generate()
	skB := nostr.Generate()
	krA := NewDMKeyer(skA)
	krB := NewDMKeyer(skB)

	ctx := context.Background()
	ciphertext, err := krA.Encrypt(ctx, "a secret message", skB.Public())
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ciphertext == "" || ciphertext == "a secret message" {
		t.Fatalf("expected non-empty, non-plaintext ciphertext, got %q", ciphertext)
	}

	plaintext, err := krB.Decrypt(ctx, ciphertext, skA.Public())
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plaintext != "a secret message" {
		t.Fatalf("expected round-tripped plaintext %q, got %q", "a secret message", plaintext)
	}
}
