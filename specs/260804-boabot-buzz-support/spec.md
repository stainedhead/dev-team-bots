# Spec: BaoBot Buzz Support

**Created:** 2026-08-04
**Source PRD:** `specs/260804-boabot-buzz-support/boabot-buzz-support-PRD.md`
**Status:** Draft

---

## Executive Summary

Buzz is Block's open-source, Nostr-native workspace for humans and AI agents (`github.com/block/buzz`, Apache-2.0). This spec covers two workstreams derived from one PRD:

1. **Buzz support** — BaoBot joins a Buzz workspace as a first-class agent member with its own secp256k1 keypair, authenticating via NIP-42 and an optional NIP-OA owner attestation, reading and writing channel messages (`kind:9`) scoped by `#h`, and maintaining presence (`kind:20001`). Delivered as a new `ChannelMonitor` adapter alongside the existing Slack adapter, which requires first fixing a pre-existing Clean Architecture violation in `TeamManager`.
2. **Secret storage** — A `SecretStore` domain port with an ordered provider chain (env var → systemd credentials → OS keystore → credentials file) that resolves the Buzz nsec and BaoBot's other existing secrets (`ANTHROPIC_API_KEY`, `BOABOT_BACKUP_TOKEN`, Slack tokens) from OS-native encrypted credential stores on macOS, Windows, and Linux, with a fallback path that keeps every existing deployment working unchanged.

Both workstreams are implemented in this run per an explicit scope decision (see `DEV-FLOW-STATUS.md`). OQ-1 (multi-instance singleton) is resolved as a process-level lock. Acceptance criteria requiring live infrastructure (a running `buzz-relay`, a macOS LaunchDaemon, a Windows service, a systemd unit) are implemented as `//go:build integration` tests, not run automatically in this job, and flagged for manual verification.

## Problem Statement

BaoBot today reaches humans through Slack only, with a documented-but-nonexistent Microsoft Teams adapter. Buzz is where agents (goose, Codex, Claude Code) and humans already collaborate as cryptographically identified peers on a shared relay. Joining requires BaoBot to hold its own Nostr keypair, speak the relay's NIPs, and manage that keypair (and BaoBot's other secrets) more safely than the plaintext-in-config or dotfile mechanisms available today.

Full detail: see the source PRD's Problem Statement and Background sections (`boabot-buzz-support-PRD.md` in this directory).

## Goals

- **G1** — BaoBot joins a Buzz workspace as a first-class agent member with its own secp256k1 keypair, reading and writing channel messages under its own authorship.
- **G2** — Buzz is added as a peer channel to Slack behind the existing `ChannelMonitor` seam, with no change to the worker harness, budget, memory, or autonomy subsystems.
- **G3** — Agent identity is owner-attested (NIP-OA/NIP-AA) so BaoBot's provenance is cryptographically verifiable, and revoking the owner's membership revokes the agent's access.
- **G4** — The channel seam is corrected to depend on a domain interface rather than a concrete Slack type.
- **G5** — Secrets can be stored in the OS-native encrypted credential store on macOS, Windows, and Linux, for interactive use.
- **G6** — BaoBot resolves secrets correctly when started unattended as a service on all three platforms.
- **G7** — No secret is required to live in `config.yaml`.
- **G8** — Secret resolution moves behind a domain port with an ordered, testable provider chain.
- **G9** — Existing deployments keep working with no configuration change.

## Non-Goals

Fourteen items, NG1–NG14 — see the source PRD's Non-Goals section. Highlights: not running under `buzz-acp` (NG1), not hosting a relay (NG2), not replacing memory with NIP-AE engrams (NG3), not remote secret managers (NG9), not TPM sealed-at-use (NG11), not secret rotation (NG12).

## User Requirements

All 54 functional requirements (FR-001–FR-054) are carried verbatim from the source PRD — see `boabot-buzz-support-PRD.md` §Functional Requirements in this directory for full text. Grouped by area:

- **Identity and authentication** (FR-001–FR-011): keypair custody, NIP-42/NIP-OA/NIP-AA auth, profile publication.
- **Channel participation** (FR-012–FR-027): connection lifecycle, channel discovery, message pub/sub, threading, dispatch, presence, typing, `!shutdown`, reactions.
- **Security and safety** (FR-028–FR-032): content sanitisation, author gating, budget/autonomy gate inheritance, single-instance enforcement (FR-031, OQ-1 now resolved), audit logging.
- **Wiring and configuration** (FR-033–FR-037): `ChannelMonitor` seam fix, config schema, activation gating, doc corrections.
- **Secret storage — port and chain** (FR-038–FR-045): `SecretStore`/`SecretProvider` domain interfaces, provider chain ordering, keystore provider, systemd provider, credentials-file provider, env-var precedence, per-bot namespacing.
- **Secret storage — callers and configuration** (FR-046–FR-050): `main.go` migration, Slack token resolution, config rejection of inlined secrets, `boabotctl` subcommands, diagnostic command.
- **Secret storage — safety** (FR-051–FR-054): no value logging, no subprocess-argument leakage, provider-enumerated errors, documented residual exposure.

## Non-Functional Requirements

Carried from the PRD's NFR section: Performance (500ms p95 dispatch, 1s p95 reply, 2s secret resolution), Reliability (reconnect with backoff, no cross-channel degradation, provider chain degrades gracefully), Security (credential path constraints, NIP-OA capability-scope warning, Escalating gate for git signing), Observability (structured logs per connect/auth/dispatch/publish stage and per-secret provider), Maintainability (import confinement to infrastructure layer), Testing (TDD, 90% coverage, `//go:build integration` tags), Deployment (single binary, no sidecars), Portability (macOS/Windows/Linux CI matrix), Compatibility (zero-config-change upgrade path).

## System Architecture

**Affected layers:**

- `internal/domain/` — new `ChannelMonitor`-compatible port for the relay client (illustrative `RelayClient` interface); new `SecretStore`/`SecretProvider` interfaces.
- `internal/application/team/` — `TeamManager` refactored to hold a slice of `domain.ChannelMonitor` rather than a concrete `*slackinfra.Monitor` field; result-forwarding loop rewritten to iterate.
- `internal/infrastructure/buzz/` — new package: Nostr relay client over `fiatjaf.com/nostr`, NIP-OA attestation construction/validation, event kind handling, `ChannelMonitor` implementation.
- `internal/infrastructure/secret/` — new package tree: `SecretStore` implementation, `env` provider, `keystore` provider (`zalando/go-keyring`), `systemd` provider, `file` provider (wraps existing `credentials` package).
- `cmd/boabot/main.go` — wiring changes: construct `SecretStore` with the default provider chain; construct and register the Buzz monitor; replace the two `applyCredential` calls.
- `internal/infrastructure/config/` — `SlackConfig` gains secret-resolution path; new `BuzzConfig`.
- `boabotctl/` — new secret-management subcommands (FR-049).

**New components:** Nostr relay client, NIP-OA attestation module, `SecretStore` port + 4 providers, process-singleton lock (OQ-1 resolution).

**Modified components:** `TeamManager` (channel-monitor collection + result forwarding), `SlackConfig`, `cmd/boabot/main.go` secret wiring.

## Scope of Changes

**New files (indicative — actual layout finalized during Architecture phase):**
- `internal/domain/buzz.go` (or similar) — `RelayClient`, `Event`, `Filter` port types
- `internal/domain/secret.go` — `SecretStore`, `SecretProvider`, `SecretRef`
- `internal/infrastructure/buzz/` — monitor, relay client, NIP-OA, event kinds, config
- `internal/infrastructure/secret/` — store, `env/`, `keystore/`, `systemd/`, `file/`
- `boabotctl/internal/.../secret*.go` — CLI subcommands

**Modified files:**
- `internal/application/team/team_manager.go`
- `internal/infrastructure/config/config.go`
- `cmd/boabot/main.go`
- `boabot/AGENTS.md`, root `AGENTS.md` (Teams adapter correction, Buzz adapter documentation)
- `docs/technical-details.md`, `docs/product-details.md`, `docs/architectural-decision-record.md`, `README.md`

**New dependencies:** `fiatjaf.com/nostr` (pseudo-versioned), `zalando/go-keyring`, transitively `godbus/dbus/v5`, `danieljoos/wincred`.

## Breaking Changes

None to existing external interfaces. `SlackConfig` inline tokens remain supported through a deprecation window (FR-047/FR-048) before becoming a hard rejection in a later release — that future rejection is explicitly out of this spec's scope (P2, deferred).

## Success Criteria and Acceptance Criteria

All 51 acceptance criteria from the PRD apply, split "Buzz support" (30) and "Secret storage" (21) — see `boabot-buzz-support-PRD.md` §Acceptance Criteria. Criteria requiring live infrastructure are satisfied by a `//go:build integration` test existing and compiling, plus a manual-verification note in `implementation-notes.md` — not by a passing automated run in this job.

## Risks and Mitigation

Carried from the PRD's Dependencies and Risks table (18 rows) — see source PRD. Highest-attention items for implementation: the `TeamManager` refactor blast radius (mitigate: land behind green Slack tests before any Buzz code), the NIP-OA credential-scope warning (mitigate: document prominently, bound `created_at<`), and the macOS keystore `-k` argument gap (mitigate: FR-041's validation requirement, tagged integration test, documented limitation if unresolved).

## Timeline and Milestones

Phasing carried from the PRD, both workstreams P0 only for this spec (P1/P2/Deferred items are out of scope for this implementation run — see `tasks.md`):

- **Buzz P0:** FR-001→024, FR-028→037
- **Secret Storage P0+P1 (this run implements P1 too, per scope decision):** FR-038→054

## References

- Source PRD: `specs/260804-boabot-buzz-support/boabot-buzz-support-PRD.md`
- `boabot/AGENTS.md`, root `AGENTS.md` — module conventions
- [github.com/block/buzz](https://github.com/block/buzz), [fiatjaf.com/nostr](https://pkg.go.dev/fiatjaf.com/nostr), [zalando/go-keyring](https://github.com/zalando/go-keyring) — see PRD Appendix for full source list
