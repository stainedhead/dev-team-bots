# Data Dictionary: BaoBot ACP Stdio Harness Support

**Created:** 2026-08-13

## Purpose

Defines the domain entities, value objects, interfaces, and protocol message types introduced by BaoBot's ACP stdio harness mode.

## Entities

`[TBD]` — pending architecture.md. Likely candidates: an ACP session type wrapping a single persona's `Worker` invocation lifecycle.

## Value Objects

`[TBD]`

## Interfaces

`[TBD]` — likely a narrow domain-layer seam distinct from `ChannelMonitor`/`MessageQueue` (those model an async multi-bot queue; ACP mode is synchronous, single-persona, single-session). Exact shape pending architecture.md and research into `buzz-acp`'s per-turn/per-session behavior.

## Enumerations

`[TBD]` — e.g. ACP method/notification kinds, once confirmed against the protocol spec and `buzz-acp`'s actual usage.

## API Request/Response Types

`[TBD]` — ACP JSON-RPC request/response/notification payload shapes (initialize, session/new, session/prompt, session/update, session/cancel or equivalent), to be captured once confirmed during Phase 2 research.
