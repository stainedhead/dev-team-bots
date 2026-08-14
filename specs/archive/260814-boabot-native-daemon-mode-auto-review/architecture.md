# Architecture: Boabot Native Daemon Mode — Code Review Fixes

**Created:** 2026-08-14
**Status:** Draft

## Architecture Overview

No architectural changes. All 10 findings are localized fixes/corrections within the architecture the original feature already established (see `specs/archive/260814-boabot-native-daemon-mode/architecture.md`).

## Component Architecture

Unchanged. Fixes touch:
- `BuzzTaskBridge` (application layer) — FR-101, FR-102, FR-110.
- `TeamManager` (application layer) — FR-103, FR-107.
- Documentation only — FR-104, FR-105, FR-106, FR-108, FR-109.

## Layer Responsibilities

Unchanged from the original feature — no layer boundaries are touched or reconsidered by these fixes.

## Data Flow

Unchanged. FR-101's fix changes *when* within the existing dispatch flow the dedup state is recorded (after success instead of before attempt), not the flow's shape.

## Sequence Diagrams

Not applicable — no new flows.

## Integration Points

Unchanged.

## Architectural Decisions

- **FR-107 (recommended): revert `LoadTeamConfig` to unexported.** The original feature's architecture.md rationale for exporting it ("main.go doesn't duplicate YAML parsing") no longer holds — the final design moved monitor construction inside `TeamManager.Run()` instead, so `main.go` never calls it. Reverting removes unnecessary public API surface. (Alternative, not recommended: keep exported and update the rationale — acceptable if a future caller outside the package is anticipated, but none is known today.)
- **FR-106 (recommended): clarify coverage-gate framing in docs, don't chase a coverage number.** CI already enforces an aggregate gate across `internal/domain/...`+`internal/application/...`, currently 91.2%, comfortably over 90%. `internal/application/team`'s 78.9% (up from a pre-feature 77.8%, not a regression) only looks like a violation against README's per-package table framing — the framing is the actual gap, not the coverage.
