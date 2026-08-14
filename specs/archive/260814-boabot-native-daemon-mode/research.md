# Research: Boabot Native Daemon Mode (Multi-Agent Buzz Support)

**Created:** 2026-08-14
**Source PRD:** [boabot-native-daemon-mode-PRD.md](./boabot-native-daemon-mode-PRD.md)

## Research Questions

1. ~~Exact `DirectTaskSource` value for Buzz-originated tasks~~ — **Resolved.** `internal/domain/direct_task.go:9-17` defines three values today: `DirectTaskSourceChat`, `DirectTaskSourceOperator`, `DirectTaskSourceBoard`. No exhaustive switch exists over the enum — only equality checks (`team_manager.go:915`, `http/server.go:1139,1219`, `direct_task_store.go:180`). Adding `DirectTaskSourceBuzz` is additive and safe. The one place that matters: `internal/application/execute_task.go:100` selects `chatProvider` via a raw string check `task.Source == "chat"` — this must be extended to also match Buzz-sourced tasks (either add an explicit `"buzz"` branch, or a small `isConversationalSource(source)` helper) so Buzz-dispatched work gets the same provider treatment as chat-dispatched work. **Decision:** add `DirectTaskSourceBuzz`; extend the `execute_task.go:100` check accordingly.
2. ~~`buildBuzzMonitor` resolution / per-bot factory~~ — **Resolved.** `buildBuzzMonitor` (`boabot/cmd/boabot/main.go:196-266`) is called exactly once, process-wide, from `run()` (`main.go:174`), against the single daemon-level `cfg` loaded in `main()` (`main.go:80`). It guards on `cfg.Buzz.Enabled`, resolves the private key via `buzzinfra.LoadKeypair(ctx, store, bc.BotName)` (`main.go:206`) — already namespaced per bot name, not global. `mgr.WithChannelMonitor(m)` (`team_manager.go:208`) just appends to a slice — already safe to call N times (both Slack and Buzz call it today). `TeamManager` already has team-entry loading (`loadTeamConfig`, `team_manager.go:1086`, currently unexported) and per-bot `config.Load` (`team_manager.go:487-488`). **Decision:** in `run()`, loop over Buzz-enabled `team.yaml` entries, `config.Load` each bot's own `config.yaml`, and call `buildBuzzMonitor` + `mgr.WithChannelMonitor` once per persona instead of the single call at line 174. No signature change needed to `buildBuzzMonitor`. Team-entry loading needs to be exposed from `team_manager.go` (currently unexported) rather than reimplemented in `main.go` — keeps wiring (`main.go`) from duplicating config-parsing logic.
3. ~~JSON-store concurrency safety~~ — **Resolved — no hardening needed.** Both `InMemoryDirectTaskStore` (`internal/infrastructure/local/orchestrator/direct_task_store.go:22-26`) and `InMemoryBoardStore` (`board.go:26-27`) already use `sync.RWMutex` around every read/write, with atomic persist via temp-file + `os.Rename`. Concurrent multi-persona dispatch is already safe against these stores. `-race` tests should still be added to lock in this guarantee, but no store-level code change is required.
4. ~~NL scheduling mechanism~~ — **Resolved.** `ParseScheduleNL` (`internal/application/orchestrator/chat_task_manager.go:295`) is a regex/heuristic parser (not an LLM tool call) that turns chat text into a `domain.Schedule`/`RecurrenceRule`. `ChatTaskManager.DetectAndHandle` (`chat_task_manager.go:60-90`) drives a confirm/cancel flow before calling `dispatcher.DispatchWithSchedule(...)`. **Decision:** reuse `ParseScheduleNL`/`ChatTaskManager`'s existing heuristic parser and confirm/cancel flow for the Buzz path rather than building a new NL→schedule mechanism — same call path, invoked with Buzz-originated dispatch context instead of chat's. Revisit only if Buzz message phrasing in practice defeats the existing heuristics (tracked as an implementation-time risk, not a design gap).
5. Which second `boabot-team` persona will be used to demonstrate multi-agent participation — **narrowed, not fully resolved.** `boabot-team/team.yaml` already has 3 enabled bots with a `buzz:` block (`enabled: true`, same relay URL): `orchestrator`, `architect`, `tech-lead`. `implementer`, `reviewer`, `maintainer` have no `buzz:` block. So no new config-wiring is needed for a second persona — this is now purely a pick among 3 already-ready personas, plus provisioning each chosen persona's `buzz_private_key` secret. **Open:** which 2 (of 3) to actually provision and use for the demo — repo owner/operator decision, not a technical unknown.

### Additional item surfaced during research (not in original 5)

- The PRD/spec state today's single Buzz dispatch "goes straight to `Worker.Execute`... bypassing `DirectTaskStore`/board entirely" — the exact current call site for that direct dispatch (where in the Buzz monitor's message-handling path `Worker.Execute` is invoked today) was not traced in this research pass. This is the central thing FR-005's bridge replaces, so it's recorded as the **first implementation task** (see `tasks.md` P1.0) rather than blocking spec approval on another research round.

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

- RQ5 (which second persona to provision) — operator decision, not yet made; does not block task breakdown since either choice (architect or tech-lead) follows the identical wiring path.
- Exact current Buzz→`Worker.Execute` call site — traced as implementation task P1.0, not yet located.
- **Resolved during Phase 0:** Should Buzz DM (direct message) listening be in scope for this feature? Investigated the existing code — neither native mode nor ACP mode handles DMs today; `trigger.go:7` explicitly flags NIP-17 gift-wrap DM support (kind 1059) as deferred/out of scope. Decision: keep DMs out of scope for this feature too; documented as a Non-Goal in `spec.md` and the source PRD. DM listening is candidate future-PRD work.

## References

- Source PRD: [boabot-native-daemon-mode-PRD.md](./boabot-native-daemon-mode-PRD.md)
- `spec.md` in this directory
