# PRD: remove-aws-infra Auto Code Review Fixes

**Created:** 2026-05-06
**Jira:** N/A
**Status:** Implemented
**Branch:** feat/remove-aws-infra
**Source review:** Step 5 — Code and Design Review of `feat/remove-aws-infra`

## Executive Summary

The `feat/remove-aws-infra` implementation is architecturally sound with clean layer boundaries, strong test coverage (≥90% on all new packages), and correct credential security. The automated code review identified three Must Fix findings and one Warning. All four have been resolved in commit `9bc4bd8`. This PRD documents the findings and their acceptance criteria for traceability.

## Functional Requirements

**FR-R001 (P0):** `config.Load()` must reject unknown YAML fields — including `aws:` — at parse time with a clear error message containing the field name.

Acceptance criteria:
- [ ] `config.Load()` calls `dec.KnownFields(true)` before decoding.
- [ ] A config file containing `aws: {region: us-east-1}` returns a non-nil error whose message contains `"aws"`.
- [ ] A config file with any unknown top-level field returns a non-nil error.
- [ ] All existing config tests continue to pass (they use only known fields).
- [ ] `internal/infrastructure/config` coverage remains 100%.

**FR-R002 (P0):** When `backup.enabled: true`, `startBot` must wire `GitHubBackup` and `ScheduledBackupUseCase` and start the backup goroutine. When `backup.restore_on_empty: true` and the memory directory is absent or empty, `startBot` must call `Restore()` before initialising any other adapter (FS, vector, budget).

Acceptance criteria:
- [ ] `startBot` instantiates `githubbackup.GitHubBackup` from `botCfg.Backup` when `Enabled: true`.
- [ ] `isDirEmpty` helper returns true for absent or empty directories.
- [ ] Restore is called before `fs.New()`, `vector.New()`, and `budget.New()` so restored files are visible on first load.
- [ ] When `Restore()` fails, `startBot` returns a wrapped error (not a panic); `TeamManager` logs and restarts the bot.
- [ ] `ScheduledBackupUseCase` goroutine is started with a derived context that is cancelled when `startBot` returns.
- [ ] `TestStartBot_RestoreOnEmptyFails` passes — bot repeatedly fails restore and is restarted; manager does not crash.

**FR-R003 (P0):** When `memory.embedder` is set to a non-`bm25` value (e.g. `openai`) and the OpenAI embedder adapter is not yet implemented, `startBot` must log a `slog.Warn` naming the configured embedder and indicating it is falling back to BM25. Silent fallback without any log line is not acceptable.

Acceptance criteria:
- [ ] `startBot` emits `slog.Warn` with fields `"bot"` and `"embedder"` when `botCfg.Memory.Embedder` is non-empty and not `"bm25"`.
- [ ] The `bm25.DefaultEmbedder()` is still used as the fallback.
- [ ] `validateEmbedderProvider` continues to reject provider types that do not support embeddings (`anthropic`, `bedrock`).

**FR-R004 (P1):** The stray binary `boabot/boabot` (produced by running `go build` in the module root without `-o bin/boabot`) must be deleted and its pattern added to `.gitignore` so it cannot be accidentally committed.

Acceptance criteria:
- [ ] `boabot/boabot` does not exist in the working tree.
- [ ] `.gitignore` contains `boabot/boabot` and `boabotctl/boabotctl`.
- [ ] `git status` reports no untracked binaries at those paths.

## Non-Functional Requirements

- **TDD:** Each P0 fix implemented test-first (failing test → implementation → passing test).
- **Coverage:** `internal/infrastructure/config` ≥ 100%; `internal/application/team` ≥ 90%.
- **Lint:** `go fmt`, `go vet`, `golangci-lint` pass with 0 issues after all fixes.
- **No regressions:** Full test suite (`go test -race ./...`) passes.

## Dependencies and Risks

| Item | Type | Notes |
|---|---|---|
| `gopkg.in/yaml.v3` `KnownFields(true)` | Dependency | Available since yaml.v3; no new imports needed |
| `TestStartBot_RestoreOnEmptyFails` network call | Risk | Uses `localhost:1` which fails immediately; test is fast but requires TCP stack |

## Open Questions

None — all findings resolved.
