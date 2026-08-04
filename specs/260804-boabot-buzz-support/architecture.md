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

[TBD — to be added during implementation if a visual is needed for the NIP-42/NIP-AA handshake or the provider-chain resolution; text-form flows above are sufficient for planning.]

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
