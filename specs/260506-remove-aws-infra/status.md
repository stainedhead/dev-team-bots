# Status: Remove AWS Infrastructure — Local Single-Binary Runtime

**Feature:** remove-aws-infra
**Created:** 2026-05-06
**Spec dir:** specs/260506-remove-aws-infra/

---

## Overall Progress

| Phase | Description | Status |
|---|---|---|
| 0 | Spec Creation | In Progress |
| 1 | Research | Not Started |
| 2 | Data Modeling | Not Started |
| 3 | Architecture | Not Started |
| 4 | Implementation — M1: Local Adapters | Complete |
| 5 | Implementation — M2: Anthropic Provider | Complete |
| 6 | Implementation — M3: Vector Store + Embedder | Complete |
| 7 | Implementation — M4: TeamManager + Wiring | Complete |
| 8 | Implementation — M5: GitHub Backup | Not Started |
| 9 | Implementation — M6: Config + Credentials + Watchdog | Not Started |
| 10 | Implementation — M7: Remove AWS + CDK + Docs | Not Started |
| 11 | Tests & Quality | Not Started |

---

## Phase 0 Tasks

- [x] Spec directory created
- [x] PRD moved into spec directory
- [x] spec.md populated from PRD
- [x] All phase files initialized
- [x] Research questions identified and recorded in research.md (5 RQs)
- [x] Edge cases documented in spec.md
- [x] Open questions documented in spec.md (OQ-1, OQ-2, OQ-3)
- [x] Acceptance criteria expanded to cover all 22 FRs
- [ ] architecture.md draft reviewed

---

## Phase 4 Tasks (M1: Local Adapters)

- [x] `internal/infrastructure/local/queue/queue.go` — Router + Queue implementing domain.MessageQueue (coverage: 95.1%)
- [x] `internal/infrastructure/local/bus/bus.go` — Bus implementing domain.Broadcaster (coverage: 96.0%)
- [x] `internal/infrastructure/local/fs/fs.go` — FS implementing domain.MemoryStore (coverage: 93.5%)
- [x] `internal/infrastructure/local/budget/budget.go` — BudgetTracker implementing domain.BudgetTracker (coverage: 91.9%)
- [x] All packages pass `go test -race ./internal/infrastructure/local/...`
- [x] `go fmt`, `go vet`, `golangci-lint` all pass with 0 issues
- [x] No new external dependencies (stdlib only)

---

## Phase 5 Tasks (M2: Anthropic Provider)

- [x] `go get github.com/anthropics/anthropic-sdk-go@v1.40.0` added as direct dependency
- [x] `internal/infrastructure/anthropic/provider.go` — Provider implementing domain.ModelProvider
- [x] `internal/infrastructure/anthropic/mocks/messages_client.go` — injectable MessagesClient mock
- [x] `internal/infrastructure/anthropic/provider_test.go` — 14 unit tests (100% statement coverage on provider.go)
- [x] `internal/infrastructure/anthropic/provider_integration_test.go` — integration test with `//go:build integration` tag
- [x] `NewFromEnv` reads `ANTHROPIC_API_KEY` exclusively; returns clear error if unset
- [x] Rate-limit mapping: HTTP 429 and 503 → `RateLimitError` with `ErrRateLimit` sentinel
- [x] `go fmt`, `go vet`, `golangci-lint` all pass with 0 issues
- [x] Coverage: 100% on implementation package (90.7% total including mocks)

---

## Phase 6 Tasks (M3: Vector Store + Embedder)

- [x] `internal/infrastructure/local/vector/vector.go` — VectorStore implementing domain.VectorStore (coverage: 92.4%)
  - On-disk format: `<key>.vec` (binary LE: [uint32 dim][float32*dim]) + `<key>.meta` (JSON)
  - Atomic writes via temp-file + os.Rename
  - Path-traversal prevention: rejects keys starting with `.` or containing `..` segments
  - In-memory flat-slice cache with pre-computed magnitudes for O(n) search
  - `BulkCache` helper for performance testing without disk I/O
  - Benchmark: 40ms per search over 100k × 512-dim vectors (Apple M5 Max, no race detector)
- [x] `internal/infrastructure/local/bm25/embedder.go` — BM25-style feature-hash Embedder implementing domain.Embedder (coverage: 100%)
  - Fixed output dimension: 512 (configurable via `NewEmbedder(dims int)`)
  - Tokenise: lowercase + split on non-letter/non-digit (unicode-aware)
  - Feature hashing: FNV-1a hash mod dims → index; weight = 1/sqrt(unique_tokens)
  - L2-normalised output; zero vector for empty/whitespace-only input
  - Stdlib only — no external dependencies
- [x] 20 unit tests in vector package; 12 unit tests in bm25 package; all pass with -race
- [x] 1 benchmark: BenchmarkSearch100k — 40.3ms/op at 100k × 512-dim (87 iterations, -benchtime=3s)
- [x] `TestSearch100kVectors` timing assertion: < 100ms (non-race), < 3s (race detector)
- [x] `go fmt`, `go vet`, `golangci-lint` all pass with 0 issues
- [x] No new external dependencies (stdlib only)

---

## Blockers

None.

---

## Recent Activity

- 2026-05-06 — Spec directory created from remove-aws-infra-PRD.md
- 2026-05-06 — M1 complete: four local adapters implemented with TDD, all ≥90% coverage
- 2026-05-06 — M2 complete: Anthropic SDK provider implemented with TDD, 100% coverage on implementation; anthropic-sdk-go v1.40.0 added
- 2026-05-06 — M3 complete: local VectorStore (cosine similarity, 92.4% coverage) + BM25 embedder (feature hashing, 100% coverage); 40ms/search at 100k×512-dim
- 2026-05-06 — M4 complete: TeamManager + BotRegistry + localProviderFactory + main.go rewired to TeamManager; 94.5% coverage on team package; binary builds and starts without AWS
