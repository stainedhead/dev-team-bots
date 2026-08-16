# Implementation Notes: ACP/Native Shared State and Task-Layer Parity

**Created:** 2026-08-16

## Purpose

Running log of technical decisions, edge cases, and deviations discovered during implementation. Update as Step 3/9 progress — this is the durable record `write-flow-analys` and future readers rely on.

## Technical Decisions

- **FR-501 redesigned during implementation.** A verification pass (before writing any FR-501 code) confirmed the PRD's original wording — "fail loudly if native mode's config and an ACP persona's config diverge" — cannot be implemented: an ACP worker process only ever loads its own persona `config.yaml` (`resolveACPConfigPath` in `acp.go`), never native mode's separate top-level config, and native mode may not even be running at the time. There is no cross-process channel to compare against. Implemented instead: a marker file (`sharedstate.EnsureOwner`, new package `internal/infrastructure/local/sharedstate`) inside the shared directory itself, recording the identity that claimed it. This catches identity drift/collision within an already-shared directory (checkable with no cross-process communication) but does NOT catch two processes configured to point at genuinely different roots when they were meant to share — that case is undetectable by construction and is called out explicitly rather than silently claimed as solved. `spec.md`'s FR-501 and AC1 were updated to match.
- **No new config field for the shared-state root.** `memory.path` already exists on both native mode's top-level config and every ACP persona's config, and both already resolve board/chat/task paths from it (native: `MemoryRoot` + orchestrator name; ACP: `cfg.Memory.Path` + `cfg.Bot.Name`, falling back to exe-dir if empty). Adding a second, redundant field (`shared_state.root`) would duplicate this without adding capability — the "make it explicit" goal is served by (a) documenting that operators should set `memory.path` explicitly on any ACP persona meant to share state with native mode, and (b) the marker file catching the failure mode that actually matters (accidental collision/drift), not by inventing a parallel config surface.
- **Pre-existing bug found and fixed as a dependency of this feature, not a new feature.** `ChatStore` and `DirectTaskStore` already existed (used by native mode) but their `persist()` methods had the same unlocked-write clobber bug `board.json` had before its P0 fix — undetected until now because neither store was genuinely shared cross-process before this feature. Fixed by applying the identical `filelock`-based re-read-merge-write pattern `board.go` already established. Regression tests confirmed genuinely red (deterministic across 20 runs) before the fix.

## Edge Cases & Solutions

(populated during implementation)

## Deviations from Plan

(populated during implementation)

## Lessons Learned

(populated during implementation)
