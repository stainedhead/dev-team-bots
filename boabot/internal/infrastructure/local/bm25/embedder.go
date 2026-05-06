// Package bm25 provides a local, stdlib-only implementation of domain.Embedder.
// It uses BM25-style sparse embedding with the feature-hashing trick to convert
// text into a fixed-length float32 vector suitable for cosine-similarity search.
//
// # Algorithm
//
//  1. Tokenize input: lowercase, split on any rune that is neither a letter nor
//     a digit (unicode-aware). Deduplicate tokens.
//  2. For each unique token, compute its FNV-1a hash modulo dims → index.
//  3. Add weight 1.0 / sqrt(unique_token_count) to output[index] (TF-style
//     normalization; hash collisions accumulate additively).
//  4. L2-normalize the output vector so it has unit magnitude.
//
// Empty or whitespace-only input produces a zero vector (all 0.0).
// The Embed method always returns nil error — it performs no external calls.
package bm25

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

const defaultDims = 512

// Embedder implements domain.Embedder using BM25-style feature hashing.
type Embedder struct {
	dims int
}

// NewEmbedder constructs an Embedder with the given output dimension.
func NewEmbedder(dims int) *Embedder {
	return &Embedder{dims: dims}
}

// DefaultEmbedder returns an Embedder with 512 dimensions.
func DefaultEmbedder() *Embedder {
	return NewEmbedder(defaultDims)
}

// Embed converts text to a float32 vector of length e.dims.
//
// The returned vector is L2-normalized unless the input produced no tokens,
// in which case a zero vector is returned. The context argument is accepted
// for interface compatibility but is not used (no external calls are made).
func (e *Embedder) Embed(_ context.Context, text string) ([]float32, error) {
	out := make([]float32, e.dims)

	tokens := tokenize(text)
	if len(tokens) == 0 {
		return out, nil
	}

	// Deduplicate tokens to get unique set.
	unique := dedup(tokens)
	n := len(unique)

	weight := float32(1.0 / math.Sqrt(float64(n)))

	for _, tok := range unique {
		idx := fnv1aIndex(tok, e.dims)
		out[idx] += weight
	}

	// L2-normalize.
	var sumSq float64
	for _, x := range out {
		sumSq += float64(x) * float64(x)
	}
	if sumSq > 0 {
		mag := float32(math.Sqrt(sumSq))
		for i := range out {
			out[i] /= mag
		}
	}

	return out, nil
}

// tokenize lowercases text and splits it on any non-letter, non-digit rune.
// Empty segments are discarded.
func tokenize(text string) []string {
	lower := strings.ToLower(text)
	return strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// dedup returns the unique elements of tokens in stable order.
func dedup(tokens []string) []string {
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}

// fnv1aIndex computes the FNV-1a hash of s and returns it modulo dims.
func fnv1aIndex(s string, dims int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32()) % dims
}
