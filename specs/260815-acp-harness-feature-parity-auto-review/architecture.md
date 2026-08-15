# Architecture: ACP Harness Feature Parity — Code Review Fixes

**Created:** 2026-08-15
**Status:** Draft

## Architecture Overview

FR-1's fix touches `internal/infrastructure/local/orchestrator/board.go`, the shared store implementation both native mode and ACP mode use — appropriate since the store itself, not either mode's caller, is where the concurrency guarantee belongs. A new small reusable file-lock helper is extracted from `internal/infrastructure/buzz/lock.go`'s existing atomic-publish primitive. FR-2–FR-5 are documentation-only (except FR-5, optional and deferred).

## Component Architecture

- New file-lock helper (extracted from `lock.go`'s primitive, retry-with-backoff wrapper added) — likely `internal/infrastructure/local/filelock` or similar, importable by `board.go` without a `buzz`→`orchestrator` cross-adapter dependency.
- `InMemoryBoardStore.persist()` modified: acquire lock → re-read disk → merge by item ID → write → release lock.
- `InMemoryBoardStore.loadFromDisk()` unchanged in its construction-time role; the re-read-before-persist logic is new, separate code (reuses the same unmarshal logic, doesn't replace the constructor's initial load).

## Layer Responsibilities

- **Domain:** unchanged — `WorkItem`'s existing `ID`/`SortPosition`/`Status` fields are what the merge operates on, no new fields needed.
- **Infrastructure:** `board.go` gains the lock+re-read+merge logic; new file-lock helper package (infrastructure-layer, no domain/application changes).

## Data Flow

Each mutating call (`Create`/`Update`/`Delete`/`Reorder`) → acquire `persistPath + ".lock"` (retry-with-backoff if held) → re-read `persistPath` from disk into a fresh map → merge (union by ID, caller's own touched item(s) win) → write merged result → release lock. Read-only calls (`Get`/`List`) continue reading from in-memory state without a disk re-read (acceptable staleness window for reads, per the review's scope — the bug is write-clobbering, not read-staleness).

## Sequence Diagrams

[Deferred — the data-flow description above is sufficiently precise for a fix of this size.]

## Integration Points

- `internal/infrastructure/buzz/lock.go` — source of the reused atomic-publish/stale-check primitive, not modified itself.
- `internal/infrastructure/buzz/lock_race_test.go` — template for the new cross-process board test's deterministic-race-forcing pattern.

## Architectural Decisions

- **Reuse `lock.go`'s primitive via extraction, not `syscall.Flock`.** This codebase has no `golang.org/x/sys` dependency and `lock.go`'s own doc comments show deliberate engineering effort to stay stdlib-only for Windows portability — introducing `Flock` (Unix-only) would break that existing constraint for no compensating benefit.
- **Retry-with-backoff, not fail-fast.** `lock.go`'s singleton-lock semantic (fail immediately if held) is correct for its own use case (one process per Buzz identity) but wrong for board.go, which expects many legitimate concurrent writers to each get their turn, not error out.
- **Re-read-and-merge is required in addition to locking, not instead of it.** A lock alone prevents two simultaneous writes from corrupting the file, but does nothing about a long-lived process's in-memory state going stale relative to disk (it loaded once at construction). Both are needed together.
- **`Reorder`'s true concurrent-conflict case is an accepted, documented limitation.** Solving full concurrent-edit-of-the-same-item semantics is out of scope for this fix (per the review's own framing); the merge only needs to stop one process's write from discarding items the other process didn't touch.
