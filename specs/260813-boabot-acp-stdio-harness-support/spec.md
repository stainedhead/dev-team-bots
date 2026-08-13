# Spec: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13
**Status:** Draft

## Executive Summary

Give `boabot` a second, opt-in entrypoint that speaks the Agent Client Protocol (ACP) over stdin/stdout, so a single BaoBot persona can be registered as a `buzz-acp` custom harness — spawned per turn/session by Block's `buzz-acp` bridge the same way `goose acp` is today — without requiring that persona to own its own Buzz relay identity, private key, or always-on daemon process. This is additive: the native `internal/infrastructure/buzz` `ChannelMonitor` integration (merged PR #26) is untouched and remains the recommended path for always-on, BaoBot-owned team identities.

## Problem Statement

Buzz workspaces support "custom harness" agents: `buzz-acp` owns a workspace identity's relay connection and, per turn, spawns a configured `--agent-command`/`--agent-args` process that speaks ACP over stdio. BaoBot currently has no such mode — its only Buzz integration is the native `ChannelMonitor`, which requires a dedicated Nostr keypair and a persistent `boabot` daemon per persona. Operators who want the lighter-weight, `buzz-acp`-managed distribution model (as already used for `goose`, and presumably `claude`/`codex`, per the `.claude`/`.codex`/`.goose` directories Buzz Desktop scaffolds) have no way to register BaoBot that way today.

## Goals / Non-Goals

**Goals:**
- `boabot` gains a CLI mode that speaks ACP over stdio sufficiently for `buzz-acp` to drive a full turn (initialize → prompt → streamed output → completion/cancellation).
- Reuse BaoBot's existing single-turn execution engine (`Worker`/`WorkerFactory`, model provider abstraction, tool attention, skills, memory, `BudgetTracker`, calibrated-autonomy gates) — not a parallel runtime.
- Fully additive: native `ChannelMonitor`/`TeamManager` daemon mode is unaffected.

**Non-Goals:**
- Replacing or deprecating the native Buzz `ChannelMonitor` integration.
- Full ACP breadth (IDE/editor-oriented filesystem, LSP-style capabilities) — scope is limited to what `buzz-acp`'s bridge exercises.
- Multi-bot orchestration within one `boabot acp` process — one process = one persona/config.
- BaoBot managing any Buzz relay connection, private key, or event signing in ACP mode — exclusively `buzz-acp`'s responsibility.

## User Requirements

**FR-001:** `boabot` supports a new invocation mode (`boabot acp` or equivalent flag) that starts a single-persona ACP JSON-RPC server over stdin/stdout instead of the long-running `TeamManager.Run()` daemon loop.

**FR-002:** ACP mode implements the minimum ACP method set `buzz-acp` requires: session initialization, new-session, prompt/turn, cancellation. Exact method names/schema to be pinned during research against the ACP spec version `buzz-acp` implements.

**FR-003:** ACP mode streams incremental turn output (text deltas, tool-call events) via ACP update notifications as the `Worker` produces them, without buffering the full response first.

**FR-004:** ACP mode loads exactly one bot persona's `config.yaml` (same shape as native daemon mode) via a CLI flag, so persona behavior is identical across both modes.

**FR-005:** ACP mode enforces the same `BudgetTracker` caps and calibrated-autonomy gates as native daemon mode, scoped to the single session/turn.

**FR-006:** ACP mode never opens a Slack or Buzz relay connection and registers no `ChannelMonitor` — purely a local stdio protocol server.

**FR-007:** `boabot acp` exits cleanly on stdin EOF or host shutdown, and respects mid-turn cancellation, compatible with `buzz-acp`'s `--idle-timeout`/`--max-turn-duration` controls.

**FR-008:** Worker panics during an ACP turn are recovered and surfaced as protocol-compliant error responses, never a raw process crash.

## Non-Functional Requirements

- **Performance:** Streamed output begins before full-turn completion. `[TBD]` specific latency target.
- **Reliability:** Turn execution is bound to a cancellable `context.Context` wired to ACP cancellation and `buzz-acp`'s max-turn-duration.
- **Security:** No new secret categories; ACP mode never touches `BUZZ_PRIVATE_KEY`/Buzz secrets; still requires the persona's model-provider credentials.
- **Observability:** Turn start/end, tool calls, and errors logged with the same structure as native daemon-mode task execution.
- **Compatibility:** Verified against the actual bundled `buzz-acp` binary, not spec-reading alone.

## System Architecture

**Affected layers:**
- `cmd/boabot/main.go` — new CLI subcommand/flag routing to ACP mode instead of `mgr.Run(ctx)`.
- New infrastructure package, e.g. `internal/infrastructure/acp/` — ACP JSON-RPC-over-stdio server implementation (transport + method handlers).
- `internal/domain/` — likely a new narrow interface if ACP-mode turn execution needs a seam distinct from `ChannelMonitor`/`MessageQueue` (native mode's task-intake abstraction assumes an async multi-bot queue, which doesn't fit ACP's synchronous per-session model). Exact seam is `[TBD]` pending architecture.md.
- `internal/application/` — a use case wrapping a single `Worker` invocation per ACP turn, reusing existing `BudgetTracker`/autonomy-gate application logic rather than duplicating it.

**New/modified components:** ACP transport/protocol handler, ACP-mode wiring in `main.go`, likely a session-turn use case. No changes anticipated to `TeamManager`, native `ChannelMonitor` implementations, or `MessageQueue`.

## Scope of Changes

- **New files:** `internal/infrastructure/acp/` package (server, protocol types, tests); `internal/application/acp/` or similar use-case package (exact naming `[TBD]`).
- **Modified files:** `cmd/boabot/main.go` (new mode routing); `docs/architectural-decision-record.md` (new/superseding ADR entry vs. ADR-B020); `docs/technical-details.md`, `docs/product-summary.md`, `README.md`.
- **Dependencies:** Possible new Go module dependency for ACP/JSON-RPC — existence of a maintained Go ACP SDK is unconfirmed; see research.md.

## Breaking Changes

None anticipated. This is a new, additive, opt-in CLI mode. No changes to existing `config.yaml` schema, native daemon behavior, or public interfaces of existing packages.

## Success Criteria and Acceptance Criteria

- [ ] `boabot acp -config <bot-config.yaml>` starts and responds correctly to ACP session initialization over stdio.
- [ ] A prompt/turn request drives a real `Worker` execution and returns streamed updates plus a final result.
- [ ] End-to-end smoke test against the real (or recorded) `buzz-acp` binary: a channel mention → `buzz-acp` spawns `boabot acp` → real reply → published to channel.
- [ ] Native daemon-mode tests unaffected — full `go test ./...` passes with no regressions.
- [ ] New ACP code meets 90%+ coverage on domain/application layers.
- [ ] New/superseding ADR entry added addressing ADR-B020's original objections directly.
- [ ] `README.md`, `docs/technical-details.md`, `docs/product-summary.md` updated.

## Risks and Mitigation

| Risk | Mitigation |
|------|------------|
| No maintained Go ACP SDK exists | Research phase confirms availability; fall back to a minimal hand-rolled JSON-RPC 2.0 stdio implementation scoped to the required method set only. |
| Re-introducing ADR-B020's control-inversion/duplicated-logic problems | FR-005 requires reuse of existing `BudgetTracker`/autonomy-gate logic; acceptance criteria require an explicit ADR entry documenting how this differs from the original rejected design. |
| Unclear `buzz-acp` per-turn vs. per-session process lifecycle | Research phase inspects `buzz-acp` behavior/source (if available) and/or tests empirically against the bundled binary before finalizing FR-001/FR-007 design. |
| Protocol version drift vs. `buzz-acp`'s actual expectations | Integration-test against the real bundled `buzz-acp` binary, not spec compliance alone. |

## Timeline and Milestones

`[TBD]` — to be filled in during plan.md / tasks.md breakdown.

## References

- PRD: `specs/260813-boabot-acp-stdio-harness-support/boabot-acp-stdio-harness-support-PRD.md`
- Prior rejection: `boabot/docs/architectural-decision-record.md` (ADR-B020)
- Prior evaluation: `specs/archive/260804-boabot-buzz-support/boabot-buzz-support-PRD.md` (Option B, NG1)
- Native Buzz integration: `boabot/user-docs/Buzz-Adoption-Config.md`, `internal/infrastructure/buzz/`
- ACP protocol: https://agentclientprotocol.com/
