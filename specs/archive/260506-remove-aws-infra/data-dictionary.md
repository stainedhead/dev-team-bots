# Data Dictionary: Remove AWS Infrastructure — Local Single-Binary Runtime

**Feature:** remove-aws-infra
**Created:** 2026-05-06

---

## Purpose

Records all new and modified data structures, interfaces, and types introduced by this feature.

---

## Domain Interfaces (New)

### `MemoryBackup`
```go
type MemoryBackup interface {
    Backup(ctx context.Context) error
    Restore(ctx context.Context) error
    Status(ctx context.Context) (BackupStatus, error)
}
```

### `BackupStatus`
```go
type BackupStatus struct {
    LastBackupAt   time.Time
    PendingChanges int
    RemoteURL      string
}
```

---

## Domain Interfaces (Existing — implemented by new local adapters)

[TBD — record exact signatures after reading internal/domain/ source]

- `MessageQueue` — implemented by `infrastructure/local/queue`
- `Broadcaster` — implemented by `infrastructure/local/bus`
- `MemoryStore` — implemented by `infrastructure/local/fs`
- `VectorStore` — implemented by `infrastructure/local/vector`
- `Embedder` — implemented by BM25Embedder + provider-backed embedder
- `BudgetTracker` — implemented by `infrastructure/local/budget`
- `ModelProvider` — implemented by `infrastructure/anthropic`

---

## Config Structs (New / Modified)

### `MemoryConfig` (new)
```go
type MemoryConfig struct {
    Path        string  // default: <binary-dir>/memory
    VectorIndex string  // "cosine" | "hnsw"
    Embedder    string  // "bm25" | provider name
    HeapWarnMB  int     // 0 = disabled
    HeapHardMB  int     // 0 = disabled
}
```

### `BackupConfig` (new)
```go
type BackupConfig struct {
    Enabled        bool
    Schedule       string  // cron expression
    RestoreOnEmpty bool
    GitHub         GitHubBackupConfig
}

type GitHubBackupConfig struct {
    Repo        string
    Branch      string
    AuthorName  string
    AuthorEmail string
    // token read from env var BOABOT_BACKUP_TOKEN or credentials file
}
```

### `ProviderConfig` (modified — new type)
```go
// existing type gains new valid value for Type field:
// Type: "anthropic" | "bedrock" | "openai"
```

---

## Value Objects

### Vector entry (on-disk format)
[TBD — determine binary encoding: raw float32 LE, or length-prefixed?]

### Budget state (JSON)
```json
{
  "token_input": 0,
  "token_output": 0,
  "api_calls": 0,
  "last_updated": "2026-05-06T00:00:00Z"
}
```

---

## Enumerations

### Provider types
| Value | Description |
|---|---|
| `anthropic` | Direct Anthropic Claude SDK |
| `bedrock` | AWS Bedrock (existing) |
| `openai` | OpenAI-compatible HTTP endpoint (existing) |

### Vector index types
| Value | Description |
|---|---|
| `cosine` | Brute-force cosine similarity (default) |
| `hnsw` | HNSW approximate nearest-neighbour (future) |

### Embedder types
| Value | Description |
|---|---|
| `bm25` | BM25 keyword sparse vectors (default) |
| `<provider-name>` | Semantic embeddings via named provider |

---

## API Request / Response Types (Anthropic adapter)

[TBD — record after reviewing anthropic-sdk-go and mapping to existing InvokeRequest/InvokeResponse domain types]
