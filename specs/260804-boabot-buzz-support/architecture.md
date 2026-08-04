# Architecture: BaoBot Buzz Support

**Feature:** boabot-buzz-support
**Created:** 2026-08-04
**Status:** Draft

---

## Architecture Overview

Two additive subsystems, both entering through existing seams rather than new ones:

1. **Buzz** enters as a second `domain.ChannelMonitor` implementation, alongside `slack.Monitor`. This requires first fixing `TeamManager`'s existing hardcoded dependency on the concrete `*slackinfra.Monitor` type (a pre-existing Clean Architecture violation — see PRD §Code Analysis) so it holds `[]domain.ChannelMonitor` instead.
2. **Secret storage** enters as a new `SecretStore` domain port consulted at startup in `cmd/boabot/main.go`, replacing the two hardcoded `applyCredential` calls with a provider-chain lookup. Existing callers (`ANTHROPIC_API_KEY`, `BOABOT_BACKUP_TOKEN`) migrate to it; `SlackConfig` and the new `BuzzConfig` gain the same resolution path for their secret fields.

Per the PRD's Architecture Decision (Option A, rejecting Option B/`buzz-acp` and Option C/CLI-shellout): BaoBot remains the agent runtime. Buzz is a transport, not a new control plane.

## Component Architecture

```
cmd/boabot/main.go
  ├─ SecretStore (env → systemd → keystore → file)
  │    └─ used to resolve: ANTHROPIC_API_KEY, BOABOT_BACKUP_TOKEN,
  │       SlackConfig.{bot_token,app_token}, BuzzConfig.{nsec,auth_tag,api_token}
  │
  └─ TeamManager
       ├─ []domain.ChannelMonitor
       │    ├─ slack.Monitor        (existing, unchanged behavior)
       │    └─ buzz.Monitor         (new)
       │         └─ buzz.RelayClient over fiatjaf.com/nostr
       │              ├─ NIP-42 auth (+ optional NIP-OA auth tag)
       │              ├─ NIP-29 kind:9 pub/sub, #h scoped
       │              ├─ kind:0 profile, kind:20001 presence
       │              └─ process-singleton lock (OQ-1)
       └─ result-forwarding loop iterates all registered monitors
```

## Layer Responsibilities

- **Domain** (`internal/domain/`): `RelayClient`/`Event`/`Filter` port; `SecretStore`/`SecretProvider`/`SecretRef`. No infrastructure imports.
- **Application** (`internal/application/team/`): `TeamManager` holds and iterates `[]domain.ChannelMonitor`; no import of `internal/infrastructure/slack` or `internal/infrastructure/buzz` (FR-033).
- **Infrastructure** (`internal/infrastructure/buzz/`, `internal/infrastructure/secret/`): all third-party protocol/crypto/keystore/D-Bus imports confined here.

## Data Flow

**Inbound Buzz message → task dispatch** (mirrors the existing Slack path):
```
relay --kind:9 event--> buzz.Monitor.run() --filter: @mention, not self--> dispatch()
  --> domain.TaskPayload --> domain.Message{Type: task, From: "buzz"} --> domain.MessageQueue
  --> [existing worker harness executes task] --> HandleResult(TaskResultPayload)
  --> pending map lookup (taskID) --> kind:9 reply, NIP-10 threaded, #h scoped
```

**Secret resolution at startup:**
```
main() --> SecretStore.Get(ctx, SecretRef{Name: "buzz_private_key", Bot: cfg.BotName})
  --> providers tried in order: env["BUZZ_PRIVATE_KEY"] (hit/miss)
      --> systemd $CREDENTIALS_DIRECTORY/buzz_private_key (hit/miss, Linux only)
      --> keystore lookup (hit/miss)
      --> credentials file ~/.boabot/credentials (hit/miss, mode-checked)
  --> first hit wins; all-miss --> error naming every provider consulted (FR-053)
```

## Sequence Diagrams

Not needed for planning — the text-form flows above (§Data Flow) are sufficient to derive tasks from. If a visual aid is wanted for the NIP-42/NIP-AA handshake during implementation, add it to `implementation-notes.md`, not here (this file is not revisited after Phase 3).

## Edge Cases and Failure Paths

Resolved during spec review — each maps to a task in `tasks.md`:

- **FR-022 reply-publish failure.** If the `kind:9` reply publish fails (relay rejects, connection drops mid-publish), the monitor MUST log the failure with the task ID and channel, and MUST NOT re-enqueue the task (the worker already ran; re-running would duplicate side effects). The pending-map entry is popped regardless of publish outcome, matching the existing Slack adapter's fire-and-forget reply semantics. A future retry queue is out of scope (not in the PRD).
- **Secret provider timeout.** Per the NFR ("full provider chain MUST complete within 2s per secret... unreachable D-Bus or keychain MUST time out rather than hang"), `SecretStore.Get` wraps each provider's `Lookup` call with a `context.WithTimeout` (default 2s, one deadline per provider, not one for the whole chain) so one hung provider cannot block the rest of the chain. A timeout is treated as a miss (not found), not an error, and is logged as `provider=<name> timeout` distinct from a genuine not-found.
- **Pending map across reconnect.** The `pending map[string]replyTarget` (task ID → channel/thread) lives in `buzz.Monitor` and is **not** cleared or rebuilt on reconnect — only the WebSocket connection, auth, and subscriptions are re-established (FR-012). A task dispatched before a disconnect that completes after reconnect still resolves to its original reply target.
- **Presence ticker during disconnect.** The `kind:20001` presence ticker (FR-023) is suspended while the connection is down (there is no connection to publish on) and resumes immediately on reconnect, publishing an online event as part of re-establishing state. This is a natural consequence of presence being a connection-scoped publish loop, not a free-running timer — documented here so the Phase F task's test asserts it explicitly, since I3 staleness is externally observable.
- **Empty vs. unset `respond_to_allowlist`.** FR-029 says "when set." Resolved: `respond_to_allowlist` uses Go's nil-vs-empty-slice distinction — `nil` (key absent from `buzz:` config) means no allowlist gate; an explicitly configured empty list (`respond_to_allowlist: []`) means allow-none (gate rejects every sender). This matches the principle that an operator who writes `[]` explicitly intends to lock the gate down, not to leave it open. Asserted by a dedicated test case distinguishing the two.

## Cross-Module Constraint (`boabotctl` secret subcommands)

`boabotctl` is a separate Go module (`boabotctl/go.mod`) and cannot import `boabot`'s `internal/` packages — Go's `internal/` visibility is enforced per-module-root regardless of monorepo co-location. FR-049's `boabotctl` subcommands therefore add `zalando/go-keyring` as a direct dependency of `boabotctl/go.mod` and re-implement only the thin lookup/write/delete calls, following the existing `boabotctl/internal/commands/*.go` pattern (see `auth/store.go` for the module's existing local-credential-file precedent). The two modules do not share code for this; they share only the FR-045 key-naming *convention*, which is why FR-045 requires that convention to be documented and stable.

## Integration Points

- **Buzz relay** — WebSocket, `fiatjaf.com/nostr`. Local dev/integration target: Buzz's own `docker-compose.yml` (not run automatically in this job — see AC handling note in `plan.md`).
- **OS keystores** — `zalando/go-keyring` (macOS `security` subprocess, Windows `wincred`, Linux Secret Service over D-Bus).
- **systemd** — `$CREDENTIALS_DIRECTORY` env var read, no systemd API calls.
- **Existing `domain.MessageQueue`, `domain.TaskPayload`, `domain.TaskResultPayload`** — reused unchanged by the Buzz dispatch path.
- **Existing `credentials.Load`** — wrapped, not replaced, as the file provider.

## Architectural Decisions

Recorded here during implementation; final entries also go to `docs/architectural-decision-record.md` per PRD acceptance criteria:

1. **Option A (native Go Nostr client) over Option B (`buzz-acp` harness) or Option C (CLI shellout).** Rationale: preserves BaoBot's own turn loop, worker harness, budget caps, and autonomy gates; single binary; every P0 NIP primitive verified present in a maintained Go library. See PRD §Architecture Decision for full rejection rationale on B and C.
2. **`zalando/go-keyring` over `99designs/keyring`.** Rationale: actively maintained (release Mar 2026, pushed Jul 2026) vs. dormant (last release Dec 2022). Our own provider chain supplies the fallback backends 99designs would have offered.
3. **OQ-1 resolved as a process-level singleton lock**, not a startup presence probe or reply deduplication. Rationale: cheapest, matches the existing world-readable-credentials-file precedent of failing fast on misconfiguration.
4. **OQ-9 resolved as bot-name namespacing** for per-bot secrets. Rationale: consistent with existing `BotName` usage in `SlackConfig` and queue routing.
