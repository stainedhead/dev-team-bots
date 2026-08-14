# Buzz — Adoption & Configuration

[Buzz](https://github.com/block/buzz) is an open-source, Nostr-native workspace: every actor — human or agent — holds its own secp256k1 keypair, and every action (a message, a presence update, a channel membership change) is a signed Nostr event published to a relay. boabot connects to a Buzz relay (`buzz-relay`) over WebSocket, the same way it connects Slack over Socket Mode — no public inbound URL is required.

## What's supported

- **Channel `@mentions`** — the bot discovers the channels it is a relay-confirmed member of and responds when `@mention`ed, replying in a NIP-10 threaded `kind:9` message scoped to the channel.
- **Presence** — the bot publishes `kind:20001` presence at an interval under Buzz's 180-second staleness bound, and a `kind:20002` typing indicator while a dispatched task is executing.
- **Gated `!shutdown`** — a `!shutdown` message from an allowed pubkey (the author gate below, or the configured owner) triggers boabot's existing graceful shutdown.
- **NIP-OA / NIP-AA attestation (optional)** — an owner-issued `auth` tag can be attached to the bot's NIP-42 AUTH event, granting Buzz virtual channel membership without explicit per-channel enrollment.

Not yet supported (see the PRD's Phasing section): NIP-17 gift-wrapped DMs and DM-triggered tasks, NIP-50 search, NIP-CW channel-window paging, NIP-AP persona publication, NIP-AM turn metrics, NIP-AE engram publication, and git-on-Nostr (NIP-GS/NIP-34/NIP-MP).

### Multiple personas, one process

Native daemon mode wires one Buzz identity **per Buzz-enabled `team.yaml` persona** — not one process-wide identity. Any number of enabled `team.yaml` entries can each carry their own `buzz:` block (Step 2, below) in their own `bots/<type>/config.yaml`, and each gets its own relay connection, own signed keypair, and own `@mention` responses, all running as goroutines inside the one `boabot` process that also serves the orchestrator web UI/API/board. This is what makes true multi-agent Buzz conversations possible: mentioning persona A in a channel dispatches only to persona A; mentioning persona B dispatches only to persona B; both can be active in the same channel at once without cross-talk, and a Buzz-dispatched task from either shows up live in the orchestrator's Tasks list and Kanban board (tagged with the correct `bot_name`), not just in the Buzz conversation itself.

Provision **each** persona's own `buzz_private_key` separately (Step 3 below, once per persona: `boabotctl secret set buzz_private_key --bot <persona-name>`) — the secret is already namespaced by `bot_name`, so this "just works" the same way per-bot Slack tokens do. There is no additional per-process configuration: every enabled, Buzz-configured persona activates independently and in isolation from every other bot's Buzz status (a bad or missing key for one persona is logged and skipped; it never prevents another persona's monitor, the orchestrator UI, or the rest of the process from starting — the same failure-isolation guarantee a single-identity setup already had).

**One caveat:** each persona's `buzz.bot_name` must be unique across the team. Two personas whose own `config.yaml` accidentally set the same `buzz.bot_name` is treated as a misconfiguration — the second persona's Buzz monitor is refused (logged, not started) rather than silently sharing the first persona's relay connection/queue.

---

## Step 1 — Generate an identity (keypair)

Buzz identity is a secp256k1 keypair, not a username/password. Generate one with any Nostr-compatible tool (`buzz-admin generate-key`, or any `nsec1...`-producing key generator) and note both:

- the **private key** (`nsec1...` bech32, or a raw 64-character hex secret key — boabot accepts either)
- the **public key** (hex), which you will use for `owner_pubkey`/`respond_to_allowlist` entries elsewhere, and which shows up in boabot's own logs once connected

**Never put the private key in `config.yaml`.** It resolves only through boabot's `SecretStore` credential chain — see Step 3.

---

## Step 2 — Configure `config.yaml`

```yaml
buzz:
  enabled: true
  relay_url: wss://relay.example.com   # your buzz-relay endpoint
  bot_name: tech-lead                  # must match an enabled bot's name in team.yaml
  owner_pubkey: <hex>                  # optional; consulted only by the wider !shutdown gate
  respond_to: <hex>                    # optional; single-pubkey author gate
  respond_to_allowlist:                # optional; nil (key omitted) = no gate, [] = allow-none
    - <hex-of-a-trusted-pubkey>
  presence_interval: 60s               # optional; must stay under 180s
```

`bot_name` must match the `name` field of an enabled bot in `team.yaml` — Buzz messages are dispatched as tasks to that bot, exactly like Slack's `bot_name`. The Buzz monitor activates only when `enabled: true` and both `bot_name` and `relay_url` are set **and** the private key resolves (Step 3) — with `enabled: false`, or any of those missing, boabot starts with Buzz completely inert: no relay connection is attempted, and every other channel (Slack, scheduled tasks) is unaffected.

There is deliberately no `channels:` list: channel membership is discovered dynamically from the relay (`kind:39000`/`kind:39002` plus `kind:44100`/`44101` membership events), not configured statically. `config.yaml`'s decoder rejects unknown keys under `buzz:` with a clear error, so a `channels:` entry — or any secret-looking key such as `nsec`, `private_key`, or `api_token` — fails config load rather than being silently ignored or accepted.

---

## Step 3 — Provision the private key (and, if required, the API token or an auth tag)

The private key resolves through the same ordered `SecretStore` chain used for every other boabot secret: **environment variable → systemd credential → OS keystore → `~/.boabot/credentials` file**, first hit wins. The logical secret name is `buzz_private_key`, namespaced by `bot_name` on every provider except the environment variable (which is process-global).

| Provider | How to set it | Notes |
|---|---|---|
| Environment variable | `export BUZZ_PRIVATE_KEY="nsec1..."` | Process-global — ignores `bot_name`. Always wins if set. |
| systemd credential | Unit file: `LoadCredentialEncrypted=tech-lead_buzz_private_key:...` (or `SetCredentialEncrypted=`), materialised at `$CREDENTIALS_DIRECTORY/tech-lead_buzz_private_key` | Linux-service only; inert (a clean miss, zero cost) when `$CREDENTIALS_DIRECTORY` is unset. |
| OS keystore | `boabotctl secret set buzz_private_key --bot tech-lead` (prompts for the value; never accepts it as a command-line argument) | See the per-OS/per-mode matrix below before relying on this in service mode. |
| `~/.boabot/credentials` | Add `tech-lead_buzz_private_key = nsec1...` under `[default]` in a `chmod 600` file | The file must not be world-readable — boabot refuses to start otherwise. |

An optional `BUZZ_API_TOKEN` (logical name `buzz_api_token`, same chain and namespacing) is attached as an `Authorization: Bearer` header on every relay connection when the relay is deployed with `BUZZ_REQUIRE_AUTH_TOKEN=true` — provision it the same way as the private key, substituting `buzz_api_token` for `buzz_private_key`.

### Optional: the NIP-OA owner-attestation tag (`buzz_auth_tag`)

To grant this bot Buzz virtual channel membership (NIP-AA) without explicitly enrolling it — the "NIP-OA / NIP-AA attestation" capability listed under **What's supported** above — provision a third, also-optional secret: `buzz_auth_tag`, same chain and namespacing as `buzz_private_key`/`buzz_api_token` above (substitute `buzz_auth_tag` for `buzz_private_key` in the table). If it is not configured, boabot logs and connects normally without owner attestation — this is not a required secret, only a bot that only ever needs to act as an explicitly-enrolled channel member.

**Format:** a single opaque, pipe-delimited string with exactly three fields:

```
owner_pubkey_hex|conditions|sig_hex
```

- `owner_pubkey_hex` — the attesting owner's public key, hex-encoded.
- `conditions` — the NIP-OA conditions clause the owner signed (may be empty), e.g. `kind=9&created_at<1999999999`.
- `sig_hex` — the owner's Schnorr signature over the NIP-OA preimage, hex-encoded.

This is exactly the shape an external NIP-OA attestation-issuance tool produces (`tag[1]|tag[2]|tag[3]` of the underlying 4-element `["auth", owner_pubkey_hex, conditions, sig_hex]` Nostr tag) — paste that tool's output verbatim, joined with `|`, as the secret value. boabotctl does not add a `--format` flag for this: it treats the value as an opaque string like every other secret, and boabot itself parses and validates it (rejecting, at startup, a malformed field count or a signature that fails to verify — logged and non-fatal, exactly like an absent tag). Issuing the attestation itself (deciding what conditions to sign, running the signing tool) is out of scope for boabot/boabotctl.

```bash
boabotctl secret set buzz_auth_tag --bot tech-lead     # prompts for the value (or reads piped stdin)
boabotctl secret get buzz_auth_tag --bot tech-lead      # reports presence only — never prints the value
boabotctl secret delete buzz_auth_tag --bot tech-lead
```

### `boabotctl secret` reference

```bash
boabotctl secret set buzz_private_key --bot tech-lead     # prompts for the value (or reads piped stdin)
boabotctl secret get buzz_private_key --bot tech-lead      # reports presence only — never prints the value
boabotctl secret delete buzz_private_key --bot tech-lead
```

`boabotctl secret` only ever touches **this machine's** OS keystore — there is no remote-host mode.

---

## Secret provisioning: the per-OS × per-mode matrix

The interactive case (a logged-in desktop user, or a CLI run from a terminal) is uniform across all three OSes. The **unattended service/daemon** case is not — it differs fundamentally per OS, not just in configuration, so "one keystore library, works everywhere" is not a safe assumption to plan a deployment around.

| OS | Interactive (desktop / CLI / logged-in user) | Unattended service / daemon |
|---|---|---|
| **macOS** | ✅ Login keychain via `security` — unlocked at login, works with `boabotctl secret set`/keystore provider as-is. | ⚠️ The **login keychain is unreachable** from a root LaunchDaemon (no GUI session to unlock it). A daemon's keychain search list is normally just the **System keychain** (`/Library/Keychains/System.keychain`), which needs a provisioning step: `security add-generic-password -A -k /Library/Keychains/System.keychain ...`. Validate this on your actual LaunchDaemon before relying on it. |
| **Windows** | ✅ Credential Manager via `CredRead`/`CredWrite` — works with `boabotctl secret set` run interactively. | ⚠️ A credential written by an interactive administrator is bound to *that* logon session — a service running under a different account (or `LocalSystem`) cannot read it. The credential must be written under the service's own account identity. Verify this on your actual Windows service setup before depending on it; the `LocalSystem`-specific case is not verified in this codebase. |
| **Linux** | ✅ Secret Service (gnome-keyring, KWallet) over session D-Bus — works with `boabotctl secret set` in a desktop session. | ❌ **Does not work.** A headless systemd unit has no session bus and no unlocked keyring. Use the **systemd credential provider** instead (`LoadCredentialEncrypted=`/`SetCredentialEncrypted=` in the unit file, optionally sealed to a TPM2 key with `systemd-creds encrypt --with-key=tpm2`) — boabot's `systemd` provider reads `$CREDENTIALS_DIRECTORY/<name>` directly, no D-Bus involved, and costs nothing when that variable is unset. |

**Recommendation:** for any unattended/service deployment, prefer the systemd credential (Linux) or a validated, explicitly-provisioned keystore entry (macOS/Windows) over the keystore provider's default behavior — or use the `~/.boabot/credentials` file (`chmod 600`) as a straightforward fallback that works identically in every mode, at the cost of the secret living in a mode-0600 file rather than an OS-managed store.

### Residual exposure — read this before assuming "never touches disk"

- **Once loaded, a secret is plaintext in boabot's process memory** for the lifetime of the process, regardless of which provider resolved it. No provider in the chain changes this.
- **On Linux running as a systemd service, the credential is plaintext at `$CREDENTIALS_DIRECTORY`** while the unit runs. That directory is tmpfs (not persistent storage), scoped to the unit, and wiped when the unit stops — a real improvement over a plaintext dotfile in `$HOME`, but it is **not** a TPM-sealed-at-use guarantee. `systemd-creds --with-key=tpm2` storage is supported as a pass-through (the encrypted blob at rest can be TPM2-sealed), but boabot itself performs no TPM operations, and the decrypted value at `$CREDENTIALS_DIRECTORY` is ordinary tmpfs-resident plaintext for the duration of the run.
- **The OS keystore MUST NOT be presented, to any operator, as meaning "the secret never touches disk."** It means the secret is encrypted at rest by the OS with per-application access control — a genuine improvement over a plaintext file or an inline `config.yaml` value — not that boabot's own process memory, or (on Linux) the systemd credentials directory, is ever anything other than plaintext while in use.

---

## Example — a channel `@mention`

A human (or another agent) posts a `kind:9` message in a channel boabot has joined, `@mention`ing the bot — this is a Buzz client tagging the bot's pubkey with a `#p` tag on the event, the same primitive used for channel membership and DM addressing:

```
alice: @tech-lead can you check whether the payments service handles a nil webhook body?
```

What happens next, in order:

1. boabot recognises the event as a mention (its own pubkey is in the event's `#p` tags) and, if `respond_to`/`respond_to_allowlist` is configured, confirms `alice`'s pubkey is allowed.
2. It publishes a `kind:20002` typing indicator in the channel and dispatches the message text as a task to the bot named in `bot_name`.
3. While the task runs, boabot refreshes the typing indicator every 15 seconds so the channel shows the bot as actively working.
4. When the task completes, boabot publishes a `kind:9` reply, NIP-10 threaded against the mention's root event and scoped to the same channel (`#h`):

```
tech-lead: Checked internal/handlers/webhook.go — a nil body currently
           panics before the JSON decode. I've opened a fix; see
           PR #482 for the nil-guard and a regression test.
```

The reply appears threaded under `alice`'s original message in any Buzz client, exactly like a Slack thread reply.

---

## Behaviour notes

- **Loop prevention**: events authored by the bot's own pubkey are silently dropped, matching Slack's bot-message filter.
- **Threading**: replies are NIP-10 threaded against the triggering mention's root event, scoped to the originating channel (`#h`).
- **Author gating**: `respond_to`/`respond_to_allowlist` gate ordinary dispatch; `!shutdown` uses a wider gate that also accepts `owner_pubkey`. An out-of-gate sender's mention or `!shutdown` is ignored and logged, never silently dropped without a trace.
- **Budget and autonomy**: Buzz-dispatched tasks go through the exact same `MessageQueue` → worker harness path as every other channel — the existing per-bot `BudgetTracker` caps and calibrated-autonomy gates apply with no Buzz-specific bypass.
- **Process-singleton protection**: a second boabot process started against the same private key refuses to attach its Buzz monitor (logging why) rather than producing duplicate replies or a duplicate presence identity — see `docs/technical-details.md`'s "Buzz (Nostr) Channel Monitor" section.
- **One bot per relay identity**: `bot_name` routes all Buzz messages received on that keypair's identity to a single named bot, matching Slack's `bot_name` model.
