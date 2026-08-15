# Research: ACP Harness Feature Parity — Code Review Fixes

**Created:** 2026-08-15
**Source PRD:** [acp-harness-feature-parity-auto-review-PRD.md](./acp-harness-feature-parity-auto-review-PRD.md)

## Research Questions

1. ~~Fix mechanism~~ — **Resolved.** `board.go`'s `InMemoryBoardStore` (`items map[string]domain.WorkItem` + `sync.RWMutex`, in-process only) needs both a cross-process lock AND a re-read: `loadFromDisk()` (lines 56-68) only runs once at construction, so even with a lock, a long-lived process's in-memory state goes stale relative to disk. `lock.go`'s FR-031 pattern (atomic temp-file+`os.Link` publish, PID-liveness staleness check, lines 60-294) is stdlib-only and portable (macOS/Linux/Windows) — deliberately not `syscall.Flock` (Unix-only, and this codebase has no `golang.org/x/sys` dependency). But `lock.go`'s *fail-fast* semantic (`AcquireLock` returns `ErrAlreadyRunning` immediately if held) is wrong for board.go, which needs many legitimate concurrent writers to wait, not fail. **Decision:** extract `lock.go`'s atomic-publish/stale-check primitive into a small reusable helper, wrap it in a short retry-with-backoff loop (acquire-or-wait, not acquire-or-fail), and acquire it around each `persist()` using a sibling path (`persistPath + ".lock"`). Combine with re-reading `persistPath` from disk immediately before each write (not just serializing in-memory `s.items`).
2. ~~Merge semantics~~ — **Resolved.** `domain.WorkItem` (`internal/domain/orchestrator.go:61-83`) is keyed by `ID`, with `SortPosition`/`Status` as per-item scalar fields — no separate ordering slice, so a naive union-by-ID merge is safe for this fix's scope. **Decision:** before persisting, re-read disk, union `(diskItems ∪ myItems)` by ID, my process's own touched item(s) win (last-write-wins-per-item, not per-file), keep any disk-only item untouched. **Known, documented limitation, not solved here:** `Reorder` sets `SortPosition` for a whole `ids` slice in one call — two processes concurrently reordering the same column could still produce colliding/gapped positions under a naive per-item merge. Out of scope per RQ2's original framing (stop file-level clobbering, not full concurrent-edit-of-the-same-item semantics) — document as a known limitation, don't attempt to solve.
3. FR-2: confirm the exact current wording of the "mirrors team_manager.go's exact condition" claim in each of the 5 locations named (spec.md, architecture.md, research.md, ADR-B030, adoption doc) before rewriting, to make precise, targeted edits rather than guessing at current phrasing.

## Industry Standards

- File locking for concurrent-process JSON persistence: OS advisory locks (`flock` on Unix) are the standard mechanism; Go's `os` package doesn't wrap this directly but `syscall.Flock` (Unix) or a small vendored/stdlib-adjacent helper is typically used. Check whether this codebase already imports any locking utility (per RQ1, `buzz/lock.go` is a likely precedent) before introducing a new one.

## Existing Implementations

- `internal/infrastructure/buzz/lock.go` (FR-031, ADR-B024) — existing process-singleton lock for Buzz private keys, the codebase's own precedent for "prevent a second process from causing a conflict," cited directly by the review as the relevant sibling solution.
- `internal/infrastructure/local/orchestrator/board.go`'s `InMemoryBoardStore` — the store being fixed; `loadFromDisk()`/`persist()` are the two methods in scope.

## API Documentation

[TBD — no external APIs; internal Go code and file I/O only.]

## Best Practices

[TBD]

## Open Questions

None remaining on the fix mechanism — RQ1/RQ2 resolved concretely above. RQ3 (test template) also resolved: `internal/infrastructure/buzz/lock_race_test.go`'s `TestAcquireLock_ConcurrentRace_OnlyOneWinner` (goroutines racing a shared path, deterministic test-only hook to force the TOCTOU window rather than relying on scheduler luck) is the template for the new cross-process board test — two `InMemoryBoardStore` instances sharing one `persistPath`, concurrent `Create`/`Update` of distinct items, assert the final on-disk file contains both.

## References

- Source PRD: [acp-harness-feature-parity-auto-review-PRD.md](./acp-harness-feature-parity-auto-review-PRD.md)
- Original feature spec (archived): `specs/archive/260815-acp-harness-feature-parity/`
- Sibling precedent: `docs/architectural-decision-record.md` ADR-B024 (Buzz process-singleton lock)
