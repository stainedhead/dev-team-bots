package bm25_test

import (
	"context"
	"math"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/bm25"
)

// l2Norm computes the L2 norm of a float32 slice.
func l2Norm(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}

// cosineSimilarity computes the cosine similarity of two float32 slices.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		panic("length mismatch")
	}
	var dot, magA, magB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		magA += float64(a[i]) * float64(a[i])
		magB += float64(b[i]) * float64(b[i])
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

// TestDefaultEmbedder_Dims verifies DefaultEmbedder returns 512-dim vectors.
func TestDefaultEmbedder_Dims(t *testing.T) {
	t.Parallel()
	e := bm25.DefaultEmbedder()
	vec, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 512 {
		t.Errorf("expected 512 dims, got %d", len(vec))
	}
}

// TestNewEmbedder_CustomDims verifies NewEmbedder respects the dims parameter.
func TestNewEmbedder_CustomDims(t *testing.T) {
	t.Parallel()
	e := bm25.NewEmbedder(256)
	vec, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 256 {
		t.Errorf("expected 256 dims, got %d", len(vec))
	}
}

// TestEmbed_EmptyInput returns a zero vector of correct length.
func TestEmbed_EmptyInput(t *testing.T) {
	t.Parallel()
	e := bm25.DefaultEmbedder()
	vec, err := e.Embed(context.Background(), "")
	if err != nil {
		t.Fatalf("Embed empty: %v", err)
	}
	if len(vec) != 512 {
		t.Errorf("expected 512 dims for empty input, got %d", len(vec))
	}
	for i, x := range vec {
		if x != 0 {
			t.Errorf("expected zero vector for empty input, got vec[%d]=%f", i, x)
			break
		}
	}
}

// TestEmbed_WhitespaceOnlyInput returns a zero vector (no real tokens).
func TestEmbed_WhitespaceOnlyInput(t *testing.T) {
	t.Parallel()
	e := bm25.DefaultEmbedder()
	vec, err := e.Embed(context.Background(), "   \t\n  ")
	if err != nil {
		t.Fatalf("Embed whitespace: %v", err)
	}
	norm := l2Norm(vec)
	if norm > 1e-6 {
		t.Errorf("expected zero vector for whitespace-only input, norm=%f", norm)
	}
}

// TestEmbed_L2NormIsOne verifies the output vector is L2-normalized.
func TestEmbed_L2NormIsOne(t *testing.T) {
	t.Parallel()
	e := bm25.DefaultEmbedder()
	texts := []string{
		"hello world",
		"the quick brown fox",
		"golang is a compiled language",
		"vector embeddings are useful",
	}
	for _, text := range texts {
		vec, err := e.Embed(context.Background(), text)
		if err != nil {
			t.Fatalf("Embed %q: %v", text, err)
		}
		norm := l2Norm(vec)
		if math.Abs(norm-1.0) > 0.001 {
			t.Errorf("Embed(%q): expected L2 norm ~1.0, got %f", text, norm)
		}
	}
}

// TestEmbed_NoError verifies Embed never returns an error (no external calls).
func TestEmbed_NoError(t *testing.T) {
	t.Parallel()
	e := bm25.DefaultEmbedder()
	inputs := []string{"", "hello", "hello world", "a b c d e f g"}
	for _, in := range inputs {
		if _, err := e.Embed(context.Background(), in); err != nil {
			t.Errorf("Embed(%q) returned unexpected error: %v", in, err)
		}
	}
}

// TestEmbed_SimilarTextHigherSimilarity verifies semantic ordering.
// "dog cat" vs "cat dog" should have higher similarity than "dog cat" vs "airplane rocket".
func TestEmbed_SimilarTextHigherSimilarity(t *testing.T) {
	t.Parallel()
	e := bm25.DefaultEmbedder()

	ctx := context.Background()
	vA, _ := e.Embed(ctx, "dog cat")
	vB, _ := e.Embed(ctx, "cat dog")         // same tokens, different order
	vC, _ := e.Embed(ctx, "airplane rocket") // completely different tokens

	simAB := cosineSimilarity(vA, vB)
	simAC := cosineSimilarity(vA, vC)

	if simAB <= simAC {
		t.Errorf("expected sim('dog cat','cat dog')=%f > sim('dog cat','airplane rocket')=%f",
			simAB, simAC)
	}
}

// TestEmbed_AnagramTokensIdenticalVector verifies that token-order-independent hashing
// means "dog cat" and "cat dog" produce identical vectors.
func TestEmbed_AnagramTokensIdenticalVector(t *testing.T) {
	t.Parallel()
	e := bm25.DefaultEmbedder()
	ctx := context.Background()

	v1, _ := e.Embed(ctx, "dog cat")
	v2, _ := e.Embed(ctx, "cat dog")

	sim := cosineSimilarity(v1, v2)
	if math.Abs(sim-1.0) > 0.001 {
		t.Errorf("expected identical vectors for same tokens in different order, got cosine=%f", sim)
	}
}

// TestEmbed_Deterministic verifies the same text always produces the same vector.
func TestEmbed_Deterministic(t *testing.T) {
	t.Parallel()
	e := bm25.DefaultEmbedder()
	ctx := context.Background()

	text := "the quick brown fox jumps over the lazy dog"
	v1, _ := e.Embed(ctx, text)
	v2, _ := e.Embed(ctx, text)

	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("non-deterministic: v1[%d]=%f v2[%d]=%f", i, v1[i], i, v2[i])
		}
	}
}

// TestEmbed_PunctuationSplitting verifies that punctuation is treated as a separator.
func TestEmbed_PunctuationSplitting(t *testing.T) {
	t.Parallel()
	e := bm25.DefaultEmbedder()
	ctx := context.Background()

	// "hello,world" should produce same result as "hello world" (comma is a separator).
	v1, _ := e.Embed(ctx, "hello,world")
	v2, _ := e.Embed(ctx, "hello world")

	sim := cosineSimilarity(v1, v2)
	if math.Abs(sim-1.0) > 0.001 {
		t.Errorf("expected punctuation to be treated as separator, cosine sim=%f", sim)
	}
}

// TestEmbed_CaseNormalization verifies that "Hello" and "hello" map to the same token.
func TestEmbed_CaseNormalization(t *testing.T) {
	t.Parallel()
	e := bm25.DefaultEmbedder()
	ctx := context.Background()

	v1, _ := e.Embed(ctx, "Hello World")
	v2, _ := e.Embed(ctx, "hello world")

	sim := cosineSimilarity(v1, v2)
	if math.Abs(sim-1.0) > 0.001 {
		t.Errorf("expected case-normalized vectors to be identical, cosine sim=%f", sim)
	}
}

// TestEmbed_ContextNotUsed verifies a cancelled context does not affect Embed.
func TestEmbed_ContextNotUsed(t *testing.T) {
	t.Parallel()
	e := bm25.DefaultEmbedder()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	vec, err := e.Embed(ctx, "hello world")
	if err != nil {
		t.Fatalf("Embed with cancelled ctx: %v", err)
	}
	if len(vec) != 512 {
		t.Errorf("expected 512 dims, got %d", len(vec))
	}
}
