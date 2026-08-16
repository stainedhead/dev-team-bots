# Plan: ACP/Native Shared State and Task-Layer Parity

**Created:** 2026-08-16
**Status:** Planning

## Development Approach

TDD (red-green-refactor) throughout, per `AGENTS.md`. Work through the four FR groups roughly in PRD order, since FR-501 (shared-state config) is a dependency for FR-502/503, and FR-504a (board item on every task) is naturally implemented alongside FR-504 (scheduling) since both touch the same `turn.go` call site.

## Phase Breakdown

1. **FR-501/FR-502 — shared-state config mechanism.** Add explicit config field, startup validation, apply to board path resolution (reconciling the two independent formulas) and prepare for chat.json.
2. **FR-503 — ChatStore wiring + history replay in ACP mode.** Construct ChatStore in `acp.go`, wire append + replay in `turn.go`.
3. **FR-504/FR-504a — scheduling detection + DirectTask/board-item creation.** Resolve integration shape per research question 3, wire `ChatTaskManager` pre-check and DirectTask creation into `turn.go`.
4. **FR-505 — heap watchdog wiring.** Construct watchdog from persona config in `acp.go`.

## Critical Path

FR-501 blocks FR-502/FR-503 (need a validated shared root before pointing ChatStore at it). FR-504's integration-shape research question is the highest-risk item and should be resolved concretely before writing FR-504 code — do not start implementation on assumption.

## Testing Strategy

Unit tests at each new wiring seam (config validation, ChatStore construction/failure, scheduling pre-check, DirectTask creation, watchdog activation) — mirroring the test patterns already established in `acp_test.go`/`turn_test.go` from the prior ACP-parity feature. Concurrency-safety tests for any new shared-state writer, mirroring `board_concurrency_test.go`/`board_race_test.go`.

## Rollout Strategy

Single PR via standard dev-flow (branch `feat/acp-native-shared-state`), automerge per repo `AGENTS.md`. No feature flag — additive config field, backward compatible for personas not opting into shared state.

## Success Metrics

All acceptance criteria in `spec.md` pass; coverage ≥90% aggregate on domain+application (no regression from current 92.2%); existing ACP turn-handling/fallback-publish/keep-alive tests unchanged.
