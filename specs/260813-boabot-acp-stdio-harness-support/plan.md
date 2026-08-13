# Plan: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13
**Status:** Planning

## Development Approach

TDD (red-green-refactor), per repo AGENTS.md. `[TBD]` further detail pending Phase 2/3 completion.

## Phase Breakdown

`[TBD]` — to be broken into concrete phases once research (Phase 2) resolves the open protocol/lifecycle questions and architecture (Phase 3) is drafted.

## Critical Path

`[TBD]` — likely: confirm Go ACP SDK availability / protocol details → domain interface design → infrastructure ACP server → application use case wiring → `main.go` mode routing → integration test against real `buzz-acp` binary.

## Testing Strategy

- Unit tests for the ACP protocol handler (mocked transport) and the application-layer turn use case (mocked `Worker`), per repo mocking conventions.
- Integration test(s) tagged `//go:build integration` exercising the real bundled `buzz-acp` binary end-to-end, per AGENTS.md's "Adding a New Infrastructure Adapter" guidance.
- Regression coverage: full `go test ./...` must remain green for existing native daemon-mode code.

## Rollout Strategy

`[TBD]`

## Success Metrics

`[TBD]` — see spec.md Acceptance Criteria for the binary pass/fail gates.
