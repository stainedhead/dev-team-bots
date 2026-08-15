# Getting Started — A Second (or Third) Buzz-Enabled Persona

This is a task-oriented walkthrough for adding another Buzz identity to a `boabot` team that already has one working. It assumes you already have a single Buzz-enabled persona running successfully (if not, do that first — see [`Buzz-Adoption-Config.md`](Buzz-Adoption-Config.md) Steps 1-3, and [`getting-started.md`](getting-started.md) for the base team setup).

For the full `buzz:` config reference, the secret-provisioning matrix (per-OS, interactive vs. unattended), and the NIP-OA attestation flow, see [`Buzz-Adoption-Config.md`](Buzz-Adoption-Config.md) — this guide only covers what's different about running *more than one* Buzz identity.

## Why this works

Native daemon mode wires one Buzz `ChannelMonitor` **per Buzz-enabled `team.yaml` persona**, not one process-wide identity. Every persona with its own `buzz:` block in its own `bots/<type>/config.yaml` gets its own relay connection, its own signed keypair, and its own goroutine — all inside the single `boabot` process that also serves the orchestrator web UI. Mentioning persona A in a Buzz channel dispatches only to persona A; mentioning persona B dispatches only to persona B. Both can be active in the same channel or thread at the same time without cross-talk, and each Buzz-dispatched task shows up live in the orchestrator's Tasks list and Kanban board, tagged with the correct `bot_name`.

`boabot-team`'s stock personas already ship with `buzz:` blocks ready to go: `orchestrator`, `architect`, and `tech-lead`. If you're using `boabot-team/bots/`, adding a second persona is almost entirely a secret-provisioning and `team.yaml` exercise — no config-file authoring required. If you're using your own personas, copy the shape of an existing working `buzz:` block into the new persona's `config.yaml` (see Step 2 below).

## Step 1 — Confirm your first persona already works

Before adding a second identity, confirm the first one is healthy: `boabot` is running, its logs show a successful relay connection and AUTH for that persona's pubkey, and `@mention`ing it in a Buzz channel gets a reply. Adding a second persona on top of a broken first one only doubles the number of things to debug.

## Step 2 — Pick and enable the second persona

Pick a persona whose `bots/<type>/config.yaml` already has (or that you're willing to add) a `buzz:` block, and make sure it's enabled in `team.yaml`:

```yaml
team:
  - name: orchestrator
    type: orchestrator
    enabled: true
    orchestrator: true
  - name: architect
    type: architect
    enabled: true         # <-- must be true for its Buzz monitor to start
```

If the persona's `config.yaml` doesn't have a `buzz:` block yet, add one — see [`Buzz-Adoption-Config.md`](Buzz-Adoption-Config.md)'s "Step 2 — Configure `config.yaml`" for the field reference. The one thing to get right up front:

**`buzz.bot_name` must be unique across every enabled persona.** Two personas that accidentally share a `buzz.bot_name` are treated as a misconfiguration: the second one to load is logged and skipped (its Buzz monitor never starts), rather than silently sharing the first persona's relay connection. It does not crash the process or affect any other persona.

## Step 3 — Provision the second persona's own key

Every persona needs its **own** `buzz_private_key` — a distinct keypair, not a shared one. Provision it exactly the way you did for the first persona, substituting `--bot <persona-name>`:

```bash
boabotctl secret set buzz_private_key --bot architect
```

This prompts for the value (an `nsec1...` string or a raw hex secret key) and never accepts it as a command-line argument. The secret is namespaced by `bot_name` in the OS keystore, so provisioning persona B's key never touches persona A's — see [`Buzz-Adoption-Config.md`](Buzz-Adoption-Config.md)'s "Secret provisioning: the per-OS × per-mode matrix" for the full breakdown if you're deploying as an unattended service rather than running interactively.

If the persona also needs an API token (`BUZZ_REQUIRE_AUTH_TOKEN=true` on the relay) or a NIP-OA owner-attestation tag, provision those the same way, substituting `buzz_api_token` / `buzz_auth_tag` for `buzz_private_key`.

## Step 4 — Restart and verify

Restart `boabot` and check the logs for **two** separate relay connection/AUTH sequences — one per persona, each showing its own pubkey. A persona whose key wasn't provisioned (or fails to load) is logged and skipped; it does not stop the first persona or the orchestrator UI from starting. If you only see one connection where you expected two, re-check Step 3 (secret not set, or set under the wrong `--bot` name) before assuming something is broken elsewhere.

## Step 5 — Test in a Buzz channel

In a channel both personas are members of:

1. `@mention` persona A only — confirm only persona A replies.
2. `@mention` persona B only — confirm only persona B replies, and persona A stays silent.
3. `@mention` both in the same message or thread — confirm each replies independently under its own identity, with no reply crossing over to the other persona's task.

While this is happening, open the orchestrator web UI: both dispatches should appear in the Tasks list, each tagged with the correct `bot_name`, and each should have created a Kanban board item that updates as the task runs and completes.

## Step 6 — Add a third persona (optional)

Repeat Steps 2-5 for `tech-lead` (or any other persona with its own `buzz:` block) — the mechanism is identical for a third, fourth, or Nth persona. There is no special-casing for "the second persona" versus "the Nth persona"; every Buzz-enabled, team-enrolled persona is wired the same way.

## Things that surprise people the first time

- **A recurring/scheduled request only gets a live reply on confirmation, not on every future run.** If you `@mention` a persona with "run this every morning at 9am," you get an in-channel confirmation reply, but tomorrow's 9am run does not post back into the channel — it executes via the scheduler and its result lands in the Tasks UI (and Kanban board), not in the Buzz conversation. This matches how the web UI's chat path already behaves; it's not something specific to having multiple personas.
- **"Update" and "cancel" from Buzz are exactly as capable as chat's, no more.** There is no in-place task mutation — re-issuing an instruction creates a new task. Cancellation only applies to a not-yet-confirmed scheduling intent, not a task that's already dispatched or running.
- **Duplicate `buzz.bot_name` is a silent-looking skip, not a crash.** If a second persona's monitor never appears in the logs, check for a name collision before assuming a secret-provisioning problem.
- **Every persona is reachable by DM too, and it's on by default.** DM support activates automatically for every Buzz-enabled persona — there's no separate flag and no separate key to provision. If you only wanted your personas reachable via curated channels, set `respond_to`/`respond_to_allowlist` explicitly; an unconfigured gate (the default) means anyone who has a persona's pubkey can DM it and trigger a real task, with none of the relay-curated membership a channel provides. See [`Buzz-Adoption-Config.md`](Buzz-Adoption-Config.md)'s "Direct messages and threaded replies" section.
- **A reply in-thread continues the conversation without re-`@mention`ing — per persona.** If persona A dispatched in a thread and persona B is also in the channel, a follow-up reply in that thread is picked up only by persona A. This is deliberate, not a bug — it keeps a multi-persona channel's threads from cross-talking.
- **An unauthorized DM gets no reply of any kind, not even a decline.** This is a deliberate default (see `Buzz-Adoption-Config.md`), not a missing feature — a decline reply would itself leak that the persona exists and is listening to an arbitrary Nostr identity.
