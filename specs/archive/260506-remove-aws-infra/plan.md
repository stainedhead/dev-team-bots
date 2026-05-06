# Plan: Remove AWS Infrastructure — Local Single-Binary Runtime

**Feature:** remove-aws-infra
**Created:** 2026-05-06
**Status:** Planning

---

## Development Approach

TDD throughout. Each new adapter is written test-first: failing interface compliance test → minimal implementation → refactor. Milestones are ordered so that each can be merged independently — early milestones (M1–M3) are pure additions with no deletions, making them low-risk. Deletions happen last (M7) after all replacements are verified.

---

## Phase Breakdown

### M1 — Local Infrastructure Adapters (Low Risk)
New packages: `local/queue`, `local/bus`, `local/fs`, `local/budget`. Pure additions — existing AWS adapters still compile. Wire up in tests only; no change to `main.go` yet.

### M2 — Anthropic Provider
New package: `infrastructure/anthropic`. Mapped to existing `domain.ModelProvider`. Integration test gated by `ANTHROPIC_API_KEY` env var.

### M3 — Vector Store + Embedder
New packages: `local/vector`, BM25 embedder. Verify cosine search < 100ms at 100k vectors in benchmark test.

### M4 — TeamManager + main.go Rewire
New packages: `application/team` (TeamManager, BotRegistry). Modify `cmd/boabot/main.go`. This is the integration milestone — wires all M1–M3 adapters together.

### M5 — GitHub Backup
New packages: `infrastructure/github/backup`, `application/backup`. New `boabotctl memory` subcommands. Integration test gated by `BOABOT_BACKUP_TOKEN`.

### M6 — Config Schema + Credentials + Watchdog
Config struct changes, INI credentials file parser, `local/watchdog`. Startup validation for credentials file permissions.

### M7 — Remove AWS + CDK + Docs
Delete AWS packages and tests. Delete `boabot-team/cdk/`. Update all docs. Final quality pass.

---

## Critical Path

M1 → M4 (TeamManager depends on M1 adapters)
M2 → M4 (Anthropic provider must exist before wiring)
M3 → M4 (vector store wired in TeamManager)
M4 → M7 (deletion safe only after wiring verified)
M5 → M7 (backup wired before deletion)
M6 → M7 (config schema change before AWS section removal)

---

## Testing Strategy

- Unit tests for every new adapter, using interface compliance pattern.
- Integration test for `infrastructure/anthropic` — gated by `ANTHROPIC_API_KEY`.
- Integration test for `infrastructure/github/backup` — gated by `BOABOT_BACKUP_TOKEN`.
- Benchmark test for `local/vector` cosine search at 100k vectors.
- End-to-end smoke test: `go run ./cmd/boabot` with a minimal `team.yaml` and no AWS env vars.
- Race detector on all tests (`go test -race`).
- Coverage ≥ 90% enforced.

---

## Rollout Strategy

Feature branch `feat/remove-aws-infra`. Each milestone committed separately. PR opened after M7. No deployment changes required — this is a local-first binary.

---

## Success Metrics

- Binary starts with zero AWS env vars.
- All acceptance criteria in `spec.md` pass.
- Test coverage ≥ 90% across all modules.
- `go fmt`, `go vet`, `golangci-lint` clean.
