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

### Secret storage

Joining Buzz means BaoBot holds a new category of secret — the agent's Nostr private key, which *is* its identity — on top of secrets it already holds: `ANTHROPIC_API_KEY`, `BOABOT_BACKUP_TOKEN`, and Slack's `bot_token`/`app_token`. Today they land in two places:

| Secret | Where it lives today |
|---|---|
| `ANTHROPIC_API_KEY` | env var, or `~/.boabot/credentials` (INI, mode-checked) |
| `BOABOT_BACKUP_TOKEN` | env var, or `~/.boabot/credentials` |
| Slack `bot_token`, `app_token` | **`config.yaml`, in plaintext** (`SlackConfig`, `internal/infrastructure/config/config.go:23-27`) — no env or credentials-file path exists |
| Buzz nsec (this PRD) | env var, or `~/.boabot/credentials` |

Two problems follow.

1. **`config.yaml` holds plaintext secrets.** Slack tokens have no resolution path other than the config file. That file sits next to the binary, is not mode-checked, and is the file most likely to be copied, shared, or committed by accident.
2. **A dotfile is the strongest control we offer.** `credentials.Load` refuses to start on a world-readable file (`mode & 0o004`), which is a real control — but it is still plaintext at rest, readable by any process running as that user, and captured by any backup or sync tool pointed at `$HOME`.

Every desktop OS ships an encrypted-at-rest credential store. We should use it. The complication — and the reason this needs a design rather than a library call — is that **BaoBot must also run unattended as a service**, and that is precisely the mode where OS keystores stop behaving uniformly. This PRD specifies both: joining Buzz, and the secret-storage upgrade that Buzz's own identity secret motivates.

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

**What virtual membership does and does not grant** (NIP-AA, "Virtual Member Privileges" — this determines what BaoBot can actually do once authenticated):

- A virtual member **may pass relay-level membership checks for both read (subscriptions) and write (event publishing)**. Publishing community-global events such as `kind:0` profile metadata is therefore permitted without explicit enrollment.
- Virtual membership **does not** grant the owner's channel memberships, group roles, or administrative privileges. Channel-level, group-level, quota, and role checks evaluate **the agent's own pubkey**. BaoBot still needs its own membership in each private channel.
- Virtual members MUST NOT hold relay administration privileges and MUST NOT modify relay membership.
- Virtual membership is **re-checked on every new connection and never cached across reconnects** — so owner revocation takes effect at the agent's next reconnect, with no separate cleanup.
- ⚠️ **Credential scope warning (security-relevant).** An `auth` tag presented at NIP-42 authentication grants **connection-level full relay read and write access regardless of any `kind=` clauses**, unless the relay implements optional per-event enforcement. A narrowly-scoped credential does *not* imply narrowly-scoped relay access. The credential is also **not bound to a specific relay** — the same tag admits the agent to any relay implementing NIP-AA, and a tag issued purely for provenance is equally valid for relay admission. `created_at<` is the only practical scoping tool.
- Relays SHOULD aggregate rate limits and quotas **by owner pubkey** across all virtual members derived from that owner — so N BaoBots sharing one owner key may contend for one shared quota pool.

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

`docs/remote-agents.md` defines how a Buzz agent runs on remote substrate. The provider contract (`buzz-backend-<id>` binaries, `info`/`deploy` ops) is Buzz-desktop-specific and not something BaoBot implements — but its **invariants apply to any long-running Buzz agent**, including a locally-run single binary, and so constrain us regardless of where boabot runs:

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

**Every P0 primitive we need exists in maintained Go today.** The only piece with no upstream Go implementation is NIP-OA/NIP-AA attestation — roughly 150 lines of preimage construction plus Schnorr sign/verify, with published test vectors.

---

## Background — Secret Storage: What Each OS Actually Supports

The interactive case is easy and uniform. The service case is not, and it differs *per OS*, not just in configuration.

### The matrix

| OS | Interactive (desktop / CLI / logged-in user) | Unattended service / daemon |
|---|---|---|
| **macOS** | ✅ **login keychain** via `security` / Security framework. Unlocked at login. | ⚠️ **System keychain** (`/Library/Keychains/System.keychain`), reachable by a root LaunchDaemon. The **login keychain is unreachable** — a daemon gets `errSecInteractionNotAllowed` (-25308) because there is no GUI session to prompt in, and the data-protection keychain is not available to third-party daemons at all. A daemon's keychain search list is normally just the System keychain. **Needs validation** — see FR-041. |
| **Windows** | ✅ **Credential Manager** via `CredRead`/`CredWrite`. | ⚠️ Per `CredWriteW`, a credential is written "in the user's credential set" and "associated with the logon session of the current token"; network logon sessions have no credential set at all. So a service **cannot read a credential an interactive administrator wrote** — the credential must be written under the service's own account identity. Whether that is workable under `LocalSystem` specifically is **unverified** — see OQ-7. |
| **Linux** | ✅ **Secret Service** (gnome-keyring, KWallet) over session D-Bus. | ❌ **Does not work.** Secret Service needs a session bus and an unlocked keyring; a headless systemd unit has neither, and `gnome-keyring-daemon --login` exits if it does not reach a session bus within minutes. The correct mechanism is **systemd credentials**: `LoadCredentialEncrypted=` / `SetCredentialEncrypted=`, materialised at `$CREDENTIALS_DIRECTORY` on tmpfs, auto-removed when the unit stops, optionally sealed to the TPM2 via `systemd-creds encrypt --with-key=tpm2`. |

**The headline:** an OS-keystore abstraction cleanly covers all three *interactive* cells and at most one *service* cell. Linux-as-a-service needs a different mechanism entirely, and Windows-as-a-service needs a provisioning step. Any design that claims "one keyring library, works everywhere" is wrong about half the matrix.

### What a keystore does and does not buy

Worth stating plainly so the change is not oversold:

- It **does** remove plaintext secrets from `config.yaml` and from a dotfile in `$HOME`, and it puts them behind an OS-managed, encrypted-at-rest store with per-application access control.
- It **does not** stop the secret from being plaintext in the process's memory once loaded. Nothing here changes that.
- On Linux-service it **does not** avoid disk entirely either: `LoadCredentialEncrypted=` decrypts to `$CREDENTIALS_DIRECTORY`, which is tmpfs (not persistent storage) and readable by the service user. That is a genuine improvement over a dotfile — encrypted at rest, scoped to the unit, wiped on stop — but it is not a TPM-sealed-at-use story.

### Library evaluation

| Library | Stars | Latest release | Last push | Verdict |
|---|---|---|---|---|
| [`zalando/go-keyring`](https://github.com/zalando/go-keyring) | 1,307 | **v0.2.8, Mar 2026** | **Jul 2026** | ✅ **Recommended.** Actively maintained. Backends: macOS `security`, Windows Credential Manager (via `danieljoos/wincred`), Secret Service over D-Bus — exactly the three cells where a keystore is the right answer. MIT. |
| [`99designs/keyring`](https://github.com/99designs/keyring) | 655 | v1.2.2, **Dec 2022** | **May 2024** | ❌ Rejected. More backends (encrypted file, Pass, KeyCtl, KWallet), but effectively dormant — no release in over three years. Its extra fallback backends are the only reason to prefer it, and our provider chain supplies fallbacks itself. MIT. |

The fallback logic belongs in our chain, not in the library. That makes the maintained three-backend library the better fit, and keeps `keyring` confined to one provider implementation.

**Verified caveat:** `zalando/go-keyring`'s darwin backend shells out to `/usr/bin/security find-generic-password -s <service> -wa <username>` with **no `-k` keychain argument** (`keyring_darwin.go:43-49`). It therefore relies on the process's implicit keychain search list rather than naming a keychain. For a root daemon that list is normally just the System keychain, so it may work unmodified — but this is an assumption, not a documented guarantee, and it is the cell most likely to be wrongly marked as working. FR-041 requires validating it on a real LaunchDaemon.

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

### Secret storage — the single call site today

`cmd/boabot/main.go:56-69` is the whole of today's secret resolution:

```go
credsPath, err := credentials.DefaultPath()          // ~/.boabot/credentials
creds, err := credentials.Load(credsPath)            // world-readable → fatal
applyCredential(creds, "anthropic_api_key", "ANTHROPIC_API_KEY")
applyCredential(creds, "boabot_backup_token", "BOABOT_BACKUP_TOKEN")
```

`credentials.Load` (`internal/infrastructure/credentials/credentials.go`) is an INI parser: it selects a profile from `BOABOT_PROFILE` (default `"default"`), returns an empty map and `nil` when the file is absent, and returns an error when the file's mode has the world-readable bit set. `applyCredential` promotes a value to an env var **only when that env var is unset** — so an explicit env var already wins over the file.

This is a good foundation. It is already an ordered chain with two links (env var → file), it already fails closed on a permissions mistake, and it is already funnelled through one call site. **This PRD extends that chain rather than replacing it.**

**Gaps:**

1. **Slack tokens bypass it entirely.** `SlackConfig` reads `bot_token` and `app_token` straight from `config.yaml`. There is no env var or credentials-file path for them, so the only way to run Slack today is plaintext in the config file.
2. **The chain is hardcoded, not a port.** `applyCredential` is a package-level function in `main`, called twice with literal key names. There is no interface, so no provider can be added without editing `main` and no provider can be unit-tested through a seam.
3. **Nothing is namespaced per bot.** All bots in the process share one credentials file and one env space. A per-bot Buzz nsec (per OQ-4 below) has no key convention to slot into.

---

## Architecture Decision

Three integration paths for Buzz connectivity were evaluated. **We recommend Option A.**

### Option A — Native Go Nostr client as a `ChannelMonitor` adapter ✅ **Recommended**

A new `internal/infrastructure/buzz/` package implements `domain.ChannelMonitor` over `fiatjaf.com/nostr`, connecting directly to `buzz-relay` by WebSocket, authenticating with NIP-42 + NIP-OA, and publishing/subscribing `kind:9` scoped by `#h`.

- BaoBot keeps its own turn loop, worker harness, budget caps, tool gating, memory, and autonomy gates.
- Single binary; no Rust toolchain, no Node, no sidecar process — preserving the module's local single-binary runtime.
- Buzz becomes one more channel alongside Slack, not a new runtime.
- Verified: every P0 NIP has a maintained Go implementation.
- Cost: we implement NIP-OA attestation ourselves (~150 LOC + test vectors) and own protocol-drift risk against a fast-moving draft spec.

### Option B — Run under `buzz-acp` ❌ **Rejected**

BaoBot would expose an ACP stdio interface and be spawned as a subprocess of `buzz-acp`.

Rejected because **it inverts control.** `buzz-acp` owns channel discovery, mention filtering, the turn loop, idle and wall-clock turn timeouts, and reply dispatch. BaoBot's `TeamManager`, worker supervision, `BudgetTracker`, Tool Attention gating, and calibrated-autonomy gates would all sit *below* a harness that does not know about them — and turn cancellation would happen outside our budget accounting. It also mandates shipping a Rust binary plus `buzz-cli` alongside boabot purely to relay messages, breaking the single-binary deployment. We would be adopting Buzz's agent runtime, not adding Buzz support to ours.

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

### Secret storage — a `SecretStore` port with an ordered provider chain

```go
// internal/domain/secret.go (illustrative)
type SecretRef struct {
    Name string // logical name, e.g. "buzz_private_key"
    Bot  string // optional per-bot namespace; "" = global
}

type SecretProvider interface {
    Name() string                                          // for logs: "env", "keystore", ...
    Lookup(ctx context.Context, ref SecretRef) (string, bool, error)
}

type SecretStore interface {
    Get(ctx context.Context, ref SecretRef) (string, error)
}
```

`SecretStore` holds `[]SecretProvider` and returns the **first hit**. A provider that is unavailable on this platform, or has no entry, returns `(false, nil)` — not an error. Only a provider that is present and *malfunctioning* returns an error.

**Default chain order:**

| # | Provider | Rationale |
|---|---|---|
| 1 | **Explicit environment variable** | Highest precedence. Containers, CI, and `docker run -e` must always win, and this preserves today's behaviour exactly. |
| 2 | **systemd credentials** (`$CREDENTIALS_DIRECTORY`) | Linux-service only; the directory is present only when systemd set it. Correct answer for the Linux-service cell. |
| 3 | **OS keystore** (`zalando/go-keyring`) | The new capability. Covers all three interactive cells and, pending FR-041, macOS-service. |
| 4 | **Credentials file** (`~/.boabot/credentials`) | Today's mechanism, retained unchanged as the floor. Keeps every existing deployment working with no migration. |

Ordering 2 above 3 matters: on a Linux service both may nominally be "available," and the systemd credential is the one that actually works.

**Why not replace the credentials file:** because it works, it is already mode-checked, and it is the only mechanism that functions identically in every cell of the matrix — including containers, which have no keystore and no systemd. It becomes the fallback, not the recommendation.

---

## Goals

- **G1** — BaoBot joins a Buzz workspace as a first-class agent member with its own secp256k1 keypair, reading and writing channel messages under its own authorship.
- **G2** — Buzz is added as a peer channel to Slack behind the existing `ChannelMonitor` seam, with no change to the worker harness, budget, memory, or autonomy subsystems.
- **G3** — Agent identity is owner-attested (NIP-OA/NIP-AA) so BaoBot's provenance is cryptographically verifiable by any Buzz client, and revoking the owner's membership revokes the agent's access.
- **G4** — The channel seam is corrected to depend on a domain interface rather than a concrete Slack type, so Buzz (and any future channel) attaches without further application-layer surgery.
- **G5** — Secrets can be stored in the OS-native encrypted credential store on macOS, Windows, and Linux, for interactive use.
- **G6** — BaoBot resolves secrets correctly when started unattended as a service on all three platforms, using the mechanism appropriate to that platform rather than assuming a keystore is reachable.
- **G7** — No secret is required to live in `config.yaml`, including the Slack tokens that have no alternative today.
- **G8** — Secret resolution moves behind a domain port with an ordered, testable provider chain, replacing the hardcoded two-link chain in `main`.
- **G9** — Existing deployments keep working with no configuration change: env vars and `~/.boabot/credentials` continue to resolve exactly as they do now.

## Non-Goals

- **NG1** — Running BaoBot under `buzz-acp` or exposing an ACP stdio interface. (Option B, rejected above.)
- **NG2** — Hosting or operating a Buzz relay. We are a client; relay operation is the workspace owner's concern.
- **NG3** — Replacing BaoBot's memory subsystem with NIP-AE engrams. Engram publication is a P2 *projection* of existing memory, not a migration.
- **NG4** — Replacing the GitHub PR workflow with NIP-34 git-on-Nostr. Repo/patch support is explicitly deferred (see Phasing).
- **NG5** — Implementing Buzz client-experience NIPs: NIP-DV, NIP-ER, NIP-IA, NIP-PL, NIP-RS, NIP-WP.
- **NG6** — Canvases, voice huddles (NIP-53/LiveKit), Blossom media upload, and `kind:40002`/`40003` rich content and edits.
- **NG7** — Multi-workspace / multi-relay federation. One bot binds to one Buzz community per deployment.
- **NG8** — Removing or deprecating the Slack channel.
- **NG9** — Remote secret managers (AWS Secrets Manager, Vault, 1Password). The `SecretStore` port makes them additive later; none is built here.
- **NG10** — Encrypting the secret in process memory, or defending against a local attacker who can read our process memory or attach a debugger.
- **NG11** — TPM/Secure-Enclave sealed-at-use key operations. `systemd-creds --with-key=tpm2` is supported as a *storage* option because systemd provides it for free; we do not build TPM support ourselves.
- **NG12** — Secret rotation, expiry, or lease renewal. Storage and retrieval only.
- **NG13** — A GUI or TUI for secret entry. Provisioning is CLI and OS-native tooling.
- **NG14** — Changing what secrets exist or how they are used by the subsystems that consume them.

---

## Functional Requirements

### Identity and authentication

**FR-001:** Each BaoBot instance MUST hold its own secp256k1 keypair. Keys are generated out-of-band (`buzz-admin generate-key` or an equivalent Go key generator) and are never shared between bots.

**FR-002:** The agent secret key (nsec) MUST be resolved at startup from the `BUZZ_PRIVATE_KEY` environment variable, falling back to a `buzz_private_key` entry in `~/.boabot/credentials` promoted via the existing `credentials.Load` → `applyCredential` path in `cmd/boabot/main.go`. The existing world-readable-file check (mode `& 0o004`, fatal) MUST apply — it is the custody control for this key. The nsec MUST NOT appear in `config.yaml`, in `team.yaml`, in any committed file, or in any log line at any level.

> **Note:** this module is a local single-binary runtime — `boabot/AGENTS.md` states "No AWS services are required to run," and the only AWS SDK dependency is `bedrockruntime` for the model provider. There is no Secrets Manager integration and this PRD does not introduce one.
>
> Storing the nsec in the OS-native keystore (macOS Keychain, Windows Credential Manager, Linux Secret Service) is specified below as part of the same `SecretStore` chain (FR-038–FR-054, phased into P1 of the Secret Storage workstream — see Phasing). **This requirement does not depend on that work** — env var → credentials file is shippable today, and no requirement here changes when the keystore provider lands. The same applies if the runtime later moves to ECS per `PRODUCT.md`: that is one more provider, not a change to this requirement.

**FR-003:** BaoBot MUST fail closed on identity: if the private key is missing, malformed, or fails to derive the expected pubkey, the Buzz monitor MUST NOT start, and the failure MUST be logged as an error. A bot with Buzz misconfigured MUST still start with all other channels functioning.

**FR-004:** BaoBot MUST perform NIP-42 authentication against the relay: on receiving `["AUTH", "<challenge>"]`, build and sign a `kind:22242` event carrying `["relay", "<url>"]` and `["challenge", "<nonce>"]`, and send `["AUTH", <event>]`.

**FR-005:** When a NIP-OA `auth` tag is configured, BaoBot MUST include it on the `kind:22242` AUTH event per NIP-AA, so the relay grants virtual membership derived from the owner's membership.

**FR-006:** BaoBot MUST construct the NIP-OA preimage as exactly `nostr:agent-auth:` ‖ `event.pubkey` ‖ `:` ‖ `<conditions>`, hash it with SHA-256, and treat the tag as `["auth", owner-pubkey-hex, conditions, sig-hex]` with exactly four elements. The `<conditions>` string MUST be used verbatim — never reordered, deduplicated, normalized, or canonicalized.

**FR-007:** BaoBot MUST validate a configured `auth` tag locally at startup before use: verify the owner Schnorr signature over the preimage, reject `owner-pubkey == agent-pubkey`, reject malformed clause syntax (whitespace, leading/trailing/doubled `&`, non-canonical decimals, out-of-range values), and reject a tag with any element count other than four. Validation MUST be asserted against the published NIP-OA test vectors in unit tests.

**FR-008:** BaoBot MUST set its `created_at` on AUTH events from current wall-clock UTC so it falls inside the relay's freshness window (±120s RECOMMENDED). Clock skew beyond that window MUST surface as a distinguishable error, not a generic auth failure.

**FR-009:** BaoBot MUST distinguish relay auth failure classes in logs and metrics: `"invalid: …"` (malformed AUTH event, bad signature, wrong relay, stale timestamp) versus `"restricted: …"` (missing/invalid credential, non-member owner). These have different operator remedies and MUST NOT be collapsed.

**FR-010:** BaoBot MUST support optional `BUZZ_API_TOKEN` token authentication for relays configured with `BUZZ_REQUIRE_AUTH_TOKEN=true`, resolved on the same credential path as FR-002 (env var, falling back to a `buzz_api_token` credentials-file entry).

**FR-011:** BaoBot MUST publish a `kind:0` profile metadata event on first successful connection, populated from the bot's existing identity (name, bot type, and the description from its `AGENTS.md`), so humans in the workspace see a named agent rather than a bare pubkey. This is a community-global write and is permitted for NIP-AA virtual members per "Virtual Member Privileges"; it MUST NOT be made conditional on explicit relay enrollment.

### Channel participation

**FR-012:** BaoBot MUST connect to the configured relay over WebSocket, MUST reconnect with bounded exponential backoff and jitter on disconnect, and MUST re-authenticate and re-establish all subscriptions after every reconnect.

**FR-013:** BaoBot MUST discover the channels it is a member of. Discovery MUST use the relay-signed NIP-29 discovery events (`kind:39000` metadata, `kind:39002` member list) over the existing authenticated WebSocket as the primary mechanism, because that is the surface specified in Buzz's `NOSTR.md`. The REST listing `GET /api/channels?member=true` MAY be used as an optional optimisation only; it MUST NOT be required for P0, since depending on it would add a second HTTP client and a second auth mechanism (NIP-98) alongside the WebSocket connection.

**FR-014:** BaoBot MUST subscribe to `kind:9` messages for each channel it participates in, with the subscription filter scoped by `#h` to the channel UUID.

**FR-015:** BaoBot MUST auto-subscribe to newly joined channels by subscribing to `kind:44100` membership-added events filtered by `#p` matching its own authenticated pubkey, and MUST unsubscribe on `kind:44101`.

**FR-016:** Every subscription for a p-gated kind (`44100`, `44101`, `1059`) MUST include a `#p` filter whose values all equal the authenticated pubkey. A subscription that would violate this MUST be rejected in our own code before being sent to the relay, with a clear error. The guard MUST cover `1059` from the outset even though DM handling itself is P1, so the P1 work cannot introduce the violation.

**FR-017:** BaoBot MUST publish channel messages as `kind:9` events carrying the `#h` tag with the target channel UUID, signed by the agent key.

**FR-018:** BaoBot MUST reply in-thread using NIP-10 reply tags, referencing the root event of the conversation it was mentioned in.

**FR-019:** BaoBot MUST treat an inbound event as a task trigger only when it is an @mention of the agent's own pubkey in a channel it is subscribed to. All other channel traffic MUST be ignored for dispatch purposes. **DM-triggered tasks are explicitly out of P0**: DMs are NIP-17 gift-wrapped `kind:1059` and are scheduled in P1, so the P0 trigger surface is @mentions only. The dispatch path MUST be written so that adding a second trigger source in P1 does not require changing it.

**FR-020:** BaoBot MUST ignore events authored by its own pubkey to prevent self-trigger loops, matching the existing Slack adapter's bot-message filter.

**FR-021:** On a qualifying inbound event, BaoBot MUST mint a task ID, construct a `domain.TaskPayload`, and enqueue a `domain.Message{Type: MessageTypeTask, From: "buzz", …}` onto the existing `domain.MessageQueue` — identical in shape to the Slack path.

**FR-022:** BaoBot MUST correlate task results back to their originating channel and thread via a pending map keyed by task ID, and on `HandleResult` MUST publish the output as a `kind:9` reply in the correct channel and thread. Unmatched task IDs MUST be ignored silently (a result from another channel).

**FR-023:** BaoBot MUST publish `kind:20001` presence events (`online`/`away`/`offline`, ≤128 characters) representing **conversational availability**, refreshed frequently enough to stay inside the 180-second staleness bound. Presence MUST NOT be derived from process or infrastructure health.

**FR-024:** BaoBot MUST publish `kind:20001` `offline` presence and close the relay connection cleanly during graceful shutdown, before the existing shutdown path completes.

**FR-025:** BaoBot SHOULD publish `kind:20002` typing indicators while a triggered task is executing, so humans see the agent is working rather than silent.

**FR-026:** BaoBot MUST honour the `!shutdown` relay message as a stop signal, routing it through the existing graceful shutdown path — but **only** when the sending pubkey passes the FR-029 author gate (`respond_to`, or a member of `respond_to_allowlist`, or the configured `owner_pubkey`). A `!shutdown` from any other pubkey MUST be ignored and logged as a rejected control message. An ungated `!shutdown` handler would be a remote-kill primitive available to anyone able to post in a shared channel.

**FR-027:** Any reaction subscription MUST be `{"kinds":[7],"#h":["<channel-uuid>"]}`. A kinds-only reaction subscription MUST NOT be used, because the relay silently delivers nothing.

### Security and safety

**FR-028:** All inbound Buzz content — message bodies, profile metadata, channel names and topics — MUST be routed through the existing prompt-injection sanitisation path applied to untrusted tool output and external messages, and MUST be treated as untrusted regardless of author.

**FR-029:** BaoBot MUST enforce an inbound author gate: an optional `respond_to` pubkey and an optional `respond_to_allowlist` of pubkeys. When set, mentions from pubkeys outside the gate MUST be ignored and the rejection MUST be logged. This gate maps onto the existing `receive_from` concept in the pubkey key space.

**FR-030:** Buzz-triggered tasks MUST be subject to the existing per-bot budget caps (daily tokens, hourly tool calls) with no separate accounting path, and MUST be subject to the existing calibrated-autonomy gates. Buzz message origin MUST NOT downgrade any gate.

**FR-031:** *(⚠️ BLOCKED ON OQ-1 — not implementation-ready; do not score this requirement as complete until OQ-1 is answered.)* BaoBot MUST enforce at-most-one-live-instance per pubkey (invariant I4). The observable requirement is testable regardless of mechanism: **whenever two boabot processes are running against the same nsec, a single @mention MUST produce exactly one reply event and exactly one presence identity on the relay.** Because this module runs all bots as in-process goroutines in a single binary, the realistic violation is two copies of the binary started against one config — during an upgrade, or by operator error — not two orchestrated cluster tasks. The mechanism is deferred to OQ-1.

**FR-032:** The agent pubkey, relay URL, channel UUID, and event ID MUST appear in structured logs for every dispatched task and published reply, so relay-side audit records reconcile with our logs. The private key MUST never appear.

### Wiring and configuration

**FR-033:** `TeamManager` MUST hold a collection of channel monitors typed against a domain-level interface rather than the concrete `*slackinfra.Monitor`, and the `internal/application/team` package MUST NOT import `internal/infrastructure/slack` or `internal/infrastructure/buzz`.

**FR-034:** Task-result forwarding MUST iterate all registered monitors rather than branching on a single Slack field, in both the orchestrator and non-orchestrator paths. Existing Slack behaviour MUST be preserved exactly.

**FR-035:** Buzz configuration MUST live under a `buzz:` block in `config.yaml` carrying non-secret settings only: `enabled`, `relay_url`, `bot_name`, `owner_pubkey`, `respond_to`, `respond_to_allowlist`, `channels`, `presence_interval`. This mirrors `SlackConfig` in `internal/infrastructure/config/config.go`. Secret material (nsec, auth tag, API token) MUST NOT appear in this block under any key — it resolves only via the FR-002 credential path.

**FR-036:** The Buzz monitor MUST activate only when `buzz.enabled` is true and all required settings resolve, mirroring the Slack monitor's all-or-nothing activation. With Buzz disabled, no Nostr code path may execute and no relay connection may be attempted.

**FR-037:** `boabot/AGENTS.md` and the root `AGENTS.md` MUST be corrected to remove the non-existent Microsoft Teams adapter and to document the Buzz adapter in its place.

### Secret storage — the port and the chain

**FR-038:** A `SecretStore` port and a `SecretProvider` interface MUST be defined in `internal/domain/`. Neither may import any keystore, D-Bus, or OS-specific package.

**FR-039:** `SecretStore.Get` MUST consult providers in configured order and return the first hit. A provider that is unavailable on the current platform, or that holds no entry for the reference, MUST return "not found" rather than an error, and MUST NOT halt the chain.

**FR-040:** The default provider order MUST be: explicit environment variable → systemd credentials directory → OS keystore → credentials file. The order MUST be configurable, and any provider MUST be omissible.

**FR-041:** An OS keystore provider MUST be implemented over `zalando/go-keyring`, confined to `internal/infrastructure/secret/keystore/`. Its behaviour **MUST be validated on a real service on each platform** before the platform is documented as supported — specifically including whether the library's `security` invocation reaches the **System** keychain from a root LaunchDaemon, given it passes no `-k` argument. If it does not, the provider MUST either name the keychain explicitly or the macOS-service cell MUST be documented as requiring a provisioning step (`security add-generic-password -A -k /Library/Keychains/System.keychain …`).

**FR-042:** A systemd credentials provider MUST be implemented that reads `$CREDENTIALS_DIRECTORY/<name>`. It MUST be inert when that variable is unset, so it costs nothing on non-Linux platforms and on Linux outside systemd.

**FR-043:** The existing credentials-file loader MUST be wrapped as a provider with its behaviour unchanged, **including the world-readable check remaining fatal**. That check is the floor control for the file provider and MUST NOT be downgraded to a warning.

**FR-044:** The environment-variable provider MUST preserve today's precedence exactly: an explicitly-set env var wins over every other provider.

**FR-045:** Secret lookups MUST be namespaced per bot where a per-bot secret is meaningful, via `SecretRef.Bot`. The keystore key convention MUST be documented and stable, since it becomes the on-disk contract in users' keychains. (This is the namespace the per-bot Buzz nsec, per OQ-4, will use.)

### Secret storage — callers and configuration

**FR-046:** `cmd/boabot/main.go` MUST resolve `ANTHROPIC_API_KEY` and `BOABOT_BACKUP_TOKEN` through `SecretStore` rather than the current two `applyCredential` calls, with no change in observable behaviour for existing deployments.

**FR-047:** `SlackConfig` MUST gain a secret-resolution path so `bot_token` and `app_token` can be supplied without appearing in `config.yaml`. The existing inline fields MUST continue to work for one release, and MUST log a deprecation warning naming the file when used.

**FR-048:** Config loading MUST reject any `config.yaml` that inlines a secret for which a resolution path now exists, once the deprecation period in FR-047 ends. Until then it MUST warn.

**FR-049:** `boabotctl` MUST gain subcommands to write, read-presence-of (never the value), and delete secrets in the OS keystore, so operators are not required to learn `security`, `cmdkey`, and `secret-tool` separately.

**FR-050:** A diagnostic command MUST report, for each configured secret, **which provider resolved it** — never the value, and never a prefix or suffix of the value. Without this, a four-link chain is undebuggable in the field.

### Secret storage — safety

**FR-051:** No secret value may be logged at any level, by any provider, on any code path, including error paths. Provider errors MUST be reported by provider name and reference name only.

**FR-052:** A secret value MUST NOT be passed to a subprocess as a command-line argument by any provider, since process arguments are world-readable on all three platforms.

**FR-053:** When no provider resolves a required secret, the error MUST name the reference and enumerate the providers consulted, so the operator knows where the value was looked for.

**FR-054:** Documentation MUST state the residual exposure explicitly: secrets are plaintext in process memory once loaded, and on Linux-service the systemd credential is plaintext at `$CREDENTIALS_DIRECTORY` (tmpfs, unit-scoped, wiped on stop). The keystore MUST NOT be presented as meaning "the secret never touches disk."

---

## Non-Functional Requirements

- **Performance:** Time from relay delivery of a qualifying mention to task enqueue MUST be under 500 ms at p95, excluding model inference. Reply publication after `HandleResult` MUST be under 1 s at p95. Secret resolution happens at startup only: the full provider chain MUST complete within 2 s per secret, and an unreachable D-Bus or keychain MUST time out rather than hang startup indefinitely.
- **Reliability:** The Buzz monitor MUST survive relay restarts and network partitions without operator intervention, reconnecting with bounded backoff and jitter. A Buzz outage MUST NOT degrade Slack, SQS, or scheduled-task processing. Reconnect MUST NOT lose the pending task-ID correlation map. Separately, an unavailable secret provider MUST degrade to the next provider, never to a crash; a locked or absent keystore MUST NOT prevent startup when a later provider can supply the value.
- **Security:** Private keys resolve only via the FR-002 credential path (env var, or a mode-0600 `~/.boabot/credentials` entry, or — once FR-038–FR-045 land — the OS keystore or systemd credentials) — never logged, never in config, never on a command line. Inbound content is untrusted (FR-028). Author gating enforced before dispatch (FR-029). NIP-GS git signing, if ever enabled, sits behind an **Escalating** autonomy gate — an agent key that can sign commits is a materially larger blast radius than one that can post messages. **An issued NIP-OA `auth` tag must be treated as a full relay read/write capability, not a scoped one**: `kind=` clauses do not constrain relay admission, and the tag is valid at any NIP-AA relay. Attestations MUST therefore carry a bounded `created_at<` window, and issuance MUST be treated as granting workspace-wide access. No secret value may appear in `config.yaml`, on a command line, or in a log at any level (FR-051, FR-052).
- **Observability:** Structured logs for connect, authenticate, subscribe, dispatch, publish, and reconnect. Metrics for connection state, auth failures split by `invalid`/`restricted` class, events received/published by kind, dispatch latency, and reconnect count. Presence staleness MUST be observable so I3 violations are detectable. Startup MUST additionally log, per secret, which provider resolved it — by name only (FR-050).
- **Maintainability:** `fiatjaf.com/nostr` imports confined to `internal/infrastructure/buzz/`. `zalando/go-keyring` and `godbus/dbus` imports confined to `internal/infrastructure/secret/`. Domain and application layers MUST have zero Nostr or keystore imports. Buzz draft NIPs are moving targets — the version of `block/buzz` validated against MUST be recorded in `docs/architectural-decision-record.md`.
- **Testing:** TDD throughout, per `AGENTS.md`. 90%+ coverage on domain and application layers. Adapter integration tests tagged `//go:build integration` run against a real relay started from Buzz's own `docker-compose.yml`. NIP-OA implementation MUST assert against the published test vectors. The secret provider chain MUST be unit-testable with fake providers and no OS keystore; real-keystore tests MUST also be tagged `//go:build integration`.
- **Deployment:** No Rust toolchain, no Node runtime, no sidecar container. Single Go binary, unchanged image build.
- **Portability:** The secret-storage work MUST build and pass tests on macOS, Windows, and Linux. Platform-specific code MUST be behind build tags with a working no-op or fallback on every other platform. CI MUST run the test suite on all three.
- **Compatibility:** An existing deployment using env vars or `~/.boabot/credentials` MUST work unchanged with no config edit. This is a strict requirement, not a best effort.

---

## Acceptance Criteria

### Buzz support

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
- [ ] **(Blocked on OQ-1)** With two boabot processes started against the same nsec, a single @mention produces exactly one reply event on the relay, and the workspace shows one presence identity, not two.
- [ ] The nsec is read from `BUZZ_PRIVATE_KEY`, and from a `buzz_private_key` credentials-file entry when the env var is unset; a world-readable `~/.boabot/credentials` is fatal at startup, verified by test.
- [ ] A `buzz:` config block containing any secret-looking key is rejected at config load with a clear error, verified by test.
- [ ] Channel discovery works over the WebSocket alone with the REST endpoint unavailable: the bot joins a channel, receives `39000`/`39002`, and answers a mention in it — verified against a relay with REST disabled or the path blocked.
- [ ] With `BUZZ_REQUIRE_AUTH_TOKEN=true` on the relay, a bot configured with a valid `BUZZ_API_TOKEN` connects, and one without it is rejected — both asserted.
- [ ] An AUTH event built with a clock offset beyond the relay's ±120s freshness window produces a distinguishable clock-skew error, not a generic auth failure, verified by test with an injected clock.
- [ ] A `!shutdown` from a pubkey outside the FR-029 author gate is ignored and logged as a rejected control message; one from an allowed pubkey shuts the bot down gracefully.
- [ ] Dispatch latency from relay delivery to queue enqueue is measured under load and reported at p95 against the 500 ms target; reply publication is measured at p95 against the 1 s target. Measurement harness is committed with the tests.
- [ ] A NIP-AA-authenticated bot successfully publishes its `kind:0` profile without being explicitly enrolled in `relay_members`, confirming the virtual-member write path.
- [ ] A NIP-AA-authenticated bot is confirmed **not** to inherit the owner's channel memberships: it cannot read a private channel the owner belongs to and it does not.
- [ ] A Buzz-triggered task that exceeds the bot's daily token cap is refused by the existing `BudgetTracker` with no Buzz-specific bypass.
- [ ] Inbound message content containing prompt-injection patterns is sanitised on the same path as MCP tool output, verified by test.
- [ ] With `buzz.enabled: false`, no relay connection is attempted and no Nostr code path executes; Slack behaviour is byte-identical to today.
- [ ] `go build`, `go vet`, and `golangci-lint run` pass; `go test -race ./...` passes; domain and application coverage is ≥90% and has not regressed.
- [ ] `grep -r "fiatjaf.com/nostr" internal/domain internal/application` returns no matches.
- [ ] `grep -r "infrastructure/slack\|infrastructure/buzz" internal/application` returns no matches.
- [ ] The agent's private key does not appear in any log output at any level, verified by test against a captured log buffer.
- [ ] `docs/technical-details.md`, `docs/product-details.md`, and `docs/architectural-decision-record.md` are updated; the ADR entry records the Option A/B/C decision and the `block/buzz` commit validated against.
- [ ] `boabot/AGENTS.md` and root `AGENTS.md` no longer claim a Microsoft Teams adapter.

### Secret storage

- [ ] A secret written to the macOS login keychain is resolved by an interactively-run boabot, with nothing in `config.yaml` or `~/.boabot/credentials`.
- [ ] The same, on Windows via Credential Manager.
- [ ] The same, on Linux via Secret Service in a desktop session.
- [ ] **macOS service:** a LaunchDaemon-started boabot resolves a secret from the System keychain — or, if the library cannot reach it, the documented `-k /Library/Keychains/System.keychain` provisioning step is verified to work and the limitation is recorded. Either outcome is acceptable; silently claiming support is not.
- [ ] **Linux service:** a systemd unit using `LoadCredentialEncrypted=` starts with no session D-Bus and no unlocked keyring, and resolves the secret from `$CREDENTIALS_DIRECTORY`.
- [ ] **Linux service, negative:** the same unit with only a Secret Service entry and no systemd credential fails with an error naming every provider consulted — not a hang, and not a generic failure.
- [ ] **Windows service:** a boabot Windows service resolves a credential written under its own service account identity; the OQ-7 finding on `LocalSystem` is recorded either way.
- [ ] Provider precedence: with the same logical secret present in all four providers, the env var wins; unset it and the systemd credential wins; unset that and the keystore wins; remove that and the file wins. Asserted as a single ordered test.
- [ ] A provider that errors (e.g. D-Bus refuses the connection) does not halt the chain — the next provider is consulted and resolution succeeds.
- [ ] A world-readable `~/.boabot/credentials` remains fatal at startup, with the existing error message.
- [ ] Slack `bot_token` and `app_token` resolve from the keystore with neither present in `config.yaml`, and the bot connects to Slack.
- [ ] A `config.yaml` with inline Slack tokens still works and emits a deprecation warning naming the alternative.
- [ ] `boabotctl` writes, checks presence of, and deletes a keystore secret on each of the three platforms.
- [ ] The diagnostic command reports the resolving provider for every configured secret and no secret value, verified by asserting against captured output.
- [ ] No secret value appears in log output at any level, including on every provider error path, verified by test against a captured log buffer with a sentinel value.
- [ ] No provider passes a secret as a subprocess argument, verified by inspecting the constructed command in test.
- [ ] An existing deployment using only env vars, and one using only `~/.boabot/credentials`, both start with a byte-identical config to before this change.
- [ ] `go build`, `go vet`, `golangci-lint run`, and `go test -race ./...` pass on macOS, Windows, and Linux in CI; domain and application coverage ≥90% and not regressed.
- [ ] `grep -r "go-keyring\|godbus" internal/domain internal/application` returns no matches.
- [ ] `docs/technical-details.md`, `docs/product-details.md`, and `docs/architectural-decision-record.md` updated; the ADR records the per-OS matrix and the `zalando` over `99designs` decision with the maintenance evidence.
- [ ] `user-docs/` gains a secret-provisioning guide with the per-OS × per-mode matrix and the residual-exposure statement from FR-054.

---

## Phasing

### Buzz support

**P0 — Presence in the workspace** (FR-001 → FR-024, FR-028 → FR-037)
Keypair identity, NIP-OA attestation, NIP-42/NIP-AA auth, `kind:9` read/write with `#h` scoping, thread replies, mention gate, presence, graceful shutdown, the `ChannelMonitor` seam fix, config, and docs. **This is the shippable unit** — after P0, BaoBot is a working Buzz teammate. It ships independent of the Secret Storage phases below — FR-002 already works on env var → credentials file.

**P1 — Conversational polish**
Typing indicators (FR-025), gated `!shutdown` handling (FR-026), reaction publishing and subscription (FR-027), NIP-50 search over channel history, NIP-CW channel-window paging for bounded backfill, and **NIP-17 gift-wrapped DMs — including DM-triggered tasks, which FR-019 excludes from P0**.

**P2 — Agent-native Buzz features**
NIP-AP persona publication (`kind:30175`) projected from `SOUL.md` + `AgentCard`; NIP-AM turn metrics (`kind:44200`) projected from `BudgetTracker`; NIP-AE engram publication (`kind:30174`) as an encrypted projection of existing memory. Each is a projection of something BaoBot already computes.

**Deferred, not scheduled**
NIP-GS git signing, NIP-34 patches, NIP-MP projects, NIP-AO observability, Blossom media, canvases, voice, `kind:40002`/`40003` rich content. Git-on-Nostr in particular is a trust-model change, not a feature increment — it warrants its own PRD.

### Secret storage

**P0 — The chain, with no new backends** (FR-038, FR-039, FR-040, FR-043, FR-044, FR-046, FR-050–FR-054)
Introduce `SecretStore`, wrap the existing env-var and credentials-file behaviour as providers, move `main` onto it, add the diagnostic. **Ships with zero behaviour change** and is independently valuable: it converts a hardcoded chain into a tested port.

**P1 — Keystore and systemd providers** (FR-041, FR-042, FR-045, FR-049)
The actual capability, plus `boabotctl` provisioning. Gated on the FR-041 per-platform service validation.

**P2 — Get secrets out of `config.yaml`** (FR-047, FR-048)
Slack tokens, the deprecation warning, then the hard rejection one release later.

**Deferred** — remote secret managers (AWS Secrets Manager, Vault, 1Password), rotation, TPM sealed-at-use.

---

## Dependencies and Risks

| Item | Type | Notes |
|---|---|---|
| `fiatjaf.com/nostr@master` | Dependency | Verified to ship `nip29`, `nip42`, `nip44`, `nip17`, `nip34`, `nip70`. Pseudo-versioned, no stable tag — pin the exact pseudo-version in `go.mod` and record it in the ADR. |
| `nbd-wtf/go-nostr` archived 2026-01-24 | Risk | Do **not** use it. The successor is the only maintained path; if it stalls, we own a fork. Mitigation: our surface area is one adapter package behind a domain port. |
| Buzz NIPs are `draft` | Risk | All 17 Buzz-specific NIPs are marked `draft` `optional`. Breaking changes are likely. Mitigation: pin and record the validated `block/buzz` commit; run Buzz's own conformance suite (`crates/buzz-conformance`) in CI against the pinned relay image. |
| Relay has no channel-membership API | Dependency | Buzz's own docs call this a "known gap": private channels require explicit membership but there is no REST/event API to manage it. Our bot must be added to private channels out-of-band, or create its own channels. P0 must not depend on programmatic membership management. |
| Invariant I4 vs. multiple running processes | Risk | Two boabot processes sharing one nsec means duplicate presence and **duplicate replies to every mention** — user-visible and embarrassing. In a local single-binary runtime this happens by operator error or during an upgrade, not via cluster scheduling. Blocking design decision; see OQ-1. |
| nsec custody and rotation | Risk | The secret is printed once by `buzz-admin generate-key` and is unrecoverable. Loss means a new identity and loss of the pubkey's history and reputation, which are community-scoped and non-portable. Mitigation: generate directly into the mode-0600 credentials file or the deploying secret store, never to a terminal that scrolls into shell history; document rotation as an identity-replacement procedure, not a key swap. |
| `auth` tag is a broader capability than it appears | Risk | Per NIP-AA's credential-scope warning, an `auth` tag grants **connection-level full relay read/write regardless of `kind=` clauses** (unless the relay opts into per-event enforcement), and is not bound to any particular relay. An operator issuing a "narrow" credential may believe they granted less than they did. Mitigation: bounded `created_at<` windows; treat issuance as a workspace-access grant in the OQ-2 workflow. |
| Owner-scoped quota aggregation | Risk | Relays SHOULD aggregate rate limits by **owner** pubkey across all virtual members derived from that owner. If every bot in `team.yaml` is attested by one owner key, the whole team may contend for a single quota pool and throttle each other. Directly affects the OQ-4 decision. |
| Virtual members do not inherit channel access | Dependency | NIP-AA grants relay-level access only; channel-level checks evaluate the agent's own pubkey. Combined with the missing channel-membership API, each bot must be added to each private channel out-of-band. |
| Agent key compromise | Risk | A compromised agent key can post as the agent anywhere the owner is a member. NIP-OA bounds this (owner key stays safe) but `created_at<` clauses are **not wall-clock expiry** — a misbehaving agent can backdate. Mitigation: short-lived attestations, independent freshness checks, and owner-side membership revocation as the real kill switch. |
| Silent-failure subscription semantics | Risk | P-gated filters and reaction `#h` derivation both fail by delivering nothing rather than erroring. Mitigation: FR-016 and FR-027 make these compile-time-ish guards in our own code with explicit tests. |
| `TeamManager` refactor blast radius | Risk | `team_manager.go` is large and the result-handler wiring has two paths. Regression risk to Slack. Mitigation: refactor is its own commit with Slack tests green before any Buzz code lands. |
| No Go SDK from Block | Risk | Every Buzz crate is Rust; we track the wire protocol, not a vendored SDK. Mitigation: this is inherent to Option A and priced in — the wire protocol is the documented, tested-against-third-party-clients surface. |
| Relay operational dependency | Dependency | A relay must exist. Self-hosted (`docker-compose.yml`, Railway one-click) or Block's managed service. Deployment topology and whether the relay is in-VPC is unresolved — see OQ-3. |
| Prompt injection via public workspace | Risk | Buzz channels may include actors outside our organisation. Every message is model input. Mitigation: FR-028 sanitisation plus FR-029 author gating, and no relaxation of autonomy gates for Buzz-origin tasks. |
| `zalando/go-keyring` | Dependency | MIT, v0.2.8 (Mar 2026), pushed Jul 2026, 1.3k stars — actively maintained. Pulls in `godbus/dbus/v5` and `danieljoos/wincred`. |
| macOS System keychain reachability | Risk | The library passes no `-k` argument and relies on the implicit search list. If a root LaunchDaemon's search list is not what we assume, the macOS-service cell needs explicit keychain naming or a provisioning step. **FR-041 makes validation mandatory before claiming support.** |
| Windows service account identity | Risk | `CredWriteW` associates a credential with "the logon session of the current token", so a service cannot read what an interactive admin wrote. Provisioning must happen under the service's own identity. Behaviour under `LocalSystem` specifically is unverified — see OQ-7. |
| Linux Secret Service is a trap | Risk | It is *present* on desktop Linux and *absent* on servers, so a naive implementation works on the developer's laptop and fails in production. Mitigated by ordering systemd credentials ahead of the keystore and by the negative-path acceptance criterion. |
| `security` CLI dependency on macOS | Risk | The library shells out to `/usr/bin/security` rather than linking the Security framework. Fine on stock macOS, but it is a subprocess on the startup path and its output is parsed as text. Note FR-052 — the library uses `-i` stdin mode for writes, which keeps values off the command line; this MUST be re-verified on any library upgrade. |
| Cross-platform CI | Dependency | Requires macOS, Windows, and Linux runners. GitHub Actions provides all three, but the workflows in `.github/workflows/` are Linux-only today and will need matrix builds. |
| Testing keystores in CI | Risk | Headless CI has no unlocked keychain. Real-keystore tests must be `//go:build integration` and either skipped in CI or run against a keychain created and unlocked in the job. Do not let this pressure the unit tests into depending on a real keystore. |
| Deprecating inline Slack tokens | Risk | FR-048's eventual hard rejection breaks any deployment that ignored the warning. Mitigation: warn for a full release, name the exact replacement in the message, and call it out in release notes. |
| Buzz P0 vs. Secret Storage P0 sequencing | Dependency | Buzz's nsec resolution (FR-002) is independent of the Secret Storage `SecretStore` work (FR-038–FR-054): env var → credentials file is sufficient to ship Buzz P0. The two workstreams can proceed in either order or in parallel; only the *keystore provider* for the nsec (part of Secret Storage P1) depends on Buzz's config shape (FR-035) existing first. |

---

## Open Questions

- **OQ-1 (blocking — decide before implementation):** How is invariant I4 (at-most-one-live-instance per pubkey) satisfied for a local single-binary runtime? Candidates: (a) a process-level singleton — a lock file or advertised-lock on the nsec, so a second boabot started against the same key refuses to attach its Buzz monitor and logs why; (b) a startup presence probe — query the relay for a live `kind:20001` from our own pubkey and refuse to start the monitor if one is fresher than the staleness bound; (c) reply deduplication by task ID, tolerating duplicate presence but not duplicate replies; (d) a distinct keypair per running instance, which fragments identity and reputation (both community-scoped and non-portable) and is likely wrong here. (a) is cheapest and matches the existing world-readable-credentials-file precedent of failing fast on a misconfiguration; (b) is more robust across machines but adds a startup round-trip and a race window. Note this is *not* an ECS drain-before-start question — see FR-031.
- **OQ-2:** Who holds the **owner** key that issues NIP-OA attestations, and what is the issuance workflow? An operator laptop, a `boabotctl` subcommand, or an orchestrator-held key? This determines whether attestation renewal is manual or automatable, and it is the root of the whole trust chain.
- **OQ-3:** Self-hosted relay or Block's managed service? For the local single-binary runtime this is mainly a question of what `relay_url` operators point at and whether outbound WebSocket to a third-party host is acceptable. If boabot later moves to ECS per `PRODUCT.md`, it additionally becomes a VPC, security-group, and egress question.
- **OQ-4:** Which bots get Buzz identities — every bot in `team.yaml`, or only the orchestrator acting as the team's single front door? Per-bot identities are more faithful to Buzz's model and give better audit granularity; a single front door is far less key material to custody. Note the new constraint surfaced from NIP-AA: relays SHOULD aggregate quotas by **owner** pubkey across virtual members, so a fleet of per-bot identities attested by one owner key may throttle each other. If we go per-bot, we may need per-bot owner keys too — which multiplies the OQ-2 custody problem.
- **OQ-5:** Do we need `boabotctl` subcommands for Buzz key generation, attestation issuance, and channel join, so operators are not required to build Rust `buzz-admin` locally? Likely yes for adoption, but it is additional scope not currently costed in P0.
- **OQ-6:** Should NIP-AE engrams replace, mirror, or merely export our memory subsystem? Deferred to P2, but the answer affects whether P0's memory writes need any Buzz-shaped metadata from the start.
- **OQ-7:** Can a Windows service running as `LocalSystem` write and read its *own* Credential Manager entry? `CredWriteW` documents credentials as bound to the current token's logon session, and `LocalSystem` does have a profile — but this was not verified, and one widely-cited source conflated the wincred API with the Biometric Framework credential manager, which has a documented restriction on non-interactive accounts. **This must be tested on a real Windows service**, not researched. If the answer is no, Windows-service deployments need either a dedicated service *user* account or a DPAPI machine-scope blob provider, and the latter is additional scope.
- **OQ-8:** Do we run BaoBot as a service on all three platforms today, or is Windows-as-a-service hypothetical? If nobody runs it, OQ-7 drops from blocking to informational and Secret Storage P1 can ship covering macOS and Linux service modes only.
- **OQ-9:** Should the per-bot namespace (FR-045) key on bot *name* or bot *type*? Names are unique but change when a bot is renamed — which would orphan keychain entries with no migration path. Types are stable but collide across multiple bots of one type.
- **OQ-10:** Is `~/.boabot/credentials` the right home when running as a service, given the service account's `$HOME` may be `/var/lib/...`, `C:\Windows\System32\config\systemprofile`, or unset? A service-mode path override may be needed independently of the keystore work.
- **OQ-11:** Should `boabotctl secret set` (FR-049) be able to write to a *remote* bot's keystore, or only the local machine's? Local-only is far simpler and probably right, but the team runs bots on shared hosts.

---

## Appendix — Sources

Protocol, ecosystem, and secret-storage facts in this PRD were taken from primary sources on 2026-08-04:

- [github.com/block/buzz](https://github.com/block/buzz) — `README.md`, `NOSTR.md`, `VISION_REMOTE_AGENTS.md`, `docs/remote-agents.md`, `docs/nips/NIP-OA.md`, `docs/nips/NIP-AA.md`, `docs/nips/` index, `crates/` listing, `crates/buzz-acp/README.md`
- [fiatjaf.com/nostr](https://pkg.go.dev/fiatjaf.com/nostr) — package layout inspected directly in the Go module cache at `v0.0.0-20260731140316-a8080728893f`
- [nbd-wtf/go-nostr](https://github.com/nbd-wtf/go-nostr) — archive notice and successor pointer
- [The New Stack](https://thenewstack.io/block-buzz-agent-workspace/), [Decrypt](https://decrypt.co/374026/jack-dorseys-block-launches-buzz-a-nostr-based-slack-and-github-rival-for-ai-agents), [Crypto Briefing](https://cryptobriefing.com/block-launches-buzz-nostr-workspace/) — launch context
- [Agent Client Protocol](https://agentclientprotocol.com/) — the stdio protocol `buzz-acp` speaks
- [`CredWriteW` (wincred.h)](https://learn.microsoft.com/en-us/windows/win32/api/wincred/nf-wincred-credwritew) — "creates a new credential … in the user's credential set"; "associated with the logon session of the current token"; `ERROR_NO_SUCH_LOGON_SESSION` — "Network logon sessions do not have an associated credential set."
- [systemd — System and Service Credentials](https://systemd.io/CREDENTIALS/) and [`systemd-creds(1)`](https://manpages.debian.org/testing/systemd/systemd-creds.1.en.html) — `LoadCredentialEncrypted=`, `SetCredentialEncrypted=`, `$CREDENTIALS_DIRECTORY`, TPM2 sealing.
- [Apple Developer Forums — daemon keychain access](https://developer.apple.com/forums/thread/656000) — `errSecInteractionNotAllowed` (-25308); daemon search list is the System keychain; data-protection keychain unavailable to third-party daemons.
- [ArchWiki — GNOME/Keyring](https://wiki.archlinux.org/title/GNOME/Keyring) and [Fedora — ModularGnomeKeyring](https://fedoraproject.org/wiki/Changes/ModularGnomeKeyring) — Secret Service requires session D-Bus; `gnome-keyring-daemon --login` exits without one.
- [`zalando/go-keyring`](https://github.com/zalando/go-keyring) — source inspected at v0.2.8 in the module cache; `keyring_darwin.go:43-49` confirms no `-k` keychain argument.
- [`99designs/keyring`](https://github.com/99designs/keyring) — release and push dates from the GitHub API.

**Not a source:** [Managing Credentials (ee207400)](https://learn.microsoft.com/en-us/previous-versions/ee207400(v=vs.85)) states "Credentials cannot be stored, queried, or deleted for … non-interactive accounts such as LocalSystem, LocalService, or NetworkService" — but that page documents the **Windows Biometric Framework** credential manager, *not* the `wincred.h` Credential Manager API this PRD uses. It is recorded here because it surfaces in searches and is easily mistaken for authority on `CredRead`/`CredWrite`. It is the reason OQ-7 is an open question rather than a settled answer.

Codebase facts were taken from this repository at commit `ea41be3`.
