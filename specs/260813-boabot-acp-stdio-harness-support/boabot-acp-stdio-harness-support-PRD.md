# PRD: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13
**Jira:** N/A
**Status:** Draft

## Problem Statement

Buzz workspaces support "custom harness" agents: `buzz-acp` (Block's Rust ACP bridge) owns a workspace identity's Buzz relay connection, and for each turn spawns a configured `--agent-command`/`--agent-args` process that speaks the [Agent Client Protocol](https://agentclientprotocol.com/) (ACP) over stdio — the same mechanism already used to run `goose acp`, and presumably `claude ... acp` / `codex ... acp`, as Buzz-connected agents (see the `.claude`/`.codex`/`.goose` directories `buzz-desktop` scaffolds under `~/.buzz`).

BaoBot's existing Buzz integration (native `internal/infrastructure/buzz` `ChannelMonitor`, merged in PR #26) makes BaoBot own its *own* relay connection, private key, and channel discovery — a good fit for a long-running, always-on team deployment, but it means every BaoBot persona that wants to join a workspace needs its own provisioned Nostr identity and a full `boabot` daemon process.

This PRD covers a second, complementary integration: letting an operator register a BaoBot persona as a `buzz-acp` custom harness — i.e., giving `boabot` a stdio ACP server mode that `buzz-acp --agent-command boabot --agent-args acp ...` can spawn per turn, the same way it spawns `goose`. This was evaluated once already (original Buzz-support PRD, "Option B", and non-goal NG1) and explicitly rejected for the *native relay* integration's sake — see ADR-B020. This PRD does not overturn that decision; native `ChannelMonitor` remains the right model for BaoBot-owned, always-on team identities. It adds an ACP-mode entrypoint as an alternative, opt-in distribution path — matching the PRD's own forward-looking note that `buzz-acp` "is the right tool if we ever want to expose a BaoBot persona to a Buzz workspace without our cluster."

## Goals

- Give `boabot` a CLI mode that speaks ACP over stdio well enough for `buzz-acp` to drive it through a full turn (initialize → prompt → streamed output → completion/cancellation).
- Reuse BaoBot's existing single-turn execution engine (`Worker`/`WorkerFactory`, model provider abstraction, tool attention, skills, memory, `BudgetTracker`, calibrated-autonomy gates) rather than building a second, parallel agent runtime — directly addressing ADR-B020's "duplicated turn-loop/budget/autonomy logic" objection.
- Keep this fully additive and optional: the native `ChannelMonitor`/`TeamManager` daemon deployment model is unchanged and unaffected by this mode's existence.

## Non-Goals

- Replacing or deprecating the native Buzz `ChannelMonitor` integration (PR #26). Both models coexist; an operator picks one per persona.
- Implementing the full breadth of ACP (e.g. IDE-oriented filesystem/editor capabilities used by Zed). Scope is limited to what `buzz-acp`'s turn-taking bridge actually exercises: session lifecycle, prompt turns, streamed updates, cancellation.
- Multi-bot orchestration within a single `boabot acp` process. One process = one persona/config, matching `buzz-acp`'s own `--agents N` model of spawning N independent subprocesses rather than one process serving many.
- BaoBot managing any Buzz relay connection, private key, or event signing in ACP mode — `buzz-acp` exclusively owns the relay/identity side; `boabot acp` only ever speaks local stdio ACP.
- Multi-repo/editor-specific ACP capabilities (workspace file edits, LSP-style features) beyond what a chat-turn agent needs.

## Functional Requirements

**FR-001:** `boabot` supports a new invocation mode (`boabot acp` or equivalent flag) that, instead of starting the long-running multi-bot `TeamManager.Run()` daemon loop, starts a single-persona ACP JSON-RPC server communicating over stdin/stdout.

**FR-002:** ACP mode implements the minimum ACP method set required for a `buzz-acp`-driven turn: session initialization, a new-session call, a prompt/turn call, and cancellation — exact method names/schema to be pinned against the ACP spec version `buzz-acp` implements (see Open Questions).

**FR-003:** ACP mode streams incremental turn output (text deltas, tool-call events) back to the host via ACP update notifications as the underlying `Worker` produces them, rather than buffering the full response before replying.

**FR-004:** ACP mode loads exactly one bot persona's configuration (the same `config.yaml` shape used by native daemon mode: `bot`, `models`, tool/budget/context settings) via a CLI flag, so a persona's behavior is identical whether run natively or under ACP.

**FR-005:** ACP mode enforces the same `BudgetTracker` caps and calibrated-autonomy gates that native daemon mode enforces, scoped to the single session/turn — this is the direct fix for ADR-B020's rejection rationale, not a relaxation of it.

**FR-006:** ACP mode never opens a Slack or Buzz relay connection and registers no `ChannelMonitor` — it is purely a local stdio protocol server; `buzz-acp` is solely responsible for the Buzz-side relay connection, private key, and reply publication.

**FR-007:** The `boabot acp` process exits cleanly on stdin EOF or host-initiated shutdown, and respects mid-turn cancellation (so it plays correctly with `buzz-acp`'s `--idle-timeout`/`--max-turn-duration` process-lifecycle controls).

**FR-008:** Worker panics during an ACP turn are recovered and surfaced as a protocol-compliant error response, never a raw process crash — extending the existing "worker thread panics must not kill the main thread" rule to ACP mode.

## Non-Functional Requirements

- **Performance:** Streamed output must begin emitting before the full turn completes — no full-response buffering. `# TODO:` a specific latency target (e.g. "first token within Ns") was not set during this pass.
- **Reliability:** A turn must be cancellable and bounded — the underlying `Worker` call needs a `context.Context` wired to ACP cancellation and to `buzz-acp`'s max-turn-duration, so a hung turn cannot leak a runaway process.
- **Security:** ACP mode introduces no new secret categories. It never touches `BUZZ_PRIVATE_KEY`/Buzz secrets (out of scope, owned by `buzz-acp`); it still requires the persona's own model-provider credentials exactly as native daemon mode does.
- **Observability:** Turn start/end, tool calls, and errors are logged with the same structure/fields as native daemon-mode task execution, so ACP-mode runs are debuggable the same way.
- **Compatibility:** Must interoperate with the actual bundled `/Applications/Buzz.app/Contents/MacOS/buzz-acp` binary's expectations (default `--agent-args acp`, JSON-RPC over stdio) — verified by integration test against that binary, not just spec reading.

## Acceptance Criteria

- [ ] `boabot acp -config <bot-config.yaml>` starts and responds correctly to an ACP session-initialization request over stdio.
- [ ] A prompt/turn request drives a real `Worker` execution using the persona's configured model provider and tools, and returns streamed update notifications plus a final result.
- [ ] End-to-end smoke test: `buzz-acp --agent-command boabot --agent-args acp ...` against a real (or recorded/mocked) relay, where a channel mention results in `buzz-acp` spawning `boabot acp`, receiving a real reply, and publishing it.
- [ ] Native `ChannelMonitor`/`TeamManager` daemon-mode behavior and tests are unaffected — full `go test ./...` passes with no regressions.
- [ ] New ACP code meets the repo's 90%+ coverage target on domain/application layers.
- [ ] `docs/architectural-decision-record.md` gets a new ADR entry that supersedes/complements ADR-B020, explaining specifically how this design avoids the original control-inversion and duplicated-logic objections.
- [ ] `docs/technical-details.md`, `docs/product-summary.md`, and `README.md` updated to document the new mode and how it differs from native Buzz integration.

## Dependencies and Risks

| Item | Type | Notes |
|------|------|-------|
| ACP protocol implementation (Go) | Dependency | No existing ACP support or dependency found in `go.mod`. `# TODO:` confirm whether a Go ACP SDK exists (the reference implementation is Rust/TS-oriented); may require hand-rolling the JSON-RPC 2.0 method set against the public spec. |
| `buzz-acp`'s exact ACP method/schema expectations | Dependency | Partially understood from `buzz-acp --help` (agent-command/agent-args/idle-timeout/max-turn-duration contract); exact JSON-RPC method names and payload schema not yet verified against the binary. |
| ADR-B020 (prior rejection) | Risk | Re-introducing the same control-inversion/duplicated-logic problems if FR-005 isn't implemented faithfully. Mitigation: acceptance criteria require an explicit new/superseding ADR entry, not a silent reversal. |
| Per-turn process spawn cost | Risk | Unclear whether `buzz-acp` reuses one spawned harness process across a session's turns or spawns fresh per turn; cold-start cost (loading BaoBot's config/tools/memory) may matter. `# TODO:` confirm against `buzz-acp` behavior. |
| `--mcp-command` flag on `buzz-acp` | Risk / Open question | Unclear whether this is for the harness's own MCP tool access or something `buzz-acp` manages independently — needs investigation before FR scope is finalized. |

## Open Questions

- Which ACP protocol version/spec should BaoBot target, and how do we verify compliance against the actual bundled `buzz-acp` binary rather than the spec alone?
- Does `buzz-acp` spawn one long-lived harness process per session (multiple turns) or one process per turn? This materially affects FR-001/FR-007's process-lifecycle design.
- Is a Go ACP SDK available/maintained, or does this require a hand-rolled JSON-RPC 2.0 stdio implementation?
- What does `buzz-acp --mcp-command` actually configure, and does BaoBot's ACP mode need to interoperate with it?
- Should ACP mode support the same multi-provider (Anthropic/Bedrock/OpenAI) model configuration as native mode, or is a narrower default acceptable for v1?
