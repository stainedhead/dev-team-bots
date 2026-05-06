# Research: Remove AWS Infrastructure — Local Single-Binary Runtime

**Feature:** remove-aws-infra
**Created:** 2026-05-06
**Source PRD:** specs/260506-remove-aws-infra/remove-aws-infra-PRD.md

---

## Research Questions

**RQ-001 — anthropic-sdk-go API surface**
What is the current stable API for `github.com/anthropics/anthropic-sdk-go`? Specifically: how are streaming message requests constructed, what types represent `StopReason` and `TokenUsage`, and does the SDK expose an embeddings endpoint? Verify against the SDK README and any available examples.

**RQ-002 — go-git push/pull API**
What is the correct go-git v5 call sequence for: (a) staging all changes (`git add -A`), (b) committing with a custom author, (c) pushing with PAT auth over HTTPS, and (d) pulling with rebase before push on conflict? Identify any known limitations vs the `git` CLI binary.

**RQ-003 — BM25 in Go**
Is there an existing, well-maintained Go BM25 library, or should `BM25Embedder` be implemented from scratch? Evaluate: `github.com/blugelabs/bluge`, `github.com/blevesearch/bleve`, or a minimal hand-rolled implementation. Determine what `[]float32` output format is appropriate for BM25 sparse vectors.

**RQ-004 — INI credentials file parsing**
What Go libraries exist for INI-format parsing with named profile support (matching AWS CLI conventions)? Evaluate `github.com/go-ini/ini` vs writing a minimal parser. Confirm that the chosen approach can enforce mode-0600 file checks cross-platform (macOS + Linux).

**RQ-005 — Cosine similarity performance at 100k vectors**
At what vector dimension count does brute-force cosine similarity over 100k `[]float32` vectors exceed 100ms on typical laptop hardware? What is the expected embedding dimension for OpenAI `text-embedding-3-small` (1536) and for BM25 sparse vectors? Confirm the NFR is achievable without an HNSW index.

---

## Industry Standards

[TBD]

## Existing Implementations

[TBD — review existing `infrastructure/aws/bedrock` and `infrastructure/aws/sqs` implementations as the pattern reference for new adapters]

## API Documentation

[TBD — anthropic-sdk-go, go-git/go-git/v5]

## Best Practices

[TBD]

## Open Questions

[TBD — populate after research phase]

## References

[TBD]
