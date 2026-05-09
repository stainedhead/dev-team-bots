# Implementation Notes — remove-aws-infra Auto Code Review Fixes

**Feature:** remove-aws-infra-auto-review
**Created:** 2026-05-06

## Technical Decisions

**KnownFields(true) on decoder:** Applied to the top-level decoder in `config.Load()`. Recursive — covers all nested structs. No change to callers needed.

**Backup wiring order:** Restore must run before `fs.New()`, `vector.New()`, and `budget.New()` so restored files (including `budget.json`) are visible to adapter constructors on first load.

**Embedder warning instead of error:** FR-R003 uses `slog.Warn` + BM25 fallback rather than a startup error. The OpenAI embedder type is a valid future configuration; failing on it would break early adopters who configure it anticipating the implementation.

**Test URL for restore failure:** `https://localhost:1/nonexistent/repo.git` gives connection refused immediately, making `TestStartBot_RestoreOnEmptyFails` fast and deterministic without mocking.

## Deviations from Plan

None — all four fixes implemented exactly as specified in the PRD.

## Lessons Learned

- `KnownFields(true)` is a one-liner that satisfies a migration safety guarantee; should be default for any config loader.
- Wiring gap (backup implemented but not started) was caught by spec alignment check in the review, not by tests — shows the value of spec-driven review.
