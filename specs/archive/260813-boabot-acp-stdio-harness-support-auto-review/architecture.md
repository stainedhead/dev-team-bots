# Architecture: BaoBot ACP Stdio Harness Support — Auto-Review Fixes

**Created:** 2026-08-13
**Status:** Draft

## Architecture Overview

No architectural change to the overall design in `specs/archive/260813-boabot-acp-stdio-harness-support/architecture.md` — this cycle fixes bugs within that design (concurrency correctness in `internal/infrastructure/acp`, a missing construction step in `cmd/boabot/acp.go`, missing logging, hygiene items).

## Component Architecture

Unchanged from the original feature — see the archived architecture.md for the full component diagram.

## Layer Responsibilities

Unchanged — all fixes stay within `internal/infrastructure/acp` and `cmd/boabot`, no domain or application layer changes anticipated (FR-001's fix, if it touches `execute_task.go`, is fixing an existing application-layer bug — synchronizing `progressFn` access — not adding a new interface).

## Data Flow

Unchanged.

## Architectural Decisions

- **AD-1 (this cycle):** FR-001's fix approach — see research.md's open question. To be recorded here once decided during implementation, along with the rationale.
