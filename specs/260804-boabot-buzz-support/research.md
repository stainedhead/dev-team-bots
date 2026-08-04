# Research: BaoBot Buzz Support

**Feature:** boabot-buzz-support
**Created:** 2026-08-04
**Source PRD:** `boabot-buzz-support-PRD.md` (this directory)

---

## Research Questions

Seeded from the PRD's Open Questions and Dependencies/Risks sections. Questions marked **[resolved in PRD]** already have a lean stated in the source document; questions marked **[needs implementation research]** require investigation during this spec's Research/Architecture phases.

1. **[resolved in PRD, confirmed pre-flight]** How is invariant I4 (at-most-one-live-instance per pubkey) enforced given BaoBot's in-process-goroutine runtime? → Process-level singleton lock on the nsec (OQ-1 option (a)). Needs concrete design: lock file location, lock acquisition failure mode, interaction with existing graceful-shutdown path.
2. **[needs implementation research]** Exact shape of the `fiatjaf.com/nostr` client API for WebSocket connect/subscribe/publish/reconnect — the PRD verified package presence (`nip29`, `nip42`, `nip44`, `nip17`, `nip34`, `nip70`, `relay.go`, `pool.go`, `keyer/`) but not the full call-level API surface needed to drive a `ChannelMonitor` implementation.
3. **[needs implementation research]** NIP-OA Schnorr signature construction/verification in Go — confirm which `fiatjaf.com/nostr` primitive (likely under `keyer/` or a lower-level BIP-340 dependency) can be reused rather than hand-rolling secp256k1 Schnorr signing.
4. **[needs implementation research]** `zalando/go-keyring` exact function signatures (`Get`/`Set`/`Delete`) and error types (`ErrNotFound`) for the `SecretProvider` adapter.
5. **[needs implementation research]** systemd `$CREDENTIALS_DIRECTORY` read semantics — file naming convention, whether trailing newlines need stripping, permission bits to expect.
6. **[open, non-blocking]** OQ-2 — owner key custody and attestation issuance workflow. Not blocking implementation of FR-005–FR-007 (validating a *configured* `auth` tag), since issuance tooling is out of this spec's scope (deferred to `boabotctl` in a later phase per OQ-5).
7. **[open, non-blocking, resolved for implementation]** OQ-9 — namespace secrets by bot name or type. Resolved: **bot name**, matching `BotName` usage elsewhere (`SlackConfig.BotName`, queue routing). Document the rename-orphaning risk noted in the PRD.

## Industry Standards

- **Nostr NIPs** — NIP-01 (core), NIP-09, NIP-10, NIP-11, NIP-17, NIP-25, NIP-29 (relay-based groups — core channel model), NIP-42 (auth), NIP-50, NIP-70. Full detail in PRD §Background.
- **Buzz draft NIPs** — NIP-OA (Owner Attestation), NIP-AA (Agent Authentication), plus NIP-AE/AM/AO/AP/GS/MP/CW referenced for future phases (P1/P2/deferred). Full text quoted in the PRD.
- **BIP-340** — Schnorr signatures over secp256k1, required for NIP-OA attestation construction/verification.
- **systemd Credentials** (`systemd.io/CREDENTIALS`) — `LoadCredentialEncrypted=`/`SetCredentialEncrypted=`, `$CREDENTIALS_DIRECTORY`.

## Existing Implementations

- `internal/infrastructure/slack/monitor.go` — the `ChannelMonitor` pattern to replicate for Buzz (dispatch, pending-map correlation, `HandleResult`).
- `internal/infrastructure/credentials/credentials.go` — the INI file provider to wrap as `SecretProvider`.
- `cmd/boabot/main.go:56-69` — the two-link chain (`applyCredential`) to replace with `SecretStore`.

## API Documentation

- `fiatjaf.com/nostr` — package inspected in the Go module cache at `v0.0.0-20260731140316-a8080728893f`; full API surface to be documented during Architecture phase.
- `zalando/go-keyring` — inspected at `v0.2.8`; `Get(service, user)`, `Set(service, user, secret)` confirmed present; darwin backend confirmed to omit `-k` keychain argument (see PRD risk table).

## Best Practices

- Confine third-party protocol/crypto imports (`fiatjaf.com/nostr`, `zalando/go-keyring`, `godbus/dbus`) to their respective `internal/infrastructure/` packages — zero imports in `internal/domain/` or `internal/application/`, per `AGENTS.md` Clean Architecture rules.
- TDD for every new package: failing test before production code, per `AGENTS.md`.

## Open Questions

See numbered list above. None are blocking for Phase 2 (Research & Data Modeling) to proceed.

## References

- `boabot-buzz-support-PRD.md` (this directory) — full protocol review, code analysis, and 54 functional requirements.
- [github.com/block/buzz](https://github.com/block/buzz)
- [fiatjaf.com/nostr](https://pkg.go.dev/fiatjaf.com/nostr)
- [zalando/go-keyring](https://github.com/zalando/go-keyring)
