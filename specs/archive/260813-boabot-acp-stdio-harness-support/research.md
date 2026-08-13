# Research: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13
**Source PRD:** `boabot-acp-stdio-harness-support-PRD.md` (this directory)

## Research Questions

1. ~~Does a maintained Go implementation/SDK of the Agent Client Protocol exist~~ **RESOLVED — yes.**
2. ~~What exact ACP method names, request/response schemas, and protocol version does `buzz-acp` actually implement/expect~~ **RESOLVED — confirmed via `strings` on the actual binary.**
3. ~~Does `buzz-acp` spawn one long-lived harness process per session or a fresh process per turn~~ **RESOLVED — persistent pooled processes.**
4. What does `buzz-acp --mcp-command` configure? **Still open** — confirmed to exist as a CLI flag / config field (`mcp` near `command`/`args`/`env` in its own JSON config shape) but exact semantics not resolved from static analysis alone. Lower priority now that the core protocol/lifecycle questions are answered — `boabot`'s ACP mode does not need this to implement FR-001–FR-008; defer investigation to implementation time if BaoBot's own MCP tool access needs to interact with it.
5. ~~How do existing ACP harnesses in this ecosystem implement their ACP mode~~ **Partially resolved** — `goose` is not installed on this machine, so its ACP mode couldn't be inspected directly. The `coder/acp-go-sdk`'s own `example/claude-code` reference agent (below) is the better available reference.

## Industry Standards

ACP (Agent Client Protocol, https://agentclientprotocol.com/) is a JSON-RPC 2.0-based protocol between a **Client** (host — here, `buzz-acp`) and an **Agent** (here, `boabot acp`). BaoBot needs to implement the **Agent** side.

## Existing Implementations

**Go SDK — `github.com/coder/acp-go-sdk`** (Apache-2.0, actively maintained, latest tag `v0.13.5` at research time). This is a complete, idiomatic Go implementation of ACP — **no hand-rolled JSON-RPC transport needed.**

- To implement an Agent: satisfy the `acp.Agent` interface, then wrap it with `acp.NewAgentSideConnection(agent, os.Stdout, os.Stdin)` — the SDK owns the stdio transport, framing, and JSON-RPC dispatch entirely.
- Ships `example/claude-code` — a reference implementation that bridges an *existing* CLI coding agent into ACP. This is structurally the closest analog to what BaoBot needs (wrapping an existing agent runtime, not building a fresh one) and should be the primary implementation reference.
- Ships `example/agent` — a minimal from-scratch example.
- Client-side types (`RequestPermission`, `SessionUpdate` with union variants `AgentMessageChunk`/`ToolCall`/`ToolCallUpdate`/`Plan`) match the method/field names confirmed in `buzz-acp`'s own strings output — cross-validated, not just spec-asserted.
- Supports vendor extension methods (`_`-prefixed, e.g. goose's `_goose/unstable/session/steer`) — not needed for BaoBot's scope.
- Protocol version negotiation is handled internally by the SDK during `Initialize` — no raw version integer needs to be hand-tracked in BaoBot's code.

**`goose`** — not installed on this machine (`which goose` → not found); its ACP mode could not be inspected directly. Not a blocker given the SDK's own reference example.

## API Documentation

Confirmed via `strings` on the actual bundled `/Applications/Buzz.app/Contents/MacOS/buzz-acp` binary (ground truth — not spec-reading alone):

| Method | Notes |
|---|---|
| `initialize` / result | Fields: `protocolVersion`, `clientCapabilities`, `clientInfo`, `agentInfo`, `serverInfo`, `authMethods`. |
| `authenticate` | Present in the method set. |
| `session/new` | Returns `sessionId`; accepts `_meta.sessionTitle`. |
| `session/prompt` | Response includes `stopReason` (enum: `EndTurn`, `Cancelled`, `MaxTokens`, `MaxTurnRequests`, `Refusal`) and a usage block (`inputTokens`/`outputTokens`/`totalTokens`/`cachedWriteTokens`). |
| `session/update` | Streaming **notification** (not a request/response) — update kinds seen: `acp::stream`, `acp::tool`, `acp::plan`, `acp::thought`, `acp::usage`. This is FR-003's mechanism. |
| `session/cancel` | Present. |
| `session/request_permission` | Present — buzz-acp defaults `--permission-mode` to `bypass-permissions`, so this may not be exercised in the default config, but the Agent side should still implement it correctly. |
| `session/set_config_option` | Used at minimum for `configId: "mode"` (permission mode). |
| `session/set_model` | Present. |

Example harness identifiers found in the binary: `"goose"`, `"claude-agent-acp"`, `"codex-acp"` — establishes the naming convention a `boabot`-side ACP harness should likely follow (e.g. a `boabot-acp` binary or `boabot acp` subcommand, to be finalized in architecture.md).

`buzz-acp` assembles the full prompt content itself before sending it via `session/prompt` — a `[Base]` platform-context section, optional `--team-instructions`, optional NIP-AE agent-memory injection ("Agent Memory — core"), then the persona's own `[System]` prompt, then the user message. **BaoBot's ACP mode receives this as ordinary prompt content and does not need to construct or parse this structure itself** — it's opaque prompt text from BaoBot's perspective, handled the same way native daemon-mode tasks are.

`buzz-acp` has its own `models` and `auth-methods` CLI subcommands (found via strings, not shown in top-level `--help`) and reads its own `./buzz-acp.toml` config, entirely separate from BaoBot's `config.yaml` — no overlap/conflict to design around.

## Best Practices

Model BaoBot's ACP infrastructure package on `coder/acp-go-sdk`'s `example/claude-code`: an existing agent runtime (there, Claude Code; here, BaoBot's `Worker`) driven through a thin ACP adapter, rather than a ground-up protocol implementation.

## Open Questions

- `buzz-acp --mcp-command` exact semantics (Research Question 4, above) — deferred, non-blocking.
- Should ACP mode support the same multi-provider (Anthropic/Bedrock/OpenAI) configuration as native mode, or a narrower v1 default? Still open — recommend v1 supports whatever the persona's `config.yaml` already specifies (same as native mode), since FR-004 already requires reusing that config shape unchanged; no new restriction is implied by anything found in research, so defer explicit narrowing unless architecture.md finds a concrete reason to.

## References

- https://agentclientprotocol.com/
- `github.com/coder/acp-go-sdk` (v0.13.5) — https://pkg.go.dev/github.com/coder/acp-go-sdk
- `boabot/docs/architectural-decision-record.md` (ADR-B020)
- `specs/archive/260804-boabot-buzz-support/boabot-buzz-support-PRD.md`
- `/Applications/Buzz.app/Contents/MacOS/buzz-acp --help` and `strings` output (captured 2026-08-13)
