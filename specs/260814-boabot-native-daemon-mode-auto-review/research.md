# Research: Boabot Native Daemon Mode — Code Review Fixes

**Created:** 2026-08-14
**Source PRD:** [boabot-native-daemon-mode-auto-review-PRD.md](./boabot-native-daemon-mode-auto-review-PRD.md)

## Research Questions

1. FR-101: exact current line(s) in `buzz_task_bridge.go`'s `Dispatch`/`markSeenIfDuplicate` — confirm the mark-seen call site relative to the dispatch attempt before writing the fix (the review PRD cites this precisely, but re-confirm against current `HEAD`, since this spec creation happens after Step 7's archive commit).
2. FR-102: confirm `boardItemTitleMaxLen`'s current value and `buzzBoardTitle`'s exact current implementation.
3. FR-103: confirm `TeamManager.Run()`'s current test file location and existing `TestTeamManager_BuzzMonitorBuilder_*` test names, to add the new `nil`-builder test alongside them consistently.
4. FR-106: confirm `boabot.yml`'s exact coverage-check step wording, to phrase the AGENTS.md/README clarification accurately (per the review PRD's recommendation to clarify docs rather than chase coverage).
5. FR-107: confirm `LoadTeamConfig`'s only callers via grep before reverting to unexported, to make sure no test or other file outside `team_manager.go`/its own test depends on the exported name.

## Industry Standards

[TBD — not relevant; these are internal bug fixes and doc corrections, not new external-facing behavior.]

## Existing Implementations

- The original feature's implementation (`specs/archive/260814-boabot-native-daemon-mode/`) and its 3 commits (`d771f75`, `793699b`, `441dc36`) are the direct object of these fixes.
- The code review itself (`boabot-native-daemon-mode-auto-review-PRD.md`, this spec's source) already did significant verification work (Verification Notes section) — reuse those findings rather than re-deriving them from scratch.

## API Documentation

[TBD — no external APIs involved.]

## Best Practices

[TBD]

## Open Questions

- None beyond the 3 already resolved with explicit recommendations in the review PRD's "Open Decisions" section (FR-106, FR-107, FR-108 item 1) — see spec.md.

## References

- Source PRD: [boabot-native-daemon-mode-auto-review-PRD.md](./boabot-native-daemon-mode-auto-review-PRD.md)
- Original feature spec (archived): `specs/archive/260814-boabot-native-daemon-mode/`
