# Research: BaoBot ACP Stdio Harness Support — Auto-Review Fixes

**Created:** 2026-08-13
**Source PRD:** `boabot-acp-stdio-harness-support-auto-review-PRD.md` (this directory)

## Research Questions

1. FR-001: which of the three fix approaches (per-turn `Worker`, mutex-serialized turns, or session-scoped progress routing) best preserves `buzz-acp`'s expected concurrent-session behavior, and is `Worker` construction (via `application.NewExecuteTaskUseCase`) cheap enough to repeat per turn if option (a) is chosen?
2. FR-004: does wiring `WithRulesTracker` in `cmd/boabot/acp.go` surface any other native-mode construction steps ACP mode is missing, beyond what the original review found?

## Existing Implementations

- `internal/application/team/team_manager.go:836-837` — the exact `WithRulesTracker` wiring pattern FR-004 should mirror.
- `internal/application/execute_task.go:52-54` — `WithProgressHandler`'s current unsynchronized implementation, root cause of FR-001.

## API Documentation

N/A — no new external APIs involved in this fix cycle.

## Best Practices

Go's standard `sync.Mutex`/per-call-scoped state patterns apply directly to FR-001 and FR-005; no new library needed.

## Open Questions

- See research question 1 above — carried from the review PRD's own Open Questions section, this is the one genuinely undetermined design choice in this fix cycle.

## References

- `specs/archive/260813-boabot-acp-stdio-harness-support/` — the original feature's full spec/architecture/implementation-notes.
