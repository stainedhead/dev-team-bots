# Plan: BaoBot Buzz Support

**Feature:** boabot-buzz-support
**Created:** 2026-08-04
**Status:** Planning

---

## Development Approach

TDD throughout (Red → Green → Refactor), per `AGENTS.md`. Two largely-independent workstreams implemented in the same pass, sequenced to de-risk the shared prerequisite first:

1. **Prerequisite:** fix the `TeamManager` → `internal/infrastructure/slack` Clean Architecture violation (introduce `[]domain.ChannelMonitor`) with Slack's existing tests green before any Buzz code lands. This is called out explicitly as a blast-radius risk in the PRD.
2. **Secret storage first** (FR-038–054): the `SecretStore` port, its four providers, and the `main.go` migration. This is independently valuable (ships with zero behavior change per FR-046) and Buzz's own secret resolution can then be built against it directly rather than against the temporary env-var/file-only path.
3. **Buzz support** (FR-001–037): relay client, NIP-42/NIP-OA/NIP-AA auth, channel pub/sub, dispatch, presence, config, docs.

## Phase Breakdown

- **Phase A — `TeamManager` seam fix.** `domain.ChannelMonitor` collection, iterate-based result forwarding. Slack-only behavior change: none observable.
- **Phase B — Secret storage domain + providers.** `SecretStore`/`SecretProvider` in `internal/domain/`; `env`, `file` (wraps existing `credentials` package), `systemd`, `keystore` providers in `internal/infrastructure/secret/`.
- **Phase C — Secret storage callers.** `main.go` migration (FR-046), `SlackConfig` secret path (FR-047, warn-only), diagnostic command (FR-050), `boabotctl` subcommands (FR-049). FR-048's *hard rejection* is out of scope for this run (deferred one release per PRD phasing — see `spec.md` §Timeline and Milestones); only the warn-only deprecation path (FR-047) ships now, and its config-loader hook is written so the hard-rejection switch is a follow-on config-flag change, not a new code path.
- **Phase D — Buzz relay client core.** `RelayClient` port + `fiatjaf.com/nostr`-backed implementation: connect, NIP-42 auth, reconnect/backoff, `kind:0` profile publish.
- **Phase E — NIP-OA/NIP-AA.** Preimage construction, Schnorr sign/verify against published test vectors, `auth` tag inclusion in the `kind:22242` AUTH event.
- **Phase F — Channel participation.** Discovery (`39000`/`39002`), `kind:9` pub/sub with `#h` scoping, p-gate guard (FR-016), NIP-10 threading, dispatch/pending-map correlation, presence/typing, `!shutdown` gating, reaction subscription guard.
- **Phase G — Process-singleton lock (OQ-1).** Lock acquisition at Buzz-monitor startup; refusal + log on contention.
- **Phase H — Config and docs.** `BuzzConfig`, activation gating, `AGENTS.md` corrections (both files), `docs/` updates, `user-docs/` secret-provisioning guide.

## Critical Path

`TeamManager` seam fix → Secret storage providers → Buzz relay client core → NIP-OA/NIP-AA → Channel participation → process-singleton lock → config/docs. `boabotctl` subcommands (Phase C tail) can proceed in parallel with Phase D–G once Phase B lands, since they only depend on the `SecretStore` port.

## Testing Strategy

- Unit tests for every new package, mocking `RelayClient` and `SecretProvider` at their domain interfaces — no real relay or keystore required.
- NIP-OA sign/verify asserted against the PRD's published test vectors, including all negative cases enumerated in the PRD's acceptance criteria.
- Adapter/integration tests (`fiatjaf.com/nostr` against a real relay; `zalando/go-keyring` against a real OS keystore; systemd provider against a real `$CREDENTIALS_DIRECTORY`) tagged `//go:build integration` and **not run in this job** per the pre-flight scope decision — compiled only, documented in `implementation-notes.md` as needing manual verification against: a local `buzz-relay` (Buzz's `docker-compose.yml`), a macOS LaunchDaemon, a Windows service, and a systemd unit.
- Coverage target: ≥90% on `internal/domain/` and `internal/application/`, per `AGENTS.md`.

## Rollout Strategy

Both workstreams are additive and gated: Buzz activates only when `buzz.enabled: true` (FR-036); the secret-storage chain is backward compatible by construction (FR-044, env var still wins) so existing deployments are unaffected without any config change (G9). No feature flag beyond `buzz.enabled` is needed.

## Success Metrics

Per the PRD's acceptance criteria (51 total, `boabot-buzz-support-PRD.md`). This implementation run's definition of done: all functional requirements FR-001–FR-054 implemented and unit-tested — with FR-048 scoped to its warn-only clause, the hard-rejection trigger deferred per `spec.md` §Timeline and Milestones — `go build`/`go vet`/`golangci-lint run`/`go test -race ./...` passing, domain/application coverage ≥90% and not regressed, and every acceptance criterion either passing (unit-testable) or explicitly flagged as needing manual verification (infrastructure-dependent), per `tasks.md`'s Coverage Verification Note.
