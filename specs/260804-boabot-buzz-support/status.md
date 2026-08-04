# Status: BaoBot Buzz Support

**Feature:** boabot-buzz-support
**Created:** 2026-08-04
**Last Updated:** 2026-08-04

---

## Overall Progress

| Phase | Description | Status |
|---|---|---|
| Phase 0 | Initial Research (PRD) | ✅ Complete |
| Phase 1 | Specification (spec.md) | ✅ Complete |
| Phase 2 | Research & Data Modeling | ✅ Complete |
| Phase 3 | Architecture & Planning | ✅ Complete |
| Phase 4 | Task Breakdown | ✅ Complete (57 tasks across Phases A–I, `tasks.md`; all 54 FRs mapped) |
| Phase 5 | Implementation | In Progress (Phases A, B, C of A–I complete; see checklist below) |
| Phase 6 | Completion & Archival | Not Started |

## Phase 0 Task Checklist

- [x] Spec directory created (`specs/260804-boabot-buzz-support/`)
- [x] Research questions identified (see `research.md`)
- [x] Phase files initialized (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md)
- [x] PRD moved into spec directory

## Phase 5 Task Checklist (tasks.md, Phases A–I)

### Phase A — `TeamManager` seam fix
- [x] A1 — `monitors []domain.ChannelMonitor` added to `TeamManager`; registration setter appends to it instead of setting a dedicated `slackMonitor` field. FR-033.
- [x] A2 — Both result-forwarding call sites (orchestrator-mode and non-orchestrator-mode paths) rewritten to iterate `tm.monitors` via a new `forwardResultToMonitors` helper, calling `HandleResult` on each; the `if tm.slackMonitor != nil` branches are gone. FR-034.
- [x] Follow-up fix — `WithSlackMonitor(m *slackinfra.Monitor)` renamed to `WithChannelMonitor(m domain.ChannelMonitor)`, removing `internal/infrastructure/slack` from `team_manager.go`'s imports entirely. `grep -r "infrastructure/slack|infrastructure/buzz" internal/application` now returns **no matches** — FR-034's literal AC is satisfied, not just its spirit. `cmd/boabot/main.go`'s one call site and one internal test updated to match; `slackinfra.Monitor` already satisfied `domain.ChannelMonitor` per Phase A's compile-time assertion, so this was a pure rename with no behavior change. Commit `4dd772b`.

The Phase A brief had explicitly deferred this to a later phase, reasoning the AC as literally written ("`internal/application` imports no infrastructure package," full stop) was unachievable regardless since the package legitimately imports config/cliagent/github-backup/http/local-*/openai for other reasons. That broader reading was right, but the *narrower*, actually-load-bearing AC — the FR-034 grep scoped specifically to `infrastructure/slack|infrastructure/buzz` — was a small, achievable fix that didn't need to wait for Phase H's wiring work. Fixed immediately rather than carried as risk into Phase I's quality gate.

### Phase B — Secret storage domain + providers
- [x] B1 *(spike)* — `zalando/go-keyring@v0.2.8` confirmed as latest; `Get`/`Set`/`Delete`/`DeleteAll` signatures and `ErrNotFound`/`ErrSetDataTooBig` sentinels confirmed by reading the module cache. Darwin backend confirmed to write via `security -i` + stdin pipe, never argv. systemd `$CREDENTIALS_DIRECTORY` semantics confirmed via `https://systemd.io/CREDENTIALS/` (one file per credential ID, kernel-enforced per-user access); trailing-newline handling is undocumented upstream, resolved as a defensive strip. Full findings in `implementation-notes.md`.
- [x] B2 — `SecretRef`/`SecretProvider`/`SecretStore` defined in `internal/domain/secret.go`; zero infrastructure imports (only `context`). FR-038.
- [x] B3 — `Store` (in `internal/infrastructure/secret/store.go`) implements ordered-chain resolution: first-hit-wins, per-provider `context.WithTimeout` (default 2s, enforced via goroutine+select so a non-cooperative provider still times out), provider errors non-halting, configurable order via constructor slice, any provider omissible. Ordered four-provider precedence test (env > systemd > keystore > file) included. FR-039, FR-040.
- [x] B4 — `internal/infrastructure/secret/env/`: env-var provider, ignores `Bot` per FR-044.
- [x] B5 — `internal/infrastructure/secret/file/`: wraps `credentials.Load` unchanged; world-readable check remains fatal, exact message preserved.  FR-043.
- [x] B6 — `internal/infrastructure/secret/systemd/`: reads `$CREDENTIALS_DIRECTORY/<name>`; inert (miss, no error) when unset. FR-042.
- [x] B7 — `internal/infrastructure/secret/keystore/`: wraps `zalando/go-keyring`; writes never reach a subprocess argument list — verified two ways: library-source reading (B1 spike) plus a call-inspection test asserting the secret only ever lands in `Set`'s dedicated `password` parameter, never `service`/`user`, via a recording fake `backend`; a source-grep test complements both. Tests use the library's own `MockInit`/`MockInitWithError` seam, restored via `t.Cleanup` after each use (that seam is process-global state). FR-041 (unit-testable portion), FR-052.
- [x] B8 — Per-bot namespacing threaded through all four providers, keyed on bot **name** (OQ-9), **strict-match with no bot→global fallback** on any provider (an earlier draft had `file` fall back to the global key, caught in self-review as a risk of two bots silently sharing one secret): `env` ignores `Bot`; `file`/`systemd` use `"<bot>_<name>"`; `keystore` uses service `"boabot"` + account `"<bot>/<name>"`. Documented in each package's doc comment and in `implementation-notes.md`. FR-045.
- [x] B9 — `Store.Get`'s `NotFoundError` names the reference and enumerates every provider consulted; supports `errors.Is`/`errors.As` via `Unwrap() []error`. FR-053.
- [x] B10 — Cross-provider no-value-logging test (`internal/infrastructure/secret/no_value_logging_test.go`): captured `slog` buffer + sentinel value, exercised across all four providers' error paths and a hit path; no provider logs anything itself, only `Store` does (provider name + ref name only). FR-051 (provider half).

`go build ./...`, `go vet ./...`, `go test -race ./...`, and `golangci-lint run` all pass repo-wide (56/56 packages green). Coverage on the new `internal/domain/secret.go` and all five new `internal/infrastructure/secret/...` packages: 100% of statements. `go mod tidy` also resolved pre-existing go.mod staleness (aws-sdk-go-v2/config, google/uuid, slack-go/slack had been indirect despite direct imports landing in an earlier, un-tidied commit) alongside adding `zalando/go-keyring` (+`danieljoos/wincred`, `godbus/dbus/v5` as its indirect platform backends) as a direct dependency.

### Phase C — Secret storage callers
- [x] C1 — `cmd/boabot/main.go`'s two `applyCredential` calls migrated to `SecretStore.Get` via a new `resolveEnvCredentials`/`applyCredentialFromStore`/`buildSecretProviders` trio; default chain order (env, systemd, keystore, file) and the world-readable-credentials-file fatal guard both preserved exactly. FR-046.
- [x] C2 — `SlackConfig` gained `ResolveSecrets(ctx, store, logger)`: inline `bot_token`/`app_token` win with a deprecation warning naming the alternative; empty inline fields resolve via the store, bot-namespaced by `BotName`; empty `BotName` skips the store entirely (no wasted keystore/systemd round trip for non-Slack bots). FR-047.
- [x] C3 — FR-048's warn-only clause confirmed satisfied by C2's deprecation-warning branch; no new production code, one dedicated test added. FR-048 (warn-only portion).
- [x] C4 — `boabot --diag-secrets` flag (new `cmd/boabot/diag.go`): reports, per configured secret, which provider resolved it (name only), by building a throwaway single-provider `secret.Store` per candidate so per-provider timeout/non-halting semantics are inherited without touching Phase B's `store.go`. Sentinel test confirms the resolved value never reaches stdout or the log buffer. FR-050, FR-051 (diagnostic half).
- [x] C5 — `boabotctl secret set/get/delete` (new `boabotctl/internal/commands/secret.go`), local machine only (OQ-11); `zalando/go-keyring` added as `boabotctl`'s own direct dependency (cross-module constraint — no shared code with `boabot`, only the FR-045 key-naming convention); `set` reads via masked prompt/piped stdin, never argv; `get` reports presence/absence only, per FR-049's literal wording; `delete` on an absent entry is not an error. `user-docs/baobotctl.md` updated. FR-049.

`go build ./...`, `go vet ./...`, `go test -race ./...`, `golangci-lint run` all pass for both `boabot` and `boabotctl` (each module's own commands, plus `boabot`'s full repo-wide suite to confirm Phase A/B are unaffected). Full details in `implementation-notes.md`'s new "Phase C" section.

## Blockers

None currently. OQ-1 (multi-instance singleton) was resolved during PRD pre-flight as a process-level lock — see `implementation-notes.md`.

## Recent Activity

- 2026-08-04 — Spec directory created via `/create-spec boabot-buzz-support-PRD.md`, run as part of `/implm-frm-prd` Step 1.
- 2026-08-04 — All 8 phase files populated (spec.md, status.md, research.md, data-dictionary.md, architecture.md, plan.md, tasks.md, implementation-notes.md); PRD moved into this directory. Step 1 complete.
- 2026-08-04 — `/review-spec` (Step 2) run. Initial verdict: Needs revision — scope contradictions between spec.md/plan.md on FR-025–027 and FR-047/048, an unresolved domain `Event`/`Filter` type decision that would have failed a PRD acceptance criterion, several undesigned edge cases (reply-publish failure, provider timeout, pending-map-across-reconnect, presence-during-disconnect, allowlist nil-vs-empty), and `tasks.md` being a stub. All gaps fixed directly in spec.md, data-dictionary.md, architecture.md, plan.md, implementation-notes.md, and a full 57-task breakdown written to tasks.md. Re-review verdict: Implementation-ready.
- 2026-08-04 — Phase A (tasks A1, A2) complete: `domain.ChannelMonitor` gained `HandleResult(ctx, TaskResultPayload)`; `slack.Monitor`'s existing (error-less) `HandleResult` signature was kept as-is and the interface matched to it rather than the reverse. `TeamManager`'s dedicated `slackMonitor *slackinfra.Monitor` field was replaced with `monitors []domain.ChannelMonitor`. Both result-forwarding call sites (orchestrator and non-orchestrator paths) now loop over a closure-captured snapshot of `tm.monitors` via a new `forwardResultToMonitors` helper — no more Slack-only `if != nil` branch. `internal/application/mocks.ChannelMonitor` gained a `HandleResult` method so it keeps satisfying the widened interface; `slack.Monitor` got a `var _ domain.ChannelMonitor = (*Monitor)(nil)` compile-time assertion. TDD followed throughout, including for the follow-up fix below. FR-033, FR-034. `go build`, `go vet`, `go test -race -count=1 ./...`, and `golangci-lint run` all pass.
- 2026-08-04 — Follow-up fix (commit `4dd772b`): `WithSlackMonitor(m *slackinfra.Monitor)` → `WithChannelMonitor(m domain.ChannelMonitor)`. Closes the FR-034 grep AC fully — see the Phase A checklist entry above for why this was worth fixing immediately rather than deferring.
- 2026-08-04 — Phase B (tasks B1–B10) complete: `domain.SecretStore`/`SecretProvider`/`SecretRef` defined with zero infrastructure imports; ordered provider chain (`internal/infrastructure/secret.Store`) with per-provider timeout, non-halting errors, and FR-053 error enumeration; four providers (`env`, `file`, `systemd`, `keystore`) each with per-bot namespacing (FR-045, bot name per OQ-9) and no secret-value logging (FR-051, provider half) verified by a cross-provider test with a captured log buffer and sentinel value; `keystore` wraps `zalando/go-keyring@v0.2.8`, confirmed via B1 spike to write via `security -i`/stdin on macOS (never argv, FR-052), guarded by a mechanical no-`exec.Command` regression test. TDD followed for all ten tasks (failing test committed conceptually before each implementation, verified red before green at every step). Full details in `implementation-notes.md`'s new "Phase B" section. `go build`/`go vet`/`go test -race ./...`/`golangci-lint run` all pass repo-wide; 100% statement coverage on all new packages.
- 2026-08-04 — Phase C (tasks C1–C5) complete: `cmd/boabot/main.go`'s two `applyCredential` calls migrated to `SecretStore.Get` with the world-readable-credentials-file fatal guard and default provider order preserved exactly (C1, FR-046); `SlackConfig.ResolveSecrets` added so inline Slack tokens keep working with a deprecation warning while empty fields resolve via the store, bot-namespaced and skipped entirely when no bot name is configured (C2/C3, FR-047/FR-048 warn-only); a new `boabot --diag-secrets` flag reports, per secret, which provider resolved it by building a disposable single-provider `Store` per candidate rather than touching Phase B's `store.go`, with a sentinel test confirming no value ever reaches stdout or logs (C4, FR-050/FR-051 diagnostic half); `boabotctl secret set/get/delete` added as a separate-module implementation over its own direct `zalando/go-keyring` dependency, sharing only the FR-045 key-naming convention with `boabot` (not code, per the module-boundary constraint), with `get` reporting presence only per FR-049's literal wording (C5). TDD followed throughout; full details in `implementation-notes.md`'s new "Phase C" section. `go build`/`go vet`/`go test -race ./...`/`golangci-lint run` all pass for both `boabot` (repo-wide, confirming Phases A/B unaffected) and `boabotctl`.
