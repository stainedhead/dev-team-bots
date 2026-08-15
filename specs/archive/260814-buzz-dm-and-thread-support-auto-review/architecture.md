# Architecture: Buzz DM/Thread Support — Code Review Fixes

**Created:** 2026-08-15
**Status:** Draft

## Architecture Overview

No architectural changes. All 6 findings are localized fixes/corrections within the architecture the original feature already established.

## Component Architecture

Unchanged. Fixes touch: `team_manager.go`/`server.go` (FR-301), `BuzzTaskBridge` (FR-302, FR-306), `dm.go` (FR-303), `monitor.go`/`main.go` (FR-304), `monitor.go`'s `publishReply` (FR-305), plus documentation-only touches to the archived original spec.

## Layer Responsibilities

Unchanged from the original feature.

## Data Flow

Unchanged in shape. FR-301's fix (if option a) changes what value flows into `ChatStore.ThreadID` for Buzz tasks; FR-304 adds a log side-effect at DM-monitor startup.

## Sequence Diagrams

Not applicable.

## Integration Points

Unchanged.

## Architectural Decisions

- **FR-301: prefer option (a) (pass real `ThreadID` through) over option (b) (filter at read time), pending research confirmation.** Fixing the write side is more correct than filtering at every read site (`handleChatList` and any future `ListByBot`-style caller would otherwise each need the same exclusion logic). Final call deferred to implementation after confirming the failure-mode handling the review flagged.
- **FR-304: mirror the existing `LockDir`-empty warning exactly.** Consistency with an established pattern beats inventing new logging conventions.
