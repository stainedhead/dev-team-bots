package domain

import (
	"context"
	"time"
)

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

// MemoryBackup backs up and restores the agent's memory directory to/from a
// remote store (e.g. a GitHub repository).
type MemoryBackup interface {
	Backup(ctx context.Context) error
	Restore(ctx context.Context) error
	Status(ctx context.Context) (BackupStatus, error)
}

// BackupStatus describes the current state of the memory backup.
type BackupStatus struct {
	LastBackupAt   time.Time
	PendingChanges int
	RemoteURL      string
}
