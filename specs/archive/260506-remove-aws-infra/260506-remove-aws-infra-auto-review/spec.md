# Feature Spec: remove-aws-infra Auto Code Review Fixes

**Spec dir:** specs/260506-remove-aws-infra-auto-review/
**Source PRD:** specs/260506-remove-aws-infra-auto-review/remove-aws-infra-auto-review-PRD.md
**Created:** 2026-05-06
**Status:** Complete

---

## Executive Summary

Four findings from the automated code review of `feat/remove-aws-infra` required resolution: strict YAML config parsing (AC-15 compliance), wiring of the GitHub backup adapter and scheduled use case into `startBot`, a clear log warning when a non-bm25 embedder is configured but not yet implemented, and removal of a stray binary from the working tree. All fixes were implemented TDD-first in commit `9bc4bd8` on the same branch.

---

## Problem Statement

The code review of `feat/remove-aws-infra` identified three Must Fix gaps (P0) and one Warning (P1):

1. `config.Load()` silently ignored unknown YAML fields including the removed `aws:` block, violating AC-15.
2. `GitHubBackup` and `ScheduledBackupUseCase` were implemented but never instantiated or started — backup and restore-on-empty were completely inert at runtime.
3. When `memory.embedder` was configured to a non-bm25 value, `startBot` silently fell back to BM25 with no log line.
4. A stray `boabot/boabot` binary was untracked by git and not covered by `.gitignore`.

---

## Goals

1. Enforce strict YAML schema at parse time so `aws:` and any unknown fields cause an immediate, named error.
2. Wire `GitHubBackup` and `ScheduledBackupUseCase` into `startBot` so backup and restore-on-empty function at runtime.
3. Emit a `slog.Warn` when a non-bm25 embedder provider is configured but the adapter is not yet implemented.
4. Remove stray binary and prevent recurrence via `.gitignore`.

## Non-Goals

- Implementing the OpenAI embedder adapter (deferred; FR-R003 only adds the warning).
- Changing any domain interface or agent runtime loop.
- Adding new boabotctl commands beyond what is already implemented.

---

## Functional Requirements

**FR-001:** `config.Load()` must call `dec.KnownFields(true)` so unknown YAML fields return a parse-time error whose message contains the unknown field name.

**FR-002:** When `backup.enabled: true`, `startBot` must instantiate `githubbackup.GitHubBackup` from bot config, call `Restore()` before all other adapter initialisation when `restore_on_empty: true` and the memory directory is empty, and start `ScheduledBackupUseCase` in a goroutine with a derived context.

**FR-003:** When `botCfg.Memory.Embedder` is non-empty and not `"bm25"`, `startBot` must emit `slog.Warn` with fields `"bot"` and `"embedder"` before falling back to `bm25.DefaultEmbedder()`.

**FR-004:** The file `boabot/boabot` must be deleted and `.gitignore` updated with patterns `boabot/boabot` and `boabotctl/boabotctl`.

---

## Non-Functional Requirements

| Category | Requirement |
|---|---|
| TDD | Each P0 fix implemented with failing test first |
| Coverage | `internal/infrastructure/config` ≥ 100%; `internal/application/team` ≥ 90% |
| Lint | `go fmt`, `go vet`, `golangci-lint` pass with 0 issues |
| Regression | `go test -race ./...` passes in full |

---

## System Architecture

### Affected Files

| File | Change |
|---|---|
| `internal/infrastructure/config/config.go` | Add `dec.KnownFields(true)` |
| `internal/infrastructure/config/config_test.go` | Add `TestLoad_AWSBlockRejected`, `TestLoad_UnknownFieldRejected` |
| `internal/application/team/team_manager.go` | Wire backup in `startBot`; add `isDirEmpty`; add embedder warn |
| `internal/application/team/start_bot_test.go` | Add `TestStartBot_RestoreOnEmptyFails` |
| `.gitignore` | Add binary patterns |
| `boabot/boabot` | Delete |

---

## Success Criteria and Acceptance Criteria

- [ ] `config.Load()` rejects a config containing `aws:` with an error message containing `"aws"`.
- [ ] `config.Load()` rejects any unknown top-level field.
- [ ] All existing config tests continue to pass.
- [ ] `startBot` wires `GitHubBackup` when `backup.enabled: true`.
- [ ] `isDirEmpty` returns true for absent or empty directories.
- [ ] Restore is called before `fs.New()`, `vector.New()`, `budget.New()`.
- [ ] Failed `Restore()` causes `startBot` to return a wrapped error; manager restarts the bot.
- [ ] `ScheduledBackupUseCase` goroutine starts with a derived context cancelled on `startBot` return.
- [ ] `TestStartBot_RestoreOnEmptyFails` passes.
- [ ] `startBot` emits `slog.Warn` with `"bot"` and `"embedder"` fields when a non-bm25 embedder is configured.
- [ ] `boabot/boabot` does not exist in working tree.
- [ ] `.gitignore` contains `boabot/boabot` and `boabotctl/boabotctl`.
- [ ] `go fmt`, `go vet`, `golangci-lint` pass with 0 issues.
- [ ] `internal/infrastructure/config` coverage = 100%; `internal/application/team` ≥ 90%.

---

## References

- Source PRD: `specs/260506-remove-aws-infra-auto-review/remove-aws-infra-auto-review-PRD.md`
- Original feature spec: `specs/archive/260506-remove-aws-infra/`
- Implementing commit: `9bc4bd8`
