package domain

import "context"

type MemoryStore interface {
	Write(ctx context.Context, key string, value []byte) error
	Read(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

type VectorStore interface {
	Upsert(ctx context.Context, key string, vector []float32, metadata map[string]string) error
	Search(ctx context.Context, query []float32, limit int) ([]VectorResult, error)
}

type VectorResult struct {
	Key      string
	Score    float32
	Metadata map[string]string
}

// Embedder converts text to a vector for storage and search.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}
