# Tasks: BaoBot Buzz Support

**Feature:** boabot-buzz-support
**Created:** 2026-08-04
**Status:** Ready for implementation

---

## Progress Summary

**0 / 57 tasks complete.** Full breakdown below, derived from `plan.md`'s Phase A–H, plus a Phase I (integration-test stubs and the final quality gate) not separately called out in `plan.md` but required by the PRD's `//go:build integration` handling and NFR/Testing requirements. Task IDs use the phase letter (`A`–`I`), not `P1`/`P2`, to avoid colliding with the PRD's own P0/P1/P2 priority phasing.

Every task maps to one or more `FR-###` from the PRD. Coverage check: FR-001 through FR-054 each appear as the owning acceptance criterion of exactly one task below (verified by extracting every `FR-###` reference and diffing against the full range — see the note at the end of this file). Spike tasks (`B1`, `D1`) precede the phases whose downstream tasks depend on API surfaces `research.md` flagged as `[needs implementation research]` — they are not estimable without those spikes, so pretending otherwise would produce fake precision.

---

## Phase A — `TeamManager` seam fix

Prerequisite for everything else; must land with Slack tests green before any Buzz code.

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| A1 | Add `monitors []domain.ChannelMonitor` to `TeamManager`; `WithSlackMonitor` appends the Slack monitor to it instead of setting a dedicated `slackMonitor` field | — | 3h | FR-033: existing Slack test suite passes unchanged; `internal/application/team` imports no infrastructure package |
| A2 | Rewrite result-forwarding (both the orchestrator-mode and non-orchestrator paths, `~line 923`/`~930`) to iterate `tm.monitors` calling `HandleResult`, removing the `if tm.slackMonitor != nil` branches | A1 | 3h | FR-034: `grep -r "infrastructure/slack\|infrastructure/buzz" internal/application` returns no matches; Slack behavior byte-identical (full existing suite green) |

## Phase B — Secret storage domain + providers

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| B1 *(spike)* | Research `zalando/go-keyring` `Get`/`Set`/`Delete` signatures and `ErrNotFound`; confirm systemd `$CREDENTIALS_DIRECTORY/<name>` read semantics (trailing newline, permission bits). Record findings in `research.md` | — | 2h | `research.md`'s two `[needs implementation research]` items for this phase resolved with concrete signatures; unblocks B6/B7 |
| B2 | Define `SecretStore`/`SecretProvider`/`SecretRef` in `internal/domain/secret.go`; zero infrastructure imports | — | 2h | FR-038: package compiles with no keystore/D-Bus/OS-specific import; interface-shape unit test |
| B3 | Implement `SecretStore.Get` ordered-chain resolution: first-hit-wins, per-provider `context.WithTimeout` (2s default, one deadline per provider — see `architecture.md` §Edge Cases), configurable order, tested against fake providers | B2 | 4h | FR-039, FR-040; a provider that errors (e.g. simulated D-Bus refusal) does not halt the chain — next provider consulted, resolution succeeds; a hung provider times out rather than blocking the chain; PRD AC "provider precedence: with the same logical secret present in all four providers, the env var wins; unset it and the systemd credential wins; unset that and the keystore wins; remove that and the file wins — asserted as a single ordered test" (fake providers standing in for each of the four real ones) |
| B4 | Env-var provider (`internal/infrastructure/secret/env/`) | B2 | 1h | FR-044: explicit env var wins in the ordered-precedence test (B3's harness) |
| B5 | File provider wraps `credentials.Load` unchanged; world-readable check remains fatal (`internal/infrastructure/secret/file/`) | B2 | 2h | FR-043: existing error message preserved, verified by test; PRD AC "world-readable `~/.boabot/credentials` remains fatal at startup, with the existing error message" |
| B6 | Systemd credentials provider reads `$CREDENTIALS_DIRECTORY/<name>`, inert (returns not-found, zero cost) when the var is unset (`internal/infrastructure/secret/systemd/`) | B1, B2 | 3h | FR-042 |
| B7 | OS keystore provider over `zalando/go-keyring` (`internal/infrastructure/secret/keystore/`); writes go through the library's stdin (`-i`) path, never argv | B1, B2 | 4h | FR-041 (unit-testable portion: fake-backend tests only; real-keystore + LaunchDaemon/service validation is `//go:build integration`, on the manual-verification list in `implementation-notes.md`); FR-052: no provider passes a secret as a subprocess argument, verified by inspecting the constructed command in test |
| B8 | Per-bot namespacing: thread `SecretRef.Bot` through all four providers; document the keystore key convention (bot **name**, per OQ-9 resolution) in this file's References and in `internal/infrastructure/secret/keystore/` package docs | B4, B5, B6, B7 | 2h | FR-045 |
| B9 | Provider-chain error enumeration: an all-miss `Get` names the reference and lists every provider consulted | B3 | 1h | FR-053 |
| B10 | No-value-logging test across all four providers, captured log buffer + sentinel value, including every provider error path | B4, B5, B6, B7 | 2h | FR-051 (provider half — diagnostic half is C4) |

## Phase C — Secret storage callers

`C5` (`boabotctl`) can proceed in parallel with Phase D–G once B8 lands, per `plan.md`'s Critical Path note.

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| C1 | Migrate `main.go`'s two `applyCredential` calls (`ANTHROPIC_API_KEY`, `BOABOT_BACKUP_TOKEN`) to `SecretStore.Get`, zero observable behavior change | B3, B8 | 2h | FR-046; PRD AC "existing deployment using only env vars, and one using only `~/.boabot/credentials`, both start with a byte-identical config to before this change" |
| C2 | `SlackConfig` gains a secret-resolution path for `bot_token`/`app_token`; existing inline fields keep working and log a deprecation warning naming the file when used | C1 | 3h | FR-047; PRD AC "`config.yaml` with inline Slack tokens still works and emits a deprecation warning naming the alternative"; PRD AC "Slack `bot_token`/`app_token` resolve from the keystore with neither present in `config.yaml`" |
| C3 | Config-loader hook implementing FR-048's warn-only clause ("until [the deprecation period ends] it MUST warn") | C2 | 1h | FR-048 (warn-only portion only — the post-deprecation hard-rejection trigger is out of scope this run, see §Deferred Items below and `spec.md` §Breaking Changes) |
| C4 | Diagnostic command: reports, per configured secret, which provider resolved it — never the value, verified against captured output | B9, B3 | 2h | FR-050; FR-051 (diagnostic half) |
| C5 | `boabotctl secret set/get/delete` — local machine only (OQ-11 resolved local-only), depends directly on `zalando/go-keyring` per `architecture.md` §Cross-Module Constraint since `boabotctl` cannot import `boabot`'s `internal/` packages | B8 | 4h | FR-049; PRD AC "`boabotctl` writes, checks presence of, and deletes a keystore secret on each of the three platforms" (unit-testable portion; per-platform confirmation on the manual-verification list) |

## Phase D — Buzz relay client core

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| D1 *(spike)* | Research `fiatjaf.com/nostr`'s call-level API for connect/subscribe/publish/reconnect, and confirm which primitive (under `keyer/` or a BIP-340 dependency) supplies Schnorr sign/verify for NIP-OA. Record in `research.md` | — | 3h | `research.md`'s two `[needs implementation research]` items for this phase resolved; unblocks D3–D8 and E1–E2 estimates |
| D2 | Domain port + types: `RelayClient` interface and domain-owned `Event`/`Filter` (`internal/domain/buzz.go`), per `data-dictionary.md`'s resolved (not-library-reused) shape | — | 2h | Supports FR-001–FR-024 broadly; `grep -r "fiatjaf.com/nostr" internal/domain` returns no matches |
| D3 | `RelayClient` implementation: WebSocket connect over `fiatjaf.com/nostr`, plus the `domain.Event`⇄`nostr.Event` / `domain.Filter`⇄`nostr.Filter` translation layer | D1, D2 | 4h | Supports FR-012; translation tested in both directions with round-trip fixtures |
| D4 | Keypair load: nsec resolved via `SecretStore` (Phase B/C, not a temporary path — see `plan.md` rationale); fail-closed when missing/malformed/pubkey-derivation fails — Buzz monitor does not start, all other channels still start | D3, B3 | 3h | FR-001, FR-002, FR-003; PRD AC "nsec is read from `BUZZ_PRIVATE_KEY`, and from a `buzz_private_key` credentials-file entry when the env var is unset; a world-readable `~/.boabot/credentials` is fatal at startup"; PRD AC "the agent's private key does not appear in any log output at any level, verified by test against a captured log buffer" — asserted across the full Buzz-monitor startup/connect/auth path (not just the provider-level test in B10) with a captured-log-buffer + sentinel-value test |
| D5 | NIP-42 AUTH: on `["AUTH", challenge]`, build and sign a `kind:22242` event with `["relay", url]` + `["challenge", nonce]`, send `["AUTH", event]` | D3 | 3h | FR-004 |
| D6 | `BUZZ_API_TOKEN` resolution (via B3's chain) and relay auth-token support for `BUZZ_REQUIRE_AUTH_TOKEN=true` relays | D4 | 2h | FR-010; PRD AC "with `BUZZ_REQUIRE_AUTH_TOKEN=true`... a bot configured with a valid `BUZZ_API_TOKEN` connects, and one without it is rejected — both asserted" |
| D7 | `kind:0` profile publish on first successful connection, populated from bot identity (name, bot type, `AGENTS.md` description); not conditional on explicit relay enrollment | D5 | 2h | FR-011; PRD AC "bot's `kind:0` profile renders its name and description... not a bare pubkey"; PRD AC "NIP-AA-authenticated bot successfully publishes its `kind:0` profile without being explicitly enrolled in `relay_members`" |
| D8 | Reconnect: bounded exponential backoff + jitter; re-auth and re-subscribe after every reconnect; pending-map correlation survives reconnect (see `architecture.md` §Edge Cases) | D5, D3 | 4h | FR-012; PRD AC "relay restart mid-session: the bot reconnects, re-authenticates, re-subscribes, and answers a mention sent after recovery — with no operator action and no lost pending correlations" (unit-testable reconnect-logic portion; live-relay-restart confirmation on the manual-verification list) |

## Phase E — NIP-OA / NIP-AA

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| E1 | NIP-OA preimage construction: `nostr:agent-auth:` ‖ `event.pubkey` ‖ `:` ‖ `<conditions>`, SHA-256, conditions string used verbatim (never reordered/deduplicated/normalized) | D2 | 3h | FR-006 |
| E2 | Owner Schnorr sign/verify against the published NIP-OA test vectors (owner secret `0x…01`, agent secret `0x…02`), including all negative cases: 5-element tag, two `auth` tags, `owner == agent` pubkey, whitespace in conditions, leading/trailing/doubled `&`, non-canonical decimal, out-of-range `kind`, reordered conditions string | E1, D1 | 4h | FR-007; PRD AC listing the same negative-case set verbatim |
| E3 | Include the `auth` tag on the `kind:22242` AUTH event when NIP-OA is configured | E2, D5 | 2h | FR-005; PRD AC "NIP-OA `auth` tag issued by an owner key grants the agent relay access via NIP-AA without the agent being explicitly enrolled" (unit-testable construction; end-to-end relay confirmation on manual-verification list) |
| E4 | `created_at` freshness: wall-clock UTC, must fall inside the relay's ±120s window; clock skew beyond it surfaces as a distinguishable error via an injected clock in test | D5 | 2h | FR-008; PRD AC "AUTH event built with a clock offset beyond the relay's ±120s freshness window produces a distinguishable clock-skew error" |
| E5 | Auth failure class distinction: `"invalid: …"` vs `"restricted: …"` in logs/metrics, never collapsed | D5 | 2h | FR-009 |

## Phase F — Channel participation

Includes FR-025/FR-026/FR-027 (typing, gated `!shutdown`, reactions), pulled forward from PRD P1 into this run's scope — see `spec.md` §Timeline and Milestones.

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| F1 | Channel discovery via `kind:39000`/`39002` over the authenticated WebSocket; REST `GET /api/channels?member=true` not required for P0 | D3, D5 | 3h | FR-013; PRD AC "channel discovery works over the WebSocket alone with the REST endpoint unavailable" |
| F2 | Subscribe `kind:9` per discovered channel, filter scoped by `#h` | F1 | 2h | FR-014 |
| F3 | Auto-subscribe on `kind:44100` (own pubkey `#p`), unsubscribe on `kind:44101` | F1 | 3h | FR-015 |
| F4 | P-gate guard: reject in our own code (before send) any subscription for `44100`/`44101`/`1059` lacking a matching `#p` filter; covers `1059` from the outset even though DM handling is P1 | F3 | 2h | FR-016; PRD AC "subscription request for a p-gated kind without a matching `#p` filter is rejected by our own code before reaching the relay" |
| F5 | Publish `kind:9` with `#h` tag, signed by the agent key | F2 | 2h | FR-017 |
| F6 | NIP-10 reply threading referencing the mention's root event | F5 | 2h | FR-018 |
| F7 | Route inbound message bodies, profile metadata, channel names/topics through the existing prompt-injection sanitisation path, treated as untrusted regardless of author | F2 | 3h | FR-028; PRD AC "inbound message content containing prompt-injection patterns is sanitised on the same path as MCP tool output" |
| F8 | Inbound author gate: optional `respond_to` / `respond_to_allowlist`; nil vs. explicit-empty-list distinction per `architecture.md` §Edge Cases (nil = no gate, `[]` = allow-none) | F2 | 2h | FR-029; PRD AC "mention from a pubkey outside `respond_to_allowlist` is ignored, and the rejection appears in structured logs" |
| F9 | Self-authored-event filter (loop prevention), matching the Slack adapter's bot-message filter | F2 | 1h | FR-020 |
| F10 | Dispatch trigger: qualifying event = @mention of own pubkey in a subscribed channel only, gated by F7/F8/F9; written so a P1 second trigger source (DMs) is additive, not a rewrite | F7, F8, F9 | 3h | FR-019 |
| F11 | Mint task ID, build `domain.TaskPayload`, enqueue `domain.Message{Type: task, From: "buzz"}`; subject to existing per-bot budget caps and calibrated-autonomy gates with no Buzz-specific bypass | F10 | 3h | FR-021, FR-030; PRD AC "Buzz-triggered task that exceeds the bot's daily token cap is refused by the existing `BudgetTracker` with no Buzz-specific bypass" |
| F12 | Pending map (task ID → channel/thread); `HandleResult` publishes `kind:9` reply in the correct channel/thread; unmatched task IDs ignored silently; reply-publish-failure path logs and does not re-enqueue (`architecture.md` §Edge Cases) | F11, F6 | 4h | FR-022 |
| F13 | Structured logs (agent pubkey, relay URL, channel UUID, event ID) for every dispatched task and published reply; private key never appears | F11, F12 | 2h | FR-032 |
| F14 | `kind:20001` presence publish loop, ≤128 chars, refreshed under the 180s staleness bound; suspended while disconnected, resumes on reconnect (`architecture.md` §Edge Cases) | D8 | 3h | FR-023; PRD AC "bot publishes `kind:20001` at an interval under the 180-second staleness bound" |
| F15 | Graceful shutdown: publish `offline` presence and close the relay connection cleanly, before the existing shutdown path completes | F14 | 2h | FR-024; PRD AC "on `SIGTERM` it publishes `offline` and closes cleanly before exit" |
| F16 | `kind:20002` typing indicator while a triggered task executes *(pulled forward from PRD P1)* | F11 | 2h | FR-025 |
| F17 | `!shutdown` as a stop signal routed through the existing graceful-shutdown path, gated by F8's author gate; rejected+logged from any other pubkey *(pulled forward from PRD P1)* | F8 | 2h | FR-026; PRD AC "`!shutdown` from a pubkey outside the FR-029 author gate is ignored and logged... one from an allowed pubkey shuts the bot down gracefully" |
| F18 | Reaction subscriptions always `{"kinds":[7],"#h":[channel-uuid]}`; kinds-only reaction subscription rejected *(pulled forward from PRD P1)* | F2 | 2h | FR-027; PRD AC "reaction subscription is asserted by test to carry `#h`; a kinds-only reaction subscription is asserted to be rejected" |

## Phase G — Process-singleton lock (OQ-1)

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| G1 | Process-level singleton lock acquired at Buzz-monitor startup, keyed on the nsec; a second boabot process against the same key refuses to attach its Buzz monitor and logs why, without crashing the process (other channels still start, per FR-003) | D4 | 4h | FR-031; unit test: two in-process monitor-start attempts against the same nsec — second refuses, logs, first is unaffected. PRD AC "(Blocked on OQ-1)" is satisfied by this lock for the unit-testable case; the two-*process*, live-relay confirmation (exactly one reply, one presence identity) is `//go:build integration`, on the manual-verification list — see `spec.md` §Success Criteria |

## Phase H — Config and docs

`BuzzConfig` is a thin wiring type: `internal/infrastructure/buzz/`'s own components (relay client, monitor, lock, etc.) take plain typed constructor parameters (a URL string, a pubkey allowlist, a duration) in Phases D–G, not `*config.BuzzConfig` directly — only `cmd/boabot/main.go`'s wiring and H2's activation gate consume the struct. That keeps H1 dependency-free and lets it land any time; it is grouped under Phase H for narrative purposes (it belongs with the rest of "config and docs"), not because it is built last.

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| H1 | `BuzzConfig` in `internal/infrastructure/config/config.go`: `enabled`, `relay_url`, `bot_name`, `owner_pubkey`, `respond_to`, `respond_to_allowlist`, `channels`, `presence_interval`; reject any secret-looking key under the `buzz:` block at config load | — | 3h | FR-035; PRD AC "`buzz:` config block containing any secret-looking key is rejected at config load with a clear error" |
| H2 | Wire `cmd/boabot/main.go` to construct Phase D–G's components from `BuzzConfig` and activate the Buzz monitor only when `buzz.enabled` is true and all required settings resolve; disabled ⇒ zero Nostr code path executes, no relay connection attempted | H1, D2–D8, F1–F18, G1 | 2h | FR-036; PRD AC "with `buzz.enabled: false`, no relay connection is attempted and no Nostr code path executes; Slack behaviour is byte-identical to today" |
| H3 | Correct `boabot/AGENTS.md` and root `AGENTS.md`: remove the nonexistent Microsoft Teams adapter, document the Buzz adapter in its place | — | 2h | FR-037; PRD AC "`boabot/AGENTS.md` and root `AGENTS.md` no longer claim a Microsoft Teams adapter" |
| H4 | Update `docs/technical-details.md`, `docs/product-details.md`, `docs/architectural-decision-record.md` (Option A/B/C decision, validated `block/buzz` commit, `zalando` vs. `99designs` evidence, per-OS secret matrix), `README.md`, and a new `user-docs/` secret-provisioning guide stating the residual exposure explicitly (plaintext in process memory; Linux-service tmpfs) | H1, H2, B*, C* | 4h | FR-054; PRD ACs "`docs/technical-details.md`... updated" (both workstreams); PRD AC "`user-docs/` gains a secret-provisioning guide with the per-OS × per-mode matrix and the residual-exposure statement from FR-054" |

## Phase I — Integration-test stubs and final quality gate

Not a separate `plan.md` phase heading, but required by the PRD's `//go:build integration` handling (per this run's pre-flight scope decision) and by the NFR/Testing and NFR/Portability sections.

| ID | Task | Depends On | Est. Duration | Acceptance Criteria |
|---|---|---|---|---|
| I1 | `//go:build integration` test stubs (compile, not run) for: live `buzz-relay` NIP-42 auth + online presence in the desktop client; `kind:0` profile publish and rendering; end-to-end @mention→dispatch→reply; NIP-OA `auth` tag granting NIP-AA virtual membership **without** explicit enrollment (write-path: `kind:0` publish succeeds unenrolled; read-path negative: agent does **not** inherit the owner's channel memberships and cannot read a private channel the owner belongs to); owner-membership revocation causing the next connection to fail `restricted:`; reconnect-after-restart with no lost pending correlations; two boabot processes against the same nsec producing exactly one reply and one presence identity (FR-031/G1 end-to-end) | D–G complete | 4h | Every Buzz-support PRD AC requiring live infrastructure has a corresponding tagged test, explicitly including the two PRD ACs easiest to miss: "a NIP-AA-authenticated bot is confirmed not to inherit the owner's channel memberships" and the OQ-1 two-process AC; each is logged on `implementation-notes.md`'s manual-verification checklist |
| I2 | `//go:build integration` test stubs for interactive keystore resolution on macOS (login keychain), Windows (Credential Manager), and Linux (Secret Service, desktop session) — three separate PRD ACs, one per OS — plus service-mode: macOS LaunchDaemon System-keychain reachability (FR-041's core question), Windows service Credential Manager under its own account identity (OQ-7), Linux systemd `LoadCredentialEncrypted=` with no session D-Bus, and the Linux negative path (keystore-only, no systemd credential ⇒ named-provider error, not a hang) | B6, B7 | 4h | Every Secret-storage PRD AC requiring live infrastructure has a corresponding tagged test — both the three interactive-mode ACs and the four service-mode ACs — each logged on `implementation-notes.md`'s manual-verification checklist |
| I3 | Dispatch-latency measurement harness: relay-delivery→enqueue at p95 <500ms, `HandleResult`→publish at p95 <1s, committed with the tests that exercise it | F10, F12 | 3h | PRD AC "dispatch latency... measured under load and reported at p95... measurement harness is committed with the tests" |
| I4 | Final quality gate: `go fmt`/`go vet`/`golangci-lint run`/`go test -race -coverprofile=coverage.out ./...`; domain+application coverage ≥90% and not regressed; grep-based ACs (`fiatjaf.com/nostr` and `go-keyring`/`godbus` absent from domain+application; `infrastructure/slack`\|`infrastructure/buzz` absent from application) | All | 2h | NFR Testing, NFR Portability; PRD's grep-based ACs (both workstreams); CI matrix green on macOS/Windows/Linux (portability build tags — actual multi-OS CI run is out of this job's automated scope per the pre-flight decision on live-infrastructure ACs, but `go build`/`go vet`/lint/test MUST pass locally on the development platform, with platform-specific code behind build tags reviewed for a working fallback) |

---

## Deferred Items (not scheduled this run)

Out of scope per PRD phasing and this run's scope decision (`spec.md` §Timeline and Milestones, `DEV-FLOW-STATUS.md`):

- **FR-048's hard-rejection trigger** (config load fails once the deprecation period ends) — only the warn-only clause (task C3) ships now.
- **Buzz PRD P1, not pulled forward:** NIP-17 gift-wrapped DMs and DM-triggered tasks (FR-019 explicitly excludes these from P0), NIP-50 search over channel history, NIP-CW channel-window paging.
- **Buzz PRD P2:** NIP-AP persona publication (`kind:30175`), NIP-AM turn metrics (`kind:44200`), NIP-AE engram publication (`kind:30174`).
- **Buzz PRD "Deferred, not scheduled":** NIP-GS git signing, NIP-34 patches, NIP-MP projects, NIP-AO observability, Blossom media, canvases, voice, `kind:40002`/`40003` rich content.
- **Secret Storage "Deferred":** remote secret managers (AWS Secrets Manager, Vault, 1Password), secret rotation/expiry/lease renewal, TPM/Secure-Enclave sealed-at-use key operations (`systemd-creds --with-key=tpm2` storage remains supported as a pass-through, per NG11 — no TPM code is written).

## Coverage Verification Note

**FR sweep (mechanical, re-run after every edit to this file):** `grep -oE 'FR-[0-9]{3}' tasks.md` restricted to the task tables (excluding this note and the Deferred Items section) covers FR-001 through FR-054 with no gaps — verified by diffing against the full range. FR-048 appears once, scoped to its warn-only clause; its hard-rejection trigger is in Deferred Items, not double-counted here.

**AC sweep (manual, walked against the PRD's 51 `- [ ]` acceptance-criteria lines):** every Buzz-support and Secret-storage AC is either (a) the named criterion on a unit-testable task above, or (b) enumerated on `implementation-notes.md`'s "Manual Verification Required" checklist, itself produced by a Phase I integration-test-stub task. Two gaps found during this sweep and fixed: the three interactive-keystore ACs (macOS login keychain / Windows Credential Manager / Linux Secret Service, desktop session) had no home — added to I2 and to `implementation-notes.md`; and "a NIP-AA-authenticated bot is confirmed not to inherit the owner's channel memberships" had no home — added to I1 and to `implementation-notes.md`. Also strengthened: B3 now names the four-provider ordered-precedence AC explicitly; D4 now names the private-key-never-in-logs AC explicitly (distinct from B10's provider-level version — this one covers the full Buzz-monitor code path).

This mapping should be re-verified against the commit log at the close of Step 3 (`AGENTS.md`'s "check each finding off explicitly against the commit log — do not rely on memory" applies equally to this FR/AC checklist).
