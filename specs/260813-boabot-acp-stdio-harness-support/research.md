# Research: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13
**Source PRD:** `boabot-acp-stdio-harness-support-PRD.md` (this directory)

## Research Questions

1. Does a maintained Go implementation/SDK of the Agent Client Protocol exist (analogous to the TypeScript/Rust reference implementations), or does BaoBot need to hand-roll the JSON-RPC 2.0 stdio transport and method set?
2. What exact ACP method names, request/response schemas, and protocol version does `buzz-acp` (the bundled `/Applications/Buzz.app/Contents/MacOS/buzz-acp` binary) actually implement/expect? Verify empirically against the binary, not spec text alone.
3. Does `buzz-acp` spawn one long-lived harness process per session (multiple turns over one stdio connection) or a fresh process per turn? This decides FR-001/FR-007's lifecycle design.
4. What does `buzz-acp --mcp-command` configure — is it relevant to how BaoBot's ACP mode should expose or receive MCP tool access?
5. How do existing ACP harnesses in this ecosystem (`goose acp`, and any `claude`/`codex` ACP subcommands referenced by the `.claude`/`.codex` directories under `~/.buzz`) implement their ACP mode — is there a reference implementation pattern worth mirroring?

## Industry Standards

`[TBD]` — populate from the ACP spec at https://agentclientprotocol.com/ during Phase 2.

## Existing Implementations

- `goose acp` — default `--agent-command`/`--agent-args` in `buzz-acp --help`; likely the most direct reference for expected behavior.
- `[TBD]` — check whether `claude`/`codex` CLIs have an `acp` subcommand and, if so, inspect their method coverage.

## API Documentation

`[TBD]` — ACP protocol method reference (initialize, session/new, session/prompt, session/update, session/cancel or equivalent) to be captured here once confirmed against the spec and the bundled `buzz-acp` binary.

## Best Practices

`[TBD]`

## Open Questions

Carried forward from the PRD:

- Which ACP protocol version should BaoBot target, and how do we verify compliance against the actual bundled `buzz-acp` binary?
- Per-turn vs. per-session process lifecycle (see Research Question 3).
- Go ACP SDK availability (see Research Question 1).
- `buzz-acp --mcp-command` semantics (see Research Question 4).
- Should ACP mode support the same multi-provider (Anthropic/Bedrock/OpenAI) configuration as native mode, or a narrower v1 default?

## References

- https://agentclientprotocol.com/
- `boabot/docs/architectural-decision-record.md` (ADR-B020)
- `specs/archive/260804-boabot-buzz-support/boabot-buzz-support-PRD.md`
- `/Applications/Buzz.app/Contents/MacOS/buzz-acp --help` output (captured during PRD interview, 2026-08-13)
