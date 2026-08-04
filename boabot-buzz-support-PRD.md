# PRD: BaoBot Buzz Support

**Created:** 2026-08-04
**Jira:** N/A
**Status:** Draft
**Owner:** stainedhead

---

## Problem Statement

Buzz is an open-source, Nostr-native workspace released by Block in July 2026 ([github.com/block/buzz](https://github.com/block/buzz), Apache-2.0, Rust, ~22.5k stars, actively developed). It merges team chat, a git forge, and AI agents into a single relay where **every actor — human or agent — holds its own secp256k1 keypair and every action is a signed Nostr event**.

BaoBot today reaches humans through Slack only. Its `ChannelMonitor` seam has exactly one live implementation (`internal/infrastructure/slack`), and the Microsoft Teams adapter documented in `boabot/AGENTS.md` **does not exist on disk** — `internal/infrastructure/teams/` is absent from the tree.

That leaves three gaps:

1. **No cryptographic identity.** A BaoBot in Slack is a bot token owned by a workspace app. In Buzz, an agent is a first-class member with its own keypair, its own audit trail, and its own permissions — not an integration behind a human's account. BaoBot cannot participate as a peer in a Buzz workspace.
2. **No agent-to-agent interop beyond our own cluster.** BaoBot's cross-bot delegation is SQS-shaped and closed to our own team. Buzz is where third-party agents (goose, Codex, Claude Code) and humans already collaborate in the open.
3. **No portable audit trail.** BaoBot's provenance lives in our logs and DBs. Buzz gives every message, patch, review, and workflow trigger a signed, hash-chained, independently verifiable record.

This PRD defines what BaoBot must implement to join a Buzz workspace as a native, first-class agent member.

---

## Background — Protocol and Ecosystem Review

### What Buzz is, architecturally

Buzz is **a Nostr relay plus clients**, not an app with a Nostr bolt-on. The relay (`buzz-relay`, Rust/Axum) serves WebSocket and REST, backed by Postgres (events + full-text search), Redis (pub/sub, presence, typing), and S3/MinIO (media). Supporting services: `buzz-auth` (NIP-42/98 Schnorr auth, rate limiting), `buzz-db`, `buzz-pubsub`, `buzz-search`, `buzz-audit` (hash-chain log). Channels, threads, DMs, git events, workflows, canvases, and voice huddles are all signed Nostr events on that one relay.

The consequence for us: **there is no proprietary Buzz API to integrate against.** The integration surface is the Nostr wire protocol over WebSocket. Any correct Nostr client that speaks the right NIPs is a first-class Buzz client.

### Standard NIPs the relay implements

| NIP | Role in Buzz |
|---|---|
| NIP-01 | Core event/filter protocol; kind:0 profile metadata |
| NIP-09 | Event deletion (kind:5) |
| NIP-10 | Threading via reply tags |
| NIP-11 | Relay information document |
| NIP-17 | Encrypted DMs via gift wrapping (kind:1059) |
| NIP-25 | Reactions (kind:7) |
| NIP-29 | **Relay-based groups — the core channel model** |
| NIP-42 | Client-to-relay authentication challenge/response |
| NIP-50 | Full-text search over messages |
| NIP-70 | Protected events (membership rosters) |

Deferred by the relay: NIP-04/NIP-44 direct messages, and `kind:10050` DM relay lists.

### Event kinds that matter to an agent

**Channel traffic (NIP-29).** `kind:9` is the group message and **requires an `#h` tag carrying the channel UUID**. Membership and lifecycle: `9000` add user, `9001` remove user (self-remove allowed, last-owner guard), `9002` edit metadata, `9005` admin delete, `9007` create group, `9008` delete group, `9021` join request (open channels only), `9022` leave.

**Relay-signed discovery.** `39000` group metadata, `39001` admin list, `39002` member list. On private channels these are members-only.

**System and presence.** `20001` presence (ephemeral, ≤128 chars), `20002` typing indicator, `44100`/`44101` member added/removed, `1059` gift-wrapped DM.

**Buzz-specific extensions.** `9033` workspace icon, `13534` membership roster snapshot (relay-signed, NIP-70 protected), `40002` rich content, `40003` edit events. `40002`/`40003` are **Buzz-only and not rendered by standard Nostr clients**. Relay admin over WebSocket (NIP-43): `9030` add member, `9031` remove member, `9032` change role.

### Two protocol traps we must design around

1. **P-gated subscriptions.** Global subscriptions matching kinds `44100`, `44101`, `1059` **must** include a `#p` filter whose values all equal the authenticated pubkey. Otherwise: `"restricted: p-gated events require #p matching your pubkey"`.
2. **Reaction channel derivation.** Reactions resolve their channel from the target event's `#e` tag; **client-supplied `#h` tags on reactions are ignored**. Live fan-out segregates channel and global subscriptions, so a kinds-only subscription `{"kinds":[7]}` receives *no reactions at all*. Reaction subscriptions must be `{"kinds":[7],"#h":["<channel-uuid>"]}`.

Both are silent-failure modes: a naive client sees an empty stream and no error.

### Buzz's own draft NIPs — the agent-specific layer

Buzz ships 17 draft NIPs under `docs/nips/`. The agent-relevant ones:

| NIP | Kind(s) | What it gives an agent |
|---|---|---|
| **NIP-OA** Owner Attestation | tag-only | An `auth` tag by which an owner key authorizes an agent key to publish under **its own authorship**. The identity primitive. |
| **NIP-AA** Agent Authentication | 22242 | An agent presents its NIP-OA credential during NIP-42 AUTH; if the owner is an active relay member, the agent gets **virtual membership** without being separately enrolled. |
| **NIP-AE** Agent Engrams | 30174 | Addressable, NIP-44-encrypted persistent agent memory on the relay. Owner can always decrypt. |
| **NIP-AM** Agent Turn Metrics | 44200 | One encrypted event per turn recording token usage and estimated cost, readable only by the owner. |
| **NIP-AO** Agent Observability | ephemeral | Encrypted streaming of session telemetry to the owner's desktop client. |
| **NIP-AP** Agent Personas | 30175, 30178 | Public addressable "blueprint" for instantiating an agent: display name, avatar, system prompt, model, runtime, name pool. |
| **NIP-GS** Git Object Signing | — | Sign git commits and tags with Nostr secp256k1 keys via git's `gpg.x509.program` hook. |
| **NIP-MP** Multi-Repository Projects | 30621 | A named grouping of NIP-34 repo announcements (`30617`); spans repos across owners. |
| **NIP-CW** Channel Window | — | Relay-computed, cursor-paged view of a channel's top-level timeline. Backfill without unbounded scans. |

Others (NIP-DV DM visibility, NIP-ER reminders, NIP-IA identity archival, NIP-PL push leases, NIP-RS read-state sync, NIP-WP workspace profile) are client-experience concerns with no agent requirement.

**NIP-OA in detail** (this is the crux of identity, so it is specified here rather than summarized):

```json
["auth", "<owner-pubkey-hex>", "<conditions>", "<sig-hex>"]
```

- Exactly four elements; more or fewer is malformed and MUST be rejected. More than one `auth` tag on an event ⇒ treat as having none.
- Signing preimage is the UTF-8 bytes of `nostr:agent-auth:` ‖ `event.pubkey` ‖ `:` ‖ `<conditions>`. The signed message is `SHA256(preimage)`. `<sig-hex>` is a BIP-340 Schnorr signature by the owner's secret key.
- `<conditions>` is empty or `clause[&clause…]` where each clause is `kind=<decimal>`, `created_at<<ts>`, or `created_at><ts>`. No whitespace, no leading/trailing/double `&`, canonical base-10.
- The conditions string **must not be reordered, deduplicated, or normalized** before computing the preimage — clause order is part of the signed message.
- `owner-pubkey == event.pubkey` ⇒ invalid.
- The tag is a **reusable capability**: the same tag may appear on many events by the same agent key, provided each satisfies the conditions.
- The event remains authored by `event.pubkey`. Relays MUST NOT rewrite authorship; clients MUST NOT show the owner as author. This is explicitly *not* NIP-26 delegation.
- Compromise of the agent key does not imply compromise of the owner key. `created_at<` clauses bound authorization but are **not wall-clock expiry** — a misbehaving agent can backdate `created_at`. Freshness must be enforced independently.
- The NIP ships test vectors (owner secret `0x…01`, agent secret `0x…02`) that our implementation should assert against directly.

**NIP-AA flow:** relay sends `["AUTH", "<challenge>"]`; agent builds a `kind:22242` event with `["relay", …]`, `["challenge", …]`, and the `["auth", …]` tag, signs with the *agent* key, and sends it back. Relay verifies NIP-42, then the auth tag, then that the owner is an active member. `created_at` must be within a relay-defined freshness window (**±120s RECOMMENDED**). Step-1 failures return `"invalid: <reason>"`; credential/membership failures return `"restricted: <reason>"`. Once AUTH succeeds, the `auth` tag on subsequent events is **not required for relay access** — only for provenance.

### How Buzz expects agents to connect today

Buzz ships `buzz-acp`, a harness whose topology is:

```
Buzz Relay ──WS──→ buzz-acp ──stdio──→ Your Agent
                                          │
                                     Buzz CLI
                                  (send_message, …)
```

`buzz-acp` **owns the loop**: it authenticates to the relay, discovers channels (`GET /api/channels?member=true`), listens for @mentions, prompts the agent over [Agent Client Protocol](https://agentclientprotocol.com/) on stdio, enforces `BUZZ_ACP_IDLE_TIMEOUT` (620s) and `BUZZ_ACP_MAX_TURN_DURATION` (7200s), and the agent replies by shelling out to `buzz-cli`. It supports goose, Codex (via `codex-acp`), and Claude Code (via `claude-agent-acp`).

Key operational facts: each agent needs its **own** keypair (`buzz-admin generate-key`; the secret is printed once and unrecoverable) and must be registered as a relay member (`buzz-admin add-member --pubkey …`, which publishes `kind:13534` and therefore needs a stable `BUZZ_RELAY_PRIVATE_KEY` on the relay). Configuration is entirely environment-driven: `BUZZ_PRIVATE_KEY` (nsec), `BUZZ_RELAY_URL` (default `ws://localhost:3000`), `BUZZ_API_TOKEN`.

**Known gap in Buzz itself:** private channels require explicit membership, and the relay has **no REST or event API for managing channel members yet**. The documented workaround is `create_channel` via the Buzz CLI, where the creator is auto-added.

### The remote-agent contract

`docs/remote-agents.md` defines how a Buzz agent runs on remote substrate. The provider contract (`buzz-backend-<id>` binaries, `info`/`deploy` ops) is Buzz-desktop-specific and not something BaoBot implements — but its **invariants apply to any long-running Buzz agent** and directly constrain our ECS deployment:

- **I1 Identity fail-closed** — never launch without a valid private key.
- **I2 No secrets in configuration** — credentials flow in the deploy payload, never persisted config. Reserved keys (`BUZZ_PRIVATE_KEY`, `NOSTR_PRIVATE_KEY`, `BUZZ_AUTH_TAG`, `BUZZ_RELAY_URL`) are stripped from user env before merge.
- **I3 Presence is status** — liveness is derived *exclusively* from relay presence (kind:20001), with a **180-second staleness bound**. No substrate telemetry.
- **I4 At-most-one-live-instance** — one running instance per pubkey per namespace.
- **I5 Intentional termination is final** — clean exits are not resurrected.

Also relevant: the inbound author gate (`respond_to`, `respond_to_allowlist`) and the `!shutdown` relay message as the stop signal.

### Ecosystem: language and library reality

Every Buzz crate is **Rust** — `buzz-sdk`, `buzz-cli`, `buzz-acp`, `buzz-agent`, `buzz-core`, `buzz-ws-client`, `buzz-workflow`, `buzz-persona`, `git-sign-nostr`, `git-credential-nostr`, `buzz-conformance`. **There is no Go SDK.**

The Go Nostr situation was verified empirically, not from search results:

- `github.com/nbd-wtf/go-nostr` was **archived 2026-01-24** and its README redirects to `fiatjaf.com/nostr`.
- `fiatjaf.com/nostr@master` (resolved to `v0.0.0-20260731140316-a8080728893f`, i.e. four days old) was fetched into the module cache and inspected. It ships **`nip29`, `nip42`, `nip44`, `nip17`, `nip34`, `nip59`, `nip70`, `nip19`, `nip49`**, plus `relay.go`, `pool.go`, `keyer/` (plain, encrypted, bunker, readonly), `eventstore/`, and `khatru`.

Concretely: `nip29` exposes `Group`, `GroupAddress`, `NewGroup`, `NewGroupFromMetadataEvent`, `ToMetadataEvent`/`ToAdminsEvent`/`ToMembersEvent`, `MergeIn*Event`, and moderation actions. `nip42` exposes `CreateUnsignedAuthEvent(challenge, pubkey, relayURL)` and `ValidateAuthEvent`. Its dependency tree already includes BIP-340 Schnorr, which is exactly what NIP-OA attestation verification needs.

**Every P0 primitive we need exists in maintained Go today.** The only piece with no upstream Go implementation is NIP-OA/NIP-AA attestation — roughly 150 lines of preimage construction plus Schnarr sign/verify, with published test vectors.

---

## Code Analysis — Current State of BaoBot

### The channel seam

`domain.ChannelMonitor` is minimal:

```go
type ChannelMonitor interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}
```

`slack.Monitor` (`internal/infrastructure/slack/monitor.go`, 238 lines) is the only implementation and establishes the pattern a Buzz adapter should follow:

1. `Start` spawns a goroutine draining an event channel; `Stop` relies on context cancellation.
2. Inbound events are filtered (DMs and `@mentions` only; bot-authored messages skipped to avoid loops).
3. `dispatch` mints a `taskID`, marshals a `domain.TaskPayload`, and sends a `domain.Message{Type: MessageTypeTask, From: "slack", To: cfg.BotName}` onto `domain.MessageQueue`.
4. A `pending map[string]replyTarget` correlates `taskID` → reply coordinates, guarded by a mutex.
5. `HandleResult(ctx, domain.TaskResultPayload)` pops the pending entry and posts the reply back to the origin thread.

**A Buzz adapter maps onto this cleanly**: `#h` channel UUID + root event id replaces `{channelID, threadTS}`; `kind:9` publication replaces `PostMessageContext`.

### The blocker: `TeamManager` is hardcoded to one concrete Slack type

`internal/application/team/team_manager.go` imports `internal/infrastructure/slack` directly and stores a concrete pointer:

```go
slackMonitor *slackinfra.Monitor                     // field, line ~133
func (tm *TeamManager) WithSlackMonitor(m *slackinfra.Monitor)   // line ~203
```

Result forwarding is single-monitor and branchy — an orchestrator-mode path (`~line 923`) and a non-orchestrator path (`~line 930`), each doing `if tm.slackMonitor != nil { … HandleResult(…) }`.

This is a **Clean Architecture violation already present in the codebase**: the application layer imports infrastructure, in contradiction of `AGENTS.md` ("Dependencies point inward only"). Adding a second channel makes it a blocker rather than a wart — Buzz cannot be attached without either duplicating the branch or fixing the seam. **This PRD treats the fix as in-scope prerequisite work.**

### Documentation drift found during analysis

`boabot/AGENTS.md` documents `infrastructure/teams/  # Microsoft Teams adapter` and the root `AGENTS.md` lists Teams as a supported channel. **The directory does not exist.** No Teams adapter has been written. This should be corrected in the same change so the docs describe reality.

### What BaoBot already has that maps onto Buzz concepts

| BaoBot capability | Buzz equivalent | Note |
|---|---|---|
| `MemoryStore` + `VectorStore` (local fs, BM25/cosine) | NIP-AE engrams (`kind:30174`) | Overlapping, not identical. Ours is richer locally; theirs is portable and owner-readable. |
| `BudgetTracker` (tokens/tool calls, `budget.json`) | NIP-AM turn metrics (`kind:44200`) | We already compute the numbers; publishing them is a serialization task. |
| `SOUL.md` + `config.yaml` + `AgentCard` | NIP-AP personas (`kind:30175`) | Direct conceptual match. A persona is a publishable projection of our existing identity docs. |
| `receive_from` allowlist | `respond_to` / `respond_to_allowlist` | Same idea, different key space (bot names vs. pubkeys). |
| GitHub PR flow | NIP-34 patches + NIP-GS signing + NIP-MP projects | Substantially different trust model — see Risks. |
| Calibrated autonomy gates (Advisory/Validating/Blocking/Escalating) | — | No Buzz equivalent; ours must remain authoritative. |

---

## Architecture Decision

Three integration paths were evaluated. **We recommend Option A.**

### Option A — Native Go Nostr client as a `ChannelMonitor` adapter ✅ **Recommended**

A new `internal/infrastructure/buzz/` package implements `domain.ChannelMonitor` over `fiatjaf.com/nostr`, connecting directly to `buzz-relay` by WebSocket, authenticating with NIP-42 + NIP-OA, and publishing/subscribing `kind:9` scoped by `#h`.

- BaoBot keeps its own turn loop, worker harness, budget caps, tool gating, memory, and autonomy gates.
- Single binary; no Rust toolchain, no Node, no sidecar in the ECS task.
- Buzz becomes one more channel alongside Slack, not a new runtime.
- Verified: every P0 NIP has a maintained Go implementation.
- Cost: we implement NIP-OA attestation ourselves (~150 LOC + test vectors) and own protocol-drift risk against a fast-moving draft spec.

### Option B — Run under `buzz-acp` ❌ **Rejected**

BaoBot would expose an ACP stdio interface and be spawned as a subprocess of `buzz-acp`.

Rejected because **it inverts control.** `buzz-acp` owns channel discovery, mention filtering, the turn loop, idle and wall-clock turn timeouts, and reply dispatch. BaoBot's `TeamManager`, worker supervision, `BudgetTracker`, Tool Attention gating, and calibrated-autonomy gates would all sit *below* a harness that does not know about them — and turn cancellation would happen outside our budget accounting. It also mandates a Rust binary plus `buzz-cli` in every ECS task purely to relay messages. We would be adopting Buzz's agent runtime, not adding Buzz support to ours.

*(Worth noting for a different purpose: `buzz-acp` is the right tool if we ever want to expose a BaoBot **persona** to a Buzz workspace without our cluster — but that is a distribution question, not this integration.)*

### Option C — Shell out to `buzz-cli` per message ❌ **Rejected**

A subprocess-per-send adapter. Rejected: no persistent WebSocket means no live subscription, so inbound requires polling; process-per-message is a poor fit for presence (`20001`) and typing (`20002`) which are inherently streaming; it still ships a Rust binary; and it puts the agent's nsec on a command line or in subprocess env on every call, which conflicts with invariant I2 and with our own skill-script credential-stripping posture.

### Layering consequence

Nostr types must **not** leak inward. A domain-level port —

```go
// domain/buzz.go (illustrative)
type RelayClient interface {
    Connect(ctx context.Context) error
    Authenticate(ctx context.Context) error
    Publish(ctx context.Context, evt Event) error
    Subscribe(ctx context.Context, f Filter) (<-chan Event, error)
    Close() error
}
```

— keeps `fiatjaf.com/nostr` imports confined to `internal/infrastructure/buzz/`, makes the adapter mockable, and keeps the 90% domain/application coverage target reachable.

---

## Goals

- **G1** — BaoBot joins a Buzz workspace as a first-class agent member with its own secp256k1 keypair, reading and writing channel messages under its own authorship.
- **G2** — Buzz is added as a peer channel to Slack behind the existing `ChannelMonitor` seam, with no change to the worker harness, budget, memory, or autonomy subsystems.
- **G3** — Agent identity is owner-attested (NIP-OA/NIP-AA) so BaoBot's provenance is cryptographically verifiable by any Buzz client, and revoking the owner's membership revokes the agent's access.
- **G4** — The channel seam is corrected to depend on a domain interface rather than a concrete Slack type, so Buzz (and any future channel) attaches without further application-layer surgery.

## Non-Goals

- **NG1** — Running BaoBot under `buzz-acp` or exposing an ACP stdio interface. (Option B, rejected above.)
- **NG2** — Hosting or operating a Buzz relay. We are a client; relay operation is the workspace owner's concern.
- **NG3** — Replacing BaoBot's memory subsystem with NIP-AE engrams. Engram publication is a P2 *projection* of existing memory, not a migration.
- **NG4** — Replacing the GitHub PR workflow with NIP-34 git-on-Nostr. Repo/patch support is explicitly deferred (see Phasing).
- **NG5** — Implementing Buzz client-experience NIPs: NIP-DV, NIP-ER, NIP-IA, NIP-PL, NIP-RS, NIP-WP.
- **NG6** — Canvases, voice huddles (NIP-53/LiveKit), Blossom media upload, and `kind:40002`/`40003` rich content and edits.
- **NG7** — Multi-workspace / multi-relay federation. One bot binds to one Buzz community per deployment.
- **NG8** — Removing or deprecating the Slack channel.

---

## Functional Requirements

### Identity and authentication

**FR-001:** Each BaoBot instance MUST hold its own secp256k1 keypair. Keys are generated out-of-band (`buzz-admin generate-key` or an equivalent Go key generator) and are never shared between bots.

**FR-002:** The agent secret key (nsec) MUST be resolved at startup from AWS Secrets Manager via the bot's IAM role, using the existing `internal/infrastructure/credentials` loader. It MUST NOT appear in `config.yaml`, in `team.yaml`, in any committed file, in container image layers, or in any log line at any level.

**FR-003:** BaoBot MUST fail closed on identity: if the private key is missing, malformed, or fails to derive the expected pubkey, the Buzz monitor MUST NOT start, and the failure MUST be logged as an error. A bot with Buzz misconfigured MUST still start with all other channels functioning.

**FR-004:** BaoBot MUST perform NIP-42 authentication against the relay: on receiving `["AUTH", "<challenge>"]`, build and sign a `kind:22242` event carrying `["relay", "<url>"]` and `["challenge", "<nonce>"]`, and send `["AUTH", <event>]`.

**FR-005:** When a NIP-OA `auth` tag is configured, BaoBot MUST include it on the `kind:22242` AUTH event per NIP-AA, so the relay grants virtual membership derived from the owner's membership.

**FR-006:** BaoBot MUST construct the NIP-OA preimage as exactly `nostr:agent-auth:` ‖ `event.pubkey` ‖ `:` ‖ `<conditions>`, hash it with SHA-256, and treat the tag as `["auth", owner-pubkey-hex, conditions, sig-hex]` with exactly four elements. The `<conditions>` string MUST be used verbatim — never reordered, deduplicated, normalized, or canonicalized.

**FR-007:** BaoBot MUST validate a configured `auth` tag locally at startup before use: verify the owner Schnorr signature over the preimage, reject `owner-pubkey == agent-pubkey`, reject malformed clause syntax (whitespace, leading/trailing/doubled `&`, non-canonical decimals, out-of-range values), and reject a tag with any element count other than four. Validation MUST be asserted against the published NIP-OA test vectors in unit tests.

**FR-008:** BaoBot MUST set its `created_at` on AUTH events from current wall-clock UTC so it falls inside the relay's freshness window (±120s RECOMMENDED). Clock skew beyond that window MUST surface as a distinguishable error, not a generic auth failure.

**FR-009:** BaoBot MUST distinguish relay auth failure classes in logs and metrics: `"invalid: …"` (malformed AUTH event, bad signature, wrong relay, stale timestamp) versus `"restricted: …"` (missing/invalid credential, non-member owner). These have different operator remedies and MUST NOT be collapsed.

**FR-010:** BaoBot MUST support optional `BUZZ_API_TOKEN`-style token authentication for relays configured with `BUZZ_REQUIRE_AUTH_TOKEN=true`, resolved from Secrets Manager on the same path as FR-002.

**FR-011:** BaoBot MUST publish a `kind:0` profile metadata event on first successful connection, populated from the bot's existing identity (name, bot type, and the description from its `AGENTS.md`), so humans in the workspace see a named agent rather than a bare pubkey.

### Channel participation

**FR-012:** BaoBot MUST connect to the configured relay over WebSocket, MUST reconnect with bounded exponential backoff and jitter on disconnect, and MUST re-authenticate and re-establish all subscriptions after every reconnect.

**FR-013:** BaoBot MUST discover the channels it is a member of. Discovery MUST use the relay's member-scoped channel listing (`GET /api/channels?member=true`) with a subscription-based fallback over `39000`/`39002` discovery events, and MUST tolerate the endpoint being absent or renamed without crashing the monitor.

**FR-014:** BaoBot MUST subscribe to `kind:9` messages for each channel it participates in, with the subscription filter scoped by `#h` to the channel UUID.

**FR-015:** BaoBot MUST auto-subscribe to newly joined channels by subscribing to `kind:44100` membership-added events filtered by `#p` matching its own authenticated pubkey, and MUST unsubscribe on `kind:44101`.

**FR-016:** Every subscription for a p-gated kind (`44100`, `44101`, `1059`) MUST include a `#p` filter whose values all equal the authenticated pubkey. A subscription that would violate this MUST be rejected in our own code before being sent to the relay, with a clear error.

**FR-017:** BaoBot MUST publish channel messages as `kind:9` events carrying the `#h` tag with the target channel UUID, signed by the agent key.

**FR-018:** BaoBot MUST reply in-thread using NIP-10 reply tags, referencing the root event of the conversation it was mentioned in.

**FR-019:** BaoBot MUST treat an inbound event as a task trigger only when it is an @mention of the agent's own pubkey, or a message in a DM conversation with the agent. All other channel traffic MUST be ignored for dispatch purposes.

**FR-020:** BaoBot MUST ignore events authored by its own pubkey to prevent self-trigger loops, matching the existing Slack adapter's bot-message filter.

**FR-021:** On a qualifying inbound event, BaoBot MUST mint a task ID, construct a `domain.TaskPayload`, and enqueue a `domain.Message{Type: MessageTypeTask, From: "buzz", …}` onto the existing `domain.MessageQueue` — identical in shape to the Slack path.

**FR-022:** BaoBot MUST correlate task results back to their originating channel and thread via a pending map keyed by task ID, and on `HandleResult` MUST publish the output as a `kind:9` reply in the correct channel and thread. Unmatched task IDs MUST be ignored silently (a result from another channel).

**FR-023:** BaoBot MUST publish `kind:20001` presence events (`online`/`away`/`offline`, ≤128 characters) representing **conversational availability**, refreshed frequently enough to stay inside the 180-second staleness bound. Presence MUST NOT be derived from process or infrastructure health.

**FR-024:** BaoBot MUST publish `kind:20001` `offline` presence and close the relay connection cleanly during graceful shutdown, before the existing shutdown path completes.

**FR-025:** BaoBot SHOULD publish `kind:20002` typing indicators while a triggered task is executing, so humans see the agent is working rather than silent.

**FR-026:** BaoBot MUST honour the `!shutdown` relay message as a stop signal from an authorized sender, routing it through the existing graceful shutdown path.

**FR-027:** Any reaction subscription MUST be `{"kinds":[7],"#h":["<channel-uuid>"]}`. A kinds-only reaction subscription MUST NOT be used, because the relay silently delivers nothing.

### Security and safety

**FR-028:** All inbound Buzz content — message bodies, profile metadata, channel names and topics — MUST be routed through the existing prompt-injection sanitisation path applied to untrusted tool output and external messages, and MUST be treated as untrusted regardless of author.

**FR-029:** BaoBot MUST enforce an inbound author gate: an optional `respond_to` pubkey and an optional `respond_to_allowlist` of pubkeys. When set, mentions from pubkeys outside the gate MUST be ignored and the rejection MUST be logged. This gate maps onto the existing `receive_from` concept in the pubkey key space.

**FR-030:** Buzz-triggered tasks MUST be subject to the existing per-bot budget caps (daily tokens, hourly tool calls) with no separate accounting path, and MUST be subject to the existing calibrated-autonomy gates. Buzz message origin MUST NOT downgrade any gate.

**FR-031:** BaoBot MUST enforce at-most-one-live-instance per pubkey (invariant I4). Given ECS rolling deployments, the design MUST make this true rather than assume it — see Open Question OQ-1 for the decision required.

**FR-032:** The agent pubkey, relay URL, channel UUID, and event ID MUST appear in structured logs for every dispatched task and published reply, so relay-side audit records reconcile with our logs. The private key MUST never appear.

### Wiring and configuration

**FR-033:** `TeamManager` MUST hold a collection of channel monitors typed against a domain-level interface rather than the concrete `*slackinfra.Monitor`, and the `internal/application/team` package MUST NOT import `internal/infrastructure/slack` or `internal/infrastructure/buzz`.

**FR-034:** Task-result forwarding MUST iterate all registered monitors rather than branching on a single Slack field, in both the orchestrator and non-orchestrator paths. Existing Slack behaviour MUST be preserved exactly.

**FR-035:** Buzz configuration MUST live under a `buzz:` block in `config.yaml` carrying non-secret settings only: `enabled`, `relay_url`, `bot_name`, `owner_pubkey`, `respond_to`, `respond_to_allowlist`, `channels`, `presence_interval`. Secret material (nsec, auth tag, API token) MUST be referenced by Secrets Manager ARN, never inlined.

**FR-036:** The Buzz monitor MUST activate only when `buzz.enabled` is true and all required settings resolve, mirroring the Slack monitor's all-or-nothing activation. With Buzz disabled, no Nostr code path may execute and no relay connection may be attempted.

**FR-037:** `boabot/AGENTS.md` and the root `AGENTS.md` MUST be corrected to remove the non-existent Microsoft Teams adapter and to document the Buzz adapter in its place.

---

## Non-Functional Requirements

- **Performance:** Time from relay delivery of a qualifying mention to task enqueue MUST be under 500 ms at p95, excluding model inference. Reply publication after `HandleResult` MUST be under 1 s at p95.
- **Reliability:** The monitor MUST survive relay restarts and network partitions without operator intervention, reconnecting with bounded backoff and jitter. A Buzz outage MUST NOT degrade Slack, SQS, or scheduled-task processing. Reconnect MUST NOT lose the pending task-ID correlation map.
- **Security:** Private keys resolve from Secrets Manager only (FR-002), never logged, never in config, never on a command line. Inbound content is untrusted (FR-028). Author gating enforced before dispatch (FR-029). NIP-GS git signing, if ever enabled, sits behind an **Escalating** autonomy gate — an agent key that can sign commits is a materially larger blast radius than one that can post messages.
- **Observability:** Structured logs for connect, authenticate, subscribe, dispatch, publish, and reconnect. Metrics for connection state, auth failures split by `invalid`/`restricted` class, events received/published by kind, dispatch latency, and reconnect count. Presence staleness MUST be observable so I3 violations are detectable.
- **Maintainability:** `fiatjaf.com/nostr` imports confined to `internal/infrastructure/buzz/`. Domain and application layers MUST have zero Nostr imports. Buzz draft NIPs are moving targets — the version of `block/buzz` validated against MUST be recorded in `docs/architectural-decision-record.md`.
- **Testing:** TDD throughout, per `AGENTS.md`. 90%+ coverage on domain and application layers. Adapter integration tests tagged `//go:build integration` run against a real relay started from Buzz's own `docker-compose.yml`. NIP-OA implementation MUST assert against the published test vectors.
- **Deployment:** No Rust toolchain, no Node runtime, no sidecar container. Single Go binary, unchanged image build.

---

## Acceptance Criteria

- [ ] A BaoBot instance connects to a locally-run `buzz-relay` (Buzz's `docker-compose.yml`), authenticates via NIP-42, and appears as an online member in the Buzz desktop client.
- [ ] The bot's `kind:0` profile renders its name and description in the Buzz client, not a bare pubkey.
- [ ] A human @mentions the bot in a channel; the bot dispatches a task through the existing queue, executes it in the normal worker harness, and posts the result as an in-thread `kind:9` reply.
- [ ] A NIP-OA `auth` tag issued by an owner key grants the agent relay access via NIP-AA **without** the agent being explicitly enrolled as a relay member.
- [ ] Revoking the owner's relay membership causes the agent's next connection attempt to fail with a `restricted:` error, verified end-to-end.
- [ ] NIP-OA sign and verify pass against the published test vectors, including negative cases: five-element tag, two `auth` tags, `owner == agent` pubkey, whitespace in conditions, leading/trailing/doubled `&`, non-canonical decimal, out-of-range `kind`, reordered conditions string.
- [ ] A subscription request for a p-gated kind without a matching `#p` filter is rejected by our own code before reaching the relay, with a test asserting the error.
- [ ] A reaction subscription is asserted by test to carry `#h`; a kinds-only reaction subscription is asserted to be rejected.
- [ ] Relay restart mid-session: the bot reconnects, re-authenticates, re-subscribes, and answers a mention sent after recovery — with no operator action and no lost pending correlations.
- [ ] Presence: the bot publishes `kind:20001` at an interval under the 180-second staleness bound; on `SIGTERM` it publishes `offline` and closes cleanly before exit.
- [ ] A mention from a pubkey outside `respond_to_allowlist` is ignored, and the rejection appears in structured logs.
- [ ] A Buzz-triggered task that exceeds the bot's daily token cap is refused by the existing `BudgetTracker` with no Buzz-specific bypass.
- [ ] Inbound message content containing prompt-injection patterns is sanitised on the same path as MCP tool output, verified by test.
- [ ] With `buzz.enabled: false`, no relay connection is attempted and no Nostr code path executes; Slack behaviour is byte-identical to today.
- [ ] `go build`, `go vet`, and `golangci-lint run` pass; `go test -race ./...` passes; domain and application coverage is ≥90% and has not regressed.
- [ ] `grep -r "fiatjaf.com/nostr" internal/domain internal/application` returns no matches.
- [ ] `grep -r "infrastructure/slack\|infrastructure/buzz" internal/application` returns no matches.
- [ ] The agent's private key does not appear in any log output at any level, verified by test against a captured log buffer.
- [ ] `docs/technical-details.md`, `docs/product-details.md`, and `docs/architectural-decision-record.md` are updated; the ADR entry records the Option A/B/C decision and the `block/buzz` commit validated against.
- [ ] `boabot/AGENTS.md` and root `AGENTS.md` no longer claim a Microsoft Teams adapter.

---

## Phasing

**P0 — Presence in the workspace** (FR-001 → FR-024, FR-028 → FR-037)
Keypair identity, NIP-OA attestation, NIP-42/NIP-AA auth, `kind:9` read/write with `#h` scoping, thread replies, mention gate, presence, graceful shutdown, the `ChannelMonitor` seam fix, config, and docs. **This is the shippable unit** — after P0, BaoBot is a working Buzz teammate.

**P1 — Conversational polish**
Typing indicators (FR-025), `!shutdown` handling (FR-026), reaction publishing and subscription (FR-027), NIP-50 search over channel history, NIP-CW channel-window paging for bounded backfill, NIP-17 gift-wrapped DMs.

**P2 — Agent-native Buzz features**
NIP-AP persona publication (`kind:30175`) projected from `SOUL.md` + `AgentCard`; NIP-AM turn metrics (`kind:44200`) projected from `BudgetTracker`; NIP-AE engram publication (`kind:30174`) as an encrypted projection of existing memory. Each is a projection of something BaoBot already computes.

**Deferred, not scheduled**
NIP-GS git signing, NIP-34 patches, NIP-MP projects, NIP-AO observability, Blossom media, canvases, voice, `kind:40002`/`40003` rich content. Git-on-Nostr in particular is a trust-model change, not a feature increment — it warrants its own PRD.

---

## Dependencies and Risks

| Item | Type | Notes |
|---|---|---|
| `fiatjaf.com/nostr@master` | Dependency | Verified to ship `nip29`, `nip42`, `nip44`, `nip17`, `nip34`, `nip70`. Pseudo-versioned, no stable tag — pin the exact pseudo-version in `go.mod` and record it in the ADR. |
| `nbd-wtf/go-nostr` archived 2026-01-24 | Risk | Do **not** use it. The successor is the only maintained path; if it stalls, we own a fork. Mitigation: our surface area is one adapter package behind a domain port. |
| Buzz NIPs are `draft` | Risk | All 17 Buzz-specific NIPs are marked `draft` `optional`. Breaking changes are likely. Mitigation: pin and record the validated `block/buzz` commit; run Buzz's own conformance suite (`crates/buzz-conformance`) in CI against the pinned relay image. |
| Relay has no channel-membership API | Dependency | Buzz's own docs call this a "known gap": private channels require explicit membership but there is no REST/event API to manage it. Our bot must be added to private channels out-of-band, or create its own channels. P0 must not depend on programmatic membership management. |
| Invariant I4 vs. ECS rolling deploys | Risk | Two tasks sharing one nsec means duplicate presence and **duplicate replies to every mention** — user-visible and embarrassing. Blocking design decision; see OQ-1. |
| nsec custody and rotation | Risk | The secret is printed once by `buzz-admin generate-key` and is unrecoverable. Loss means a new identity and loss of the pubkey's history and reputation, which are community-scoped and non-portable. Mitigation: generate into Secrets Manager directly; document rotation as an identity-replacement procedure, not a key swap. |
| Agent key compromise | Risk | A compromised agent key can post as the agent anywhere the owner is a member. NIP-OA bounds this (owner key stays safe) but `created_at<` clauses are **not wall-clock expiry** — a misbehaving agent can backdate. Mitigation: short-lived attestations, independent freshness checks, and owner-side membership revocation as the real kill switch. |
| Silent-failure subscription semantics | Risk | P-gated filters and reaction `#h` derivation both fail by delivering nothing rather than erroring. Mitigation: FR-016 and FR-027 make these compile-time-ish guards in our own code with explicit tests. |
| `TeamManager` refactor blast radius | Risk | `team_manager.go` is large and the result-handler wiring has two paths. Regression risk to Slack. Mitigation: refactor is its own commit with Slack tests green before any Buzz code lands. |
| No Go SDK from Block | Risk | Every Buzz crate is Rust; we track the wire protocol, not a vendored SDK. Mitigation: this is inherent to Option A and priced in — the wire protocol is the documented, tested-against-third-party-clients surface. |
| Relay operational dependency | Dependency | A relay must exist. Self-hosted (`docker-compose.yml`, Railway one-click) or Block's managed service. Deployment topology and whether the relay is in-VPC is unresolved — see OQ-3. |
| Prompt injection via public workspace | Risk | Buzz channels may include actors outside our organisation. Every message is model input. Mitigation: FR-028 sanitisation plus FR-029 author gating, and no relaxation of autonomy gates for Buzz-origin tasks. |

---

## Open Questions

- **OQ-1 (blocking — decide before implementation):** How is invariant I4 (at-most-one-live-instance per pubkey) satisfied under ECS rolling deployment? Three candidates: (a) drain-before-start deployment so the old task publishes `offline` and closes before the new one authenticates; (b) a distinct keypair per running instance, accepting multiple agent identities per logical bot; (c) relay-side or client-side reply deduplication by task ID. Option (a) is cleanest but requires a deployment-strategy change; (b) fragments identity and reputation, which are community-scoped and non-portable.
- **OQ-2:** Who holds the **owner** key that issues NIP-OA attestations, and what is the issuance workflow? An operator laptop, a `boabotctl` subcommand, or an orchestrator-held key? This determines whether attestation renewal is manual or automatable, and it is the root of the whole trust chain.
- **OQ-3:** Self-hosted relay or Block's managed service? If self-hosted, does it run inside our VPC, and does that change the security-group and egress posture for ECS tasks?
- **OQ-4:** Which bots get Buzz identities — every bot in `team.yaml`, or only the orchestrator acting as the team's single front door? Per-bot identities are more faithful to Buzz's model and give better audit granularity; a single front door is far less key material to custody.
- **OQ-5:** Do we need `boabotctl` subcommands for Buzz key generation, attestation issuance, and channel join, so operators are not required to build Rust `buzz-admin` locally? Likely yes for adoption, but it is additional scope not currently costed in P0.
- **OQ-6:** Should NIP-AE engrams replace, mirror, or merely export our memory subsystem? Deferred to P2, but the answer affects whether P0's memory writes need any Buzz-shaped metadata from the start.

---

## Appendix — Sources

Protocol and ecosystem facts in this PRD were taken from primary sources on 2026-08-04:

- [github.com/block/buzz](https://github.com/block/buzz) — `README.md`, `NOSTR.md`, `VISION_REMOTE_AGENTS.md`, `docs/remote-agents.md`, `docs/nips/NIP-OA.md`, `docs/nips/NIP-AA.md`, `docs/nips/` index, `crates/` listing, `crates/buzz-acp/README.md`
- [fiatjaf.com/nostr](https://pkg.go.dev/fiatjaf.com/nostr) — package layout inspected directly in the Go module cache at `v0.0.0-20260731140316-a8080728893f`
- [nbd-wtf/go-nostr](https://github.com/nbd-wtf/go-nostr) — archive notice and successor pointer
- [The New Stack](https://thenewstack.io/block-buzz-agent-workspace/), [Decrypt](https://decrypt.co/374026/jack-dorseys-block-launches-buzz-a-nostr-based-slack-and-github-rival-for-ai-agents), [Crypto Briefing](https://cryptobriefing.com/block-launches-buzz-nostr-workspace/) — launch context
- [Agent Client Protocol](https://agentclientprotocol.com/) — the stdio protocol `buzz-acp` speaks

Codebase facts were taken from this repository at commit `32bc921`.
