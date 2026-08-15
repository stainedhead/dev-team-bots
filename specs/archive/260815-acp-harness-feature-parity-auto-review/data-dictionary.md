# Data Dictionary: ACP Harness Feature Parity — Code Review Fixes

**Created:** 2026-08-15

No new domain types. One new small infrastructure-layer helper:

## Interfaces

- New file-lock helper (name TBD at implementation, e.g. `filelock.Acquire(path string, opts...) (release func(), err error)`) — extracted from `internal/infrastructure/buzz/lock.go`'s existing atomic-publish/stale-check primitive, wrapped with retry-with-backoff. Used by `board.go`'s `persist()`.

## Entities

- `domain.WorkItem` (existing, `internal/domain/orchestrator.go:61-83`) — unchanged; its existing `ID`/`SortPosition`/`Status` fields are what FR-1's merge operates on.
