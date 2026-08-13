# Plan: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13
**Status:** Planning

## Development Approach

TDD (red-green-refactor), per repo AGENTS.md — a failing test before any production code, for every task below. 90%+ coverage target on domain/application layers; `internal/infrastructure/acp` is infrastructure, not subject to that specific gate, but still fully unit-tested per repo mocking conventions (mock `Worker`/`WorkerFactory`, and mock the ACP SDK's client-side connection where the SDK supports it). Note: no cost-enforcement mock is needed — `internal/application/cost.EnforceBudgetUseCase` exists but isn't wired into ACP mode (or native mode); see spec.md's corrected FR-005.

## Phase Breakdown

1. **Dependency + skeleton** — add `github.com/coder/acp-go-sdk`, create `internal/infrastructure/acp/` package skeleton, wire a no-op `acp` mode into `main.go` behind a flag.
2. **`initialize` + `session/new`** — minimal handshake, session allocation, unit-tested.
3. **`session/prompt` core** — build `domain.Task` from prompt content, call `WorkerFactory.New().Execute`, map `TaskResult` to the ACP response (no keep-alive yet).
4. **Keep-alive + `session/cancel`** — concurrent ticker emitting `acp::thought` updates during a turn; cancellation wired to the `Worker.Execute` context.
5. **Usage wiring (scoped down)** — leave `PromptResponse.Usage` nil per corrected FR-005; no cost-enforcement source is wired for v1. Confirm this doesn't break `buzz-acp` (Usage is documented optional in the ACP SDK).
6. **Process lifecycle** — clean shutdown on stdin EOF, panic recovery per FR-008.
7. **Integration test against the real `buzz-acp` binary** — the acceptance-criteria smoke test; tagged `//go:build integration` per repo convention.
8. **Docs + ADR** — new/superseding ADR entry, `docs/technical-details.md`, `docs/product-summary.md`, `README.md`.

## Critical Path

1 → 2 → 3 → 4 → 5 → 6 → 7, with 8 running in parallel once 3 is stable (docs can describe the working core before every edge case is polished).

## Testing Strategy

- Unit tests per phase above (red-green-refactor), mocking `domain.Worker`/`WorkerFactory` per existing repo conventions (`internal/domain/mocks/`).
- Integration test (`//go:build integration`) spawning the real bundled `buzz-acp` binary against `boabot acp`, verifying at minimum: `initialize` handshake succeeds, a `session/prompt` round-trip returns `stopReason: EndTurn`, and a deliberately slow task (mocked/stubbed Worker with an artificial delay exceeding a short test `--idle-timeout`) is NOT killed, proving the keep-alive mechanism actually works — not just that it's present.
- Full `go test ./...` must remain green throughout — no regression to native daemon-mode tests at any phase.

## Rollout Strategy

Ships as an additive `boabot acp` mode; no config migration, no changes to existing deployments. Documented as an alternative to native `ChannelMonitor`-based Buzz integration in `boabot/user-docs/`, with clear guidance on when to choose which (always-on team identity → native; lightweight `buzz-acp`-managed harness → ACP mode).

## Success Metrics

See spec.md Acceptance Criteria for the binary pass/fail gates. No additional metrics defined for v1.
