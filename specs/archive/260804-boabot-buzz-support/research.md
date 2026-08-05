# Research: BaoBot Buzz Support

**Feature:** boabot-buzz-support
**Created:** 2026-08-04
**Source PRD:** `boabot-buzz-support-PRD.md` (this directory)

---

## Research Questions

Seeded from the PRD's Open Questions and Dependencies/Risks sections. Questions marked **[resolved in PRD]** already have a lean stated in the source document; questions marked **[needs implementation research]** require investigation during this spec's Research/Architecture phases.

1. **[resolved in PRD, confirmed pre-flight]** How is invariant I4 (at-most-one-live-instance per pubkey) enforced given BaoBot's in-process-goroutine runtime? → Process-level singleton lock on the nsec (OQ-1 option (a)). Needs concrete design: lock file location, lock acquisition failure mode, interaction with existing graceful-shutdown path.
2. **[resolved — D1 spike]** Exact shape of the `fiatjaf.com/nostr` client API for WebSocket connect/subscribe/publish/reconnect — see "Phase D — D1 spike findings" below.
3. **[resolved — D1 spike]** NIP-OA Schnorr signature construction/verification in Go — confirm which `fiatjaf.com/nostr` primitive (likely under `keyer/` or a lower-level BIP-340 dependency) can be reused rather than hand-rolling secp256k1 Schnorr signing. → `github.com/btcsuite/btcd/btcec/v2/schnorr`, see below.
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

## Phase D — D1 spike findings (relay client core)

Confirmed by reading source directly in the module cache (`$(go env GOMODCACHE)/fiatjaf.com/nostr@v0.0.0-20260731140316-a8080728893f/`), not from search results or documentation. `go get fiatjaf.com/nostr@master` from `boabot/` re-resolved to the **same** pseudo-version the PRD found (`v0.0.0-20260731140316-a8080728893f`) — no newer commit exists as of this run.

**Connect:** `nostr.NewRelay(ctx, url, opts) *Relay` + `(*Relay).Connect(ctx) error`, or the one-call `nostr.RelayConnect(ctx, url, opts) (*Relay, error)`. `RelayOptions{AuthHandler, NoticeHandler, CustomHandler, RequestHeader, AssumeValid}`.

**Critical, non-obvious constraint: a `*nostr.Relay` cannot be reconnected in place.** `Close()`/any connection failure cancels the relay's internal `connectionContext` via a `context.CancelCauseFunc` captured once in `NewRelay`; `ConnectWithClient` hard-fails (`"relay context canceled"`) once that context is done, and there is no API to reset it. Separately, `(*Relay).authed` is set to `true` on a successful `Auth()` and is **never reset** anywhere in the package. Consequence for our design: **reconnect means dialing a brand-new `*nostr.Relay` (a fresh `dial`), never resetting/reusing an old one.** `internal/infrastructure/buzz/conn.go`'s `dialFunc` seam and `reconnect.go`'s loop are built around this from the start.

**Auth (NIP-42):** `(*Relay).Auth(ctx, sign func(context.Context, *Event) error) error` builds `Event{CreatedAt: Now(), Kind: KindClientAuthentication /* = 22242 */, Tags: {{"relay", url}, {"challenge", challenge}}}`, calls `sign(ctx, &event)` (our hook fills in ID/PubKey/Sig, and is exactly where a NIP-OA `auth` tag gets appended before signing — D5's extension point), then publishes an `AuthEnvelope` and blocks for the relay's OK/CLOSED response, returning that as the error (this is where FR-009's `"invalid: …"`/`"restricted: …"` text will come from, once Phase E adds classification). **Deliberately not wired via `RelayOptions.AuthHandler`:** the library auto-fires `Auth` from a goroutine when it parses an incoming `AuthEnvelope`, but that goroutine discards `Auth`'s return value entirely (`go func(){ r.Auth(...) }()` in `handleMessage`) — there is no way to observe the real OK/CLOSED text through that path. `internal/infrastructure/buzz.RelayClient` instead calls `(*Relay).Auth` explicitly from `Authenticate(ctx)`/the reconnect loop, and handles the resulting small race (challenge frame not yet processed by the read loop) by retrying on the library's exact `"no challenge, can't AUTH"` error text until `ctx` gives up — see `authenticateOn` in `relay_client.go`.

**Publish:** `(*Relay).Publish(ctx, event Event) error` — sends `EventEnvelope`, blocks for OK.

**Subscribe:** `(*Relay).Subscribe(ctx, filter Filter, opts SubscriptionOptions) (*Subscription, error)`; `Subscription.Events chan Event` delivers matches, closed when either `ctx` (the one passed to `Subscribe`) **or** the relay's own connection context is done. **Second non-obvious constraint:** handing `Subscription.Events` straight to a domain caller would mean it dies at the first disconnect and never recovers — there is no way to "re-open" a `Subscription`. `internal/infrastructure/buzz.RelayClient.Subscribe` therefore never returns the library's channel directly: it owns a long-lived `chan domain.Event` per logical subscription and a `pumpSub` goroutine that re-attaches to a fresh library `Subscribe` call (via `relayConn.Subscribe`, which intentionally returns `<-chan nostr.Event` rather than `*Subscription`) after every reconnect, forwarding translated events onto the same caller-facing channel — this is the mechanism `tasks.md` D8 asked to be designed now for later phases (F) to add more subscriptions on top of.

**Reconnect:** no built-in reconnect/backoff in `relay.go`/`pool.go` beyond `Pool`'s multi-relay fan-out helpers, which don't fit our single-relay `RelayClient` interface shape. Hand-rolled in `reconnect.go`: bounded exponential backoff with full jitter (`BackoffConfig{Base, Max}`, capped growth, `[0,1)` jitter via an injectable seam), watching `Relay.Context().Done()` (exposed through `relayConn.Done()`), re-dialing, re-authenticating, then re-attaching every registered subscription.

**BIP-340 Schnorr (for NIP-OA, Phase E):** `github.com/btcsuite/btcd/btcec/v2/schnorr` — already a transitive dependency of `fiatjaf.com/nostr` (used internally by `(*Event).Sign`/`VerifySignature` in `signature.go`). `schnorr.Sign(sk *btcec.PrivateKey, hash []byte, opts ...SignOption) (*schnorr.Signature, error)` and `sig.Verify(hash []byte, pubkey *btcec.PublicKey) bool` are the exact primitives Phase E needs for the NIP-OA preimage signature (owner signs `SHA256("nostr:agent-auth:" + agentPubkeyHex + ":" + conditions)`) — the same functions `nostr.Event.Sign`/`VerifySignature` already use, just invoked directly against a raw hash instead of an event ID. `nostr.PubKeyFromHex`/`schnorr.ParsePubKey` validate a hex pubkey decodes to a point on the curve.

**Key handling:** `nostr.SecretKey [32]byte`, `.Public() PubKey`, `.Hex()`. `nostr.Generate()`, `nostr.SecretKeyFromHex(hex string) (SecretKey, error)` (left-pads short hex with zeros — **not** a validation of key length, only of "≤64 hex chars"; a short hex string like `"abcd"` is silently accepted as a very weak but syntactically valid key). `nip19.Decode(bech32 string) (prefix string, value any, err error)` returns `("nsec", nostr.SecretKey, nil)` for an `nsec1…` string — this is what `LoadKeypair` (D4) uses for the `BUZZ_PRIVATE_KEY` value `buzz-admin generate-key` actually prints. **`LoadKeypair` independently rejects the all-zero key and re-validates the derived pubkey via `nostr.PubKeyFromHex`**, since neither `nip19.Decode` nor the short-hex left-pad path perform that check on their own.

### Critical finding: upstream checkptr/`unsafe.Pointer` bug under `go test -race`

`fiatjaf.com/nostr@v0.0.0-20260731140316-a8080728893f`'s `event.go` (`appendJSONString`/`writeJSONString`, the "SWAR" fast-path JSON-string escaper used by `Event.Serialize`/`SetID`/`Sign`/`VerifySignature`) stores a pointer as a bare `uintptr` across statements and later reconstructs it via arithmetic:

```go
// event.go:241, 245 (writeJSONString; appendJSONString has the identical pattern)
base := uintptr(unsafe.Pointer(unsafe.StringData(s)))
...
w := *(*uint64)(unsafe.Pointer(base + uintptr(i)))
```

This is a textbook violation of `unsafe.Pointer` safety rule 1 (a `uintptr`→`Pointer` conversion must happen in the same expression as the `Pointer`→`uintptr` conversion that produced the value) and is exactly the pattern Go's `checkptr` instrumentation — auto-enabled by `go test -race` — exists to catch. **Confirmed empirically:** signing any event (via `Event.Sign`, called by our `Publish`/`Authenticate`/`publishProfile`) while other goroutines are running (e.g. `RelayClient`'s own background `watchLoop`) crashes the whole test binary with `fatal error: checkptr: pointer arithmetic result points to invalid allocation` — a hard, unrecoverable process abort, not a normal test failure. It is **non-deterministic** (GC-timing dependent): isolated tight loops signing thousands of events with no concurrent goroutine did not reproduce it; the same signing call reproduced reliably once a second goroutine was alive (our `watchLoop`, started by `Connect`). This means the defect is real and not specific to any particular event content — any BaoBot code path that signs an event under `-race` while any other goroutine is running is at risk, which in practice is every non-trivial test in this package (and would be true of the production binary too, except production isn't built with `-race`, so `checkptr` is never enforced there — the underlying pointer arithmetic is still technically UB, just not caught).

**Workaround (documented, not a silent weakening):** `go test -race -gcflags=all=-d=checkptr=0 ./...` — keeps the race detector's actual data-race detection fully active, disabling only the `checkptr` pointer-arithmetic-bounds sub-diagnostic that this specific upstream code trips. Verified stable across 5+ repeated full-package runs with this flag; without it, the crash reproduces reliably (not just occasionally) once `RelayClient`'s background goroutine is running. **This flag must be used for every `go test -race` invocation touching `internal/infrastructure/buzz` (and therefore, in practice, repo-wide, since a single `go test -race ./...` command compiles the whole module) until upstream fixes `event.go`'s SWAR string-escaping code.** No local patch was applied — `fiatjaf.com/nostr` is an unmodified module-cache dependency, not vendored. Not filed upstream as part of this job (no outbound network/issue-tracker access in this environment); flagging here so a human can file it against `fiatjaf.com/nostr` with the exact `base := uintptr(...)` / reconstruction pattern above.

## References

- `boabot-buzz-support-PRD.md` (this directory) — full protocol review, code analysis, and 54 functional requirements.
- [github.com/block/buzz](https://github.com/block/buzz)
- [fiatjaf.com/nostr](https://pkg.go.dev/fiatjaf.com/nostr)
- [zalando/go-keyring](https://github.com/zalando/go-keyring)
