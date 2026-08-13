# Data Dictionary: BaoBot ACP Stdio Harness Support — Auto-Review Fixes

**Created:** 2026-08-13

## Purpose

This fix cycle introduces no new domain entities, value objects, or protocol types — it corrects concurrency/behavior bugs and hygiene issues in the existing `internal/infrastructure/acp` types documented in `specs/archive/260813-boabot-acp-stdio-harness-support/data-dictionary.md`. Refer there for the full type inventory; this file exists only to satisfy the standard spec template.

## New/Changed Types

- Possibly a new field on `session` (`session.go`) if FR-005's fix needs per-turn cancel tracking rather than a single `cancel context.CancelFunc` — exact shape depends on which fix is chosen during implementation.
- Possibly a new field/type if FR-001's fix is session-scoped progress routing (option (c) in the review PRD's Open Questions) — e.g. `map[sdk.SessionId]chan string` on `Agent`.
