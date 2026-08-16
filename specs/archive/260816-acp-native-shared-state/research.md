# Research: ACP/Native Shared State and Task-Layer Parity

**Created:** 2026-08-16
**Source PRD:** [acp-native-shared-state-PRD.md](./acp-native-shared-state-PRD.md)

## Research Questions

1. **FR-501:** What is the minimal, explicit config mechanism to make shared-state a validated fact rather than an inferred coincidence? Should this be a single new field (e.g. `shared_state.root`) on both native mode's top-level config and each ACP persona's config, checked for equality at startup? What happens today, concretely, if native mode's `memory.path` and an ACP persona's resolved `memRoot` diverge — does anything currently notice?
2. **FR-503:** What is the exact shape of `ChatStore` (`internal/infrastructure/local/orchestrator` or wherever it lives) and the `handleChatSend`/`BuzzTaskBridge` history-replay pattern used by native mode? Can `turn.go`'s `Prompt` handler reuse the identical replay logic, or does ACP's single-shot per-conversation model (one worker pool entry per Buzz agent slot) require a different conversation-ID scheme to look up the right history?
3. **FR-504 (central question):** Does ACP mode need a full `Dispatcher`/`DirectTaskStore` layer, or can scheduling be handled as a narrower pre-check using `ChatTaskManager.DetectAndHandle` before falling through to the existing synchronous `worker.Execute` path? What does `DetectAndHandle`'s actual signature/return type look like, and does it already return something that can drive a synchronous ACP confirmation reply without invoking the async dispatcher?
4. **FR-504a:** How does native-mode Buzz create a `DirectTask`/board item for every dispatched task today (which function, at what point in the flow)? Can the identical call be added to ACP mode's `turn.go` `Prompt` handler around the `worker.Execute` call, using the board store already wired by the prior ACP-parity feature?
5. **FR-505:** What does native mode's `watchdog.New(...)` wiring look like exactly (`team_manager.go` lines ~582-584, per prior session notes)? Is it a drop-in constructor call given `heap_warn_mb`/`heap_hard_mb` from a bot's config, or does it depend on multi-bot `TeamManager` state that ACP mode's single-persona process would need to stub out?

## Findings

(To be populated as research proceeds — Step 1 seeds questions only, per the standard spec-creation template.)

## Industry Standards

[TBD]

## Existing Implementations (this repo)

- Board-store concurrency fix: `boabot/internal/infrastructure/local/orchestrator/board.go`'s `persist()` + `boabot/internal/infrastructure/local/filelock/` — direct precedent for FR-502/NFR data-safety requirement.
- ACP-parity wiring precedent: `boabot/cmd/boabot/acp.go`'s `buildACPMCPOptions`/`buildACPWorker` — direct precedent for how to gate new ACP-mode wiring on config presence and log activation status (NFR-Observability requirement).

## Best Practices

[TBD]

## Open Questions

- See PRD's Open Questions section (FR-504's integration shape, FR-501's field naming) — carried forward as Research Questions 1 and 3 above.

## References

- `acp-native-shared-state-PRD.md` (this directory)
- `specs/archive/260815-acp-harness-feature-parity/` — prior feature, direct precedent for wiring pattern and P0 concurrency fix
