# PRD: Channel-Agnostic Agent Tool Parity — Code and Design Review Findings

**Created:** 2026-08-17
**Source branch:** `feat/channel-agnostic-tool-parity` (vs `origin/main`)
**Source spec:** `specs/260817-channel-agnostic-tool-parity/`
**Status:** Draft

## Executive Summary

**Overall assessment: Approve.** FR-601 (`list_board_items`), FR-602 (`list_my_tasks`), and FR-604 (audit) are complete, wired at the identical construction site/gate `complete_board_item` already used in both native and ACP mode, and reuse existing store instances (no stale-cache duplication, matching the precedent already established for the shared `BoardStore` instance). TDD confirmed genuinely red (compile failure) before implementation, green after. No P0/P1/P2 findings — this is a small, tightly-scoped, well-tested change.

## Findings

None. No Must Fix, Warning, or Info-level findings survived review.

## Explicitly Deferred, Not a Defect

**FR-603 (team-registry-read tool)** was not implemented in this pass, per the PRD's own explicit prioritization ("must-have: FR-601/602/604; stretch, cut first if time is short: FR-603") given the demo deadline. This is a scope decision documented in `spec.md`/`tasks.md`, not an oversight — recorded here so a reviewer doesn't mistake its absence for an incomplete implementation of what was actually committed to.

## Non-Findings Worth Recording

Reviewed and confirmed correct, no action needed:

- `listBoardItems`/`listMyTasks` (`client.go`) both nil-check their required store before doing anything else, matching `completeBoardItem`'s exact defensive pattern.
- `boardItemSummary`/`directTaskSummary`'s `json.Marshal` error paths are defensive, not reachable in practice (no channels/funcs/cycles in these simple structs) — consistent with existing codebase style elsewhere (e.g. `writeItemsAtomically`'s equally-defensive marshal error handling).
- ACP mode's `taskStore` is threaded through `buildACPWorker`/`buildACPMCPOptions` as a parameter rather than constructed a second time inside `buildACPMCPOptions` — confirmed this avoids the exact stale-in-memory-view hazard `ADR-B031` already documented for the shared `BoardStore` instance.
- FR-604's audit claim (no tool construction branches on `task.Source`) was verified by direct grep across `execute_task.go` and `client.go`, not asserted from memory — the only source-conditioned behavior anywhere is chat-provider selection (`execute_task.go:118`), an intentional, already-documented behavior unrelated to tool availability.

## Priorities

No P0/P1/P2 findings. Nothing blocks Step 11 (Final Quality Pass) or the PR.

## Guidance for Implementing Fixes (Step 9)

No fixes required — Step 9 (Implement Review Fixes) can be a no-op confirmation, not a fix cycle, given this review found nothing to fix.
