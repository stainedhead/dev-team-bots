# Architecture: Boabot Native Daemon Mode (Multi-Agent Buzz Support)

**Created:** 2026-08-14
**Status:** Draft

## Architecture Overview

[TBD — to be filled in Phase 3, after research confirms current `buildBuzzMonitor`/`Dispatcher`/`DirectTaskStore` wiring shape.]

## Component Architecture

- Per-bot Buzz monitor factory (replacing single `buildBuzzMonitor` call in `main.go`)
- Buzz → `Dispatcher`/`DirectTaskStore` bridge
- Buzz → `BoardStore` bridge
- Buzz → `DispatchWithSchedule` bridge for recurring requests

## Layer Responsibilities

- **Domain:** `DirectTaskSource` value (possible new `DirectTaskSourceBuzz`), any new domain events for Buzz-originated dispatch.
- **Application:** Use case(s) orchestrating Buzz `ChannelMonitor` output → `Dispatcher` → `DirectTaskStore` + `BoardStore`.
- **Infrastructure:** `buzzinfra` per-bot monitor construction; no new external adapters expected beyond the existing Buzz/Nostr client.

## Data Flow

[TBD — sequence: Buzz message received by persona's `ChannelMonitor` → dispatch decision (immediate vs. scheduled) → `Dispatcher` call → `DirectTaskStore` write + `BoardStore` write → orchestrator UI reflects update via existing refresh/poll mechanism.]

## Sequence Diagrams

[TBD]

## Integration Points

- Buzz relay (`wss://feral-sysd.communities.buzz.xyz`) — one connection per Buzz-enabled persona.
- `SecretStore`/`boabotctl secret` — per-bot `buzz_private_key` resolution.
- Existing orchestrator web UI/API — no new endpoints expected; existing Tasks/Board endpoints should already surface Buzz-originated records once they're written through the same stores.

## Architectural Decisions

[TBD — record here as decisions are made during Phase 3, e.g. the `DirectTaskSource` naming decision from Research Question 1.]
