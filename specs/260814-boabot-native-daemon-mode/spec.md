# Spec: Boabot Native Daemon Mode (Multi-Agent Buzz Support)

**Created:** 2026-08-14
**Status:** Draft
**Source PRD:** [boabot-native-daemon-mode-PRD.md](./boabot-native-daemon-mode-PRD.md)

## Executive Summary

Extend `boabot`'s native daemon mode so that any `boabot-team` persona with `buzz.enabled: true` gets its own Buzz relay identity and its own `ChannelMonitor`, all running as goroutines inside one shared `boabot` process with one orchestrator web UI. Buzz-dispatched work is wired into the existing Kanban board and `DirectTask`/`Tasks` system (which it currently bypasses entirely), so multi-agent Buzz conversations are visible and manageable in the orchestrator UI live, per-persona, without cross-talk.

## Problem Statement

`boabot-team` personas currently only reach Buzz via `buzz-acp`'s ACP harness mode (`boabot -acp`), a thin stdio adapter with no orchestrator control plane — no Kanban board, no Tasks list, no REST API, no web UI. Work triggered from Buzz is invisible outside the chat itself, and ACP mode inherits an unresolved, unfixable-from-`boabot`'s-side bug on the Buzz Desktop side (`respond_to=owner-only` never actually applying despite repeated config changes and restarts).

For a demo showing the orchestrator UI live-updating in response to Buzz-sent commands — and, more broadly, to let any configured `boabot-team` persona be reachable from Buzz, individually or together in multi-agent conversations — personas need to run in `boabot`'s native daemon mode instead: each with its own Buzz relay identity and its own `ChannelMonitor`, all sharing one orchestrator web UI. Today's native-mode wiring only supports a single, process-wide Buzz identity (`cfg.Buzz`, one `buildBuzzMonitor` call), and even that single path bypasses the orchestrator's own Kanban board and Tasks/`DirectTask` system entirely — a Buzz-dispatched task goes straight to `Worker.Execute` with no persisted, UI-visible record at all.

## Goals

- Extend native mode so that **any** `boabot-team` persona with `buzz.enabled: true` and a provisioned key gets its own Buzz identity and its own `ChannelMonitor`, running as a goroutine in **one** shared `boabot` process (`team.yaml`-driven), not one process per persona.
- Multiple Buzz-enabled personas can independently participate in the same Buzz channel/thread at once — true multi-agent conversations, not just single-agent dispatch.
- Buzz-triggered work is fully visible and functional in the orchestrator web UI **at the same time** it's happening in Buzz: a real Kanban board item, and a real Task (immediate or scheduled), tagged to the correct persona.
- The wiring is persona-agnostic — driven by each persona's own config, not hardcoded to any one bot — so this generalizes beyond whichever persona is used for the initial demo.

## Non-Goals

- Not running every persona in native mode as a hard requirement of this work — the mechanism must support any configured persona, but only the persona(s) actually enabled for Buzz need to be live for the demo.
- Not fixing Buzz Desktop's `respond_to` bug upstream — native mode sidesteps it entirely by connecting directly as a raw Nostr identity, bypassing Buzz Desktop's own agent-registration/harness system altogether.
- Not actively removing ACP mode from the codebase as part of this work — it simply won't be the active integration path for any persona once this ships; full removal, if ever wanted, is a separate future cleanup PRD.
- Not adding new orchestrator UI screens or widgets — the Kanban board and Tasks list already exist; this only makes them populate from Buzz-triggered work.
- Not building a general "board/task item per inbound message" feature for every `ChannelMonitor` type (e.g. Slack) — scoped specifically to the Buzz path.
- Not implementing Buzz DM (direct message) listening — today neither native mode nor ACP mode subscribes to or processes DM events (native mode's `ChannelMonitor` only handles NIP-29 group/channel messages, kind 9; DM support via NIP-17 gift-wrap, kind 1059, is explicitly flagged as deferred in the existing code, `trigger.go:7`). This spec is scoped to channel/group @-mention multi-agent dispatch only; DM listening is a separate, future PRD (its own trigger classification, decrypt path, and dispatch wiring).

## User Requirements / Functional Requirements

**FR-001:** `main.go`'s Buzz wiring is extended from a single, process-wide `cfg.Buzz` block to a per-bot list — any `boabot-team` persona's own `config.yaml` carrying a `buzz:` block with `enabled: true` gets its own `buzzinfra.Monitor` instance, registered on the shared `TeamManager` via its own `WithChannelMonitor(...)` call.

**FR-002:** Each Buzz-enabled persona has its own dedicated `buzz_private_key` secret (`boabotctl secret set buzz_private_key --bot <name>`), giving it a distinct Buzz identity/pubkey, resolved the same way `buildBuzzMonitor` already resolves the single-identity case today.

**FR-003:** All Buzz-enabled personas run as goroutines in one `boabot` process (native `team.yaml` mode), sharing one orchestrator web UI/API/board (`orchestrator.api_port`) — not one process per persona.

**FR-004:** Multiple Buzz-enabled personas can independently join and respond within the same Buzz channel/thread, each under its own identity, without cross-talk (persona A's mention dispatches only to persona A).

**FR-005:** When any Buzz-enabled persona's `ChannelMonitor` dispatches a task, it is represented as a real `DirectTask` (via the same `Dispatcher`/`DirectTaskStore` path the orchestrator's own web-UI chat interface uses, likely a new `DirectTaskSourceBuzz` or reuse of `DirectTaskSourceChat`) **and** creates a corresponding Kanban board item — both visible in the UI, both updating as the task progresses and completing when it finishes, for every active persona, concurrently.

**FR-006:** Concurrent activity from multiple personas' `ChannelMonitor`s does not block, drop, or cross-interleave UI updates — persona A's task/board update completes and renders correctly regardless of how many other personas are dispatching at the same time.

**FR-007:** A Buzz request phrased as a recurring/scheduled instruction (e.g. "run this every day at 9am") creates a real `Schedule`/`RecurrenceRule`-backed task via `DispatchWithSchedule`, not just one-off immediate execution.

**FR-008:** A Buzz request can update or cancel a previously-created task (immediate or scheduled), reflected in the Tasks UI.

## Non-Functional Requirements

- **Performance:** Board/Task UI updates driven by a Buzz event appear within the same latency the UI already provides for operator-driven updates — no new lag introduced by routing through the Buzz path.
- **Reliability:** Each per-bot Buzz `ChannelMonitor` failure (bad/missing key, connection drop) is isolated — it must not crash the shared `TeamManager` process or block any other persona's monitor, board, or UI, extending the existing single-monitor failure-isolation pattern (FR-036 in the current codebase) to the new multi-monitor case.
- **Security:** Each persona's `buzz_private_key` stays in the OS keystore via the existing `SecretStore`/`boabotctl secret` mechanism — never logged, never cross-referenced into another persona's config, memory, or task/board records.
- **Observability:** Every Buzz-originated `DirectTask`/board item is tagged with the persona/bot it belongs to (the UI already displays `bot_name` on tasks) — multi-agent conversations must be traceable per-agent in both logs and the UI, not lumped together.

## System Architecture

- **Affected layers:** `boabot/cmd/boabot` (wiring), `internal/infrastructure/buzzinfra` (monitor construction, now per-bot instead of singleton), `internal/application` (Dispatcher/DirectTaskStore integration for the Buzz-originated task path), `internal/domain` (possible new `DirectTaskSourceBuzz` value).
- **New/modified components:**
  - Per-bot Buzz monitor construction loop replacing the single `buildBuzzMonitor` call in `main.go`.
  - Buzz → `Dispatcher`/`DirectTaskStore` bridge (new or reused from the existing web-UI chat dispatch path).
  - Buzz → Kanban `BoardStore` bridge for task-progress/completion reflection.
  - Buzz → `DispatchWithSchedule` bridge for recurring/scheduled requests parsed from natural language.
- **Out of scope architecturally:** ACP harness code path (`internal/infrastructure/acp`) — untouched.

## Scope of Changes

- Files to create: TBD during Step 3 research (bridge/adapter files for DirectTask + Board wiring from Buzz dispatch).
- Files to modify: `boabot/cmd/boabot/main.go` (Buzz wiring loop), `internal/infrastructure/buzzinfra/*` (per-bot monitor factory), `boabot-team/*/config.yaml` for any persona chosen for the demo (`buzz:` block + secret provisioning), `team.yaml` if persona enablement flags need updating.
- Dependencies: existing `Dispatcher`, `DirectTaskStore`, `BoardStore`, `SecretStore`/`boabotctl secret`, Buzz relay (`wss://feral-sysd.communities.buzz.xyz`).

## Breaking Changes

- None expected to public config schema — `buzz:` block already exists per-bot in `config.yaml`; this generalizes wiring to honor it for N bots instead of 1. `cfg.Buzz` (process-wide) usage in `main.go` is replaced/extended — confirm no other caller depends on the single-identity path before removing it (research phase).

## Success Criteria and Acceptance Criteria

- [ ] Running `boabot` in native team mode starts the orchestrator web UI/API, with the Kanban board and Tasks list both reachable and populated.
- [ ] At least two `boabot-team` personas each have their own provisioned `buzz_private_key` and their own distinct Buzz identity, both connected simultaneously from one `boabot` process.
- [ ] Mentioning persona A in a Buzz channel dispatches only to persona A; mentioning persona B dispatches only to persona B; both can be active in the same channel/thread without cross-talk.
- [ ] A Buzz-dispatched task appears in the Tasks UI (immediate), tagged with the correct `bot_name`, within the UI's existing refresh latency.
- [ ] A Buzz-dispatched task also creates a Kanban board item that updates as the task progresses and reflects completion when done.
- [ ] A Buzz request phrased as a recurring/scheduled instruction creates a real `Schedule`/`RecurrenceRule`-backed task, visible under the Tasks UI's "Scheduled" filter.
- [ ] A Buzz request can update or cancel a previously-created task (immediate or scheduled), reflected in the Tasks UI.
- [ ] If one persona's Buzz identity fails to connect, other personas' Buzz monitors, the orchestrator UI, and the rest of the process continue unaffected.
- [ ] `boabot -acp` still builds and passes its existing tests, untouched by this work.
- [ ] No `buzz_private_key` value appears in logs, board items, task records, or committed config for any persona.

**Quality gates:** `go fmt`, `go vet`, `golangci-lint run`, `go test -race ./...` all clean; ≥90% coverage on `internal/domain` and `internal/application` per AGENTS.md.

## Risks and Mitigation

| Item | Type | Notes | Mitigation |
|------|------|-------|------------|
| Buzz relay (`wss://feral-sysd.communities.buzz.xyz`) | Dependency | External network dependency for every persona's `ChannelMonitor`. | Existing single-monitor reconnect/isolation pattern extended per-bot. |
| `boabotctl secret set` / OS keystore | Dependency | Required to provision each persona's `buzz_private_key` before its monitor can start. | Document provisioning step in user-docs; verify manually before demo. |
| Existing `DirectTaskStore`/`Dispatcher`/`BoardStore` | Dependency | This work extends, not replaces, existing infrastructure. | Reuse existing interfaces; no new stores. |
| Concurrent relay connections per machine | Risk | Multiple personas each opening their own relay connection from one process is unverified at scale beyond a couple of bots. | Test with 2+ personas; document any relay-side limits found. |
| `tasks.json`/board-store JSON files | Risk | Both are single shared JSON-file-backed stores — concurrent writes from multiple personas' monitors need verification for race safety. | `-race` test coverage for concurrent dispatch; add locking if a race is found. |
| Natural-language → `Schedule`/`RecurrenceRule` parsing | Risk | Relies on the model correctly translating "every day at 9am" into a structured schedule via tool call — same class of risk as ADR-B027. | Validate against a fixed set of phrasing test cases during implementation. |
| Second demo persona selection & secret provisioning | Team dependency | Repo owner/operator must pick the second Buzz-enabled persona and provision its secret before multi-agent acceptance criteria can be executed. | Resolve during Research phase — see Open Questions in research.md. |

**One-time cutover step (not an ongoing risk):** The existing ACP-managed "Boa" agent registration in Buzz Desktop must be manually stopped once native mode is verified working, so it doesn't keep responding alongside the new native-mode identity.

## Timeline and Milestones

[TBD] — driven by dev-flow phase progression (Research → Data Modeling → Architecture → Task Breakdown → Implementation), tracked in `status.md`.

## References

- Source PRD: [boabot-native-daemon-mode-PRD.md](./boabot-native-daemon-mode-PRD.md)
- Reliability pattern precedent: FR-036 (existing single-monitor failure isolation)
- Related prior fix: ADR-B027 (`internal/infrastructure/acp` fallback-publish issue) — same class of risk for NL→schedule parsing reliability
