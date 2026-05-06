package vector_test

import (
	"context"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/vector"
)

// BenchmarkSearch100k measures Search throughput over 100,000 vectors of 512 dimensions.
// Run with: go test -bench=BenchmarkSearch100k -benchtime=5s ./internal/infrastructure/local/vector/
func BenchmarkSearch100k(b *testing.B) {
	const n = 100_000
	const dim = 512

	dir := b.TempDir()
	vs, err := vector.New(dir)
	if err != nil {
		b.Fatalf("New: %v", err)
	}

	// Build vectors.
	vecs := make([][]float32, n)
	for i := range n {
		v := make([]float32, dim)
		for j := range dim {
			v[j] = float32((i+j)%1000) * 0.001
		}
		vecs[i] = v
	}

	if err := vs.BulkCache(vecs, nil); err != nil {
		b.Fatalf("BulkCache: %v", err)
	}

	query := make([]float32, dim)
	for i := range dim {
		query[i] = float32(i%1000) * 0.001
	}

	ctx := context.Background()
	b.ResetTimer()
	for range b.N {
		results, err := vs.Search(ctx, query, 10)
		if err != nil {
			b.Fatalf("Search: %v", err)
		}
		if len(results) != 10 {
			b.Fatalf("expected 10 results, got %d", len(results))
		}
	}
}
