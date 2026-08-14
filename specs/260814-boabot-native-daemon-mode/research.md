# Research: Boabot Native Daemon Mode (Multi-Agent Buzz Support)

**Created:** 2026-08-14
**Source PRD:** [boabot-native-daemon-mode-PRD.md](./boabot-native-daemon-mode-PRD.md)

## Research Questions

1. Exact `DirectTaskSource` value for Buzz-originated tasks — new `DirectTaskSourceBuzz`, or reuse of `DirectTaskSourceChat`? What UI filtering/labeling depends on this choice?
2. How does `buildBuzzMonitor` currently resolve `cfg.Buzz` and the `buzz_private_key` secret today, and what's the minimal-diff way to make it a per-bot factory called once per Buzz-enabled persona instead of once process-wide?
3. Does `DirectTaskStore`'s and the board-store's existing JSON-file persistence (`NewInMemoryDirectTaskStore` and equivalent) have adequate locking for concurrent writes from multiple personas' monitors dispatching at the same time, or does it need hardening?
4. What tool/prompt design lets a persona turn a natural-language scheduling request ("run this every day at 9am") into a structured `Schedule`/`RecurrenceRule` via `DispatchWithSchedule` — is there a reusable tool-call pattern from the web-UI chat path already, or is this net-new?
5. Which second `boabot-team` persona (beyond the primary demo persona) will be used to demonstrate multi-agent participation, and does it already have `buzz:` config scaffolding in its `config.yaml`?

## Industry Standards

[TBD — not expected to be relevant; this is an internal wiring extension, not a new external-facing protocol.]

## Existing Implementations

- `buildBuzzMonitor` (single-identity Buzz monitor construction) — reference implementation for per-bot factory extension.
- Web-UI chat dispatch path through `Dispatcher`/`DirectTaskStore` — reference for how Buzz-originated dispatch should plug in.
- FR-036 (existing single-monitor failure-isolation pattern) — reference for extending isolation to N monitors.
- ADR-B027 (`internal/infrastructure/acp` fallback-publish fix) — reference for the class of risk in NL→structured-data reliability, applicable to NL→schedule parsing.

## API Documentation

[TBD — internal APIs only (`Dispatcher`, `DirectTaskStore`, `BoardStore`, `SecretStore`); no external API integration beyond the existing Buzz/Nostr relay client already in use.]

## Best Practices

[TBD]

## Open Questions

- See Research Questions above — all five carry into Phase 2/3 research and must be resolved before task breakdown (Phase 4).
- **Resolved during Phase 0:** Should Buzz DM (direct message) listening be in scope for this feature? Investigated the existing code — neither native mode nor ACP mode handles DMs today; `trigger.go:7` explicitly flags NIP-17 gift-wrap DM support (kind 1059) as deferred/out of scope. Decision: keep DMs out of scope for this feature too; documented as a Non-Goal in `spec.md` and the source PRD. DM listening is candidate future-PRD work.

## References

- Source PRD: [boabot-native-daemon-mode-PRD.md](./boabot-native-daemon-mode-PRD.md)
- `spec.md` in this directory
