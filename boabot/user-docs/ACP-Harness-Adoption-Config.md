# ACP Stdio Harness — Adoption & Configuration

`boabot -acp` runs a single persona as an [Agent Client Protocol](https://agentclientprotocol.com/) (ACP) agent over stdio — registrable as a `buzz-acp` custom harness (Buzz's default `--agent-command` is `goose`; this document covers pointing it at `boabot` instead). This is a **different, complementary** integration from the native Buzz channel monitor described in [`Buzz-Adoption-Config.md`](Buzz-Adoption-Config.md) — read that document first if you haven't, to understand the tradeoff below.

## Native Buzz channel monitor vs. ACP harness mode — which one do you want?

| | Native channel monitor (`config.yaml`'s `buzz:` block) | ACP harness mode (`boabot -acp`) |
|---|---|---|
| Who owns the Buzz relay connection and identity? | `boabot` itself — its own Nostr keypair | `buzz-acp` — you register the persona as a Buzz-managed agent |
| Deployment model | Always-on daemon process, one per persona (or one process running the whole team, with one Buzz identity) | Spawned and pooled by `buzz-acp`, alongside any other harnesses (`goose`, etc.) it manages |
| Requires its own `buzz_private_key` secret? | Yes | No — no Buzz secret material touches `boabot` at all in this mode |
| Best fit | An always-on team member with its own durable identity across restarts | A lighter-weight registration, or when you don't want to provision and manage a separate Nostr identity per persona |

Both modes execute tasks through the exact same `Worker` (model provider, tools, memory, skills) — there is no behavioral difference in *what* the persona can do, only in *how* it connects to Buzz.

## Step 1 — Confirm your persona's `config.yaml`

ACP mode loads **one persona's own `config.yaml` directly** (e.g. `boabot-team/bots/tech-lead/config.yaml`) — not `team.yaml`. It needs at minimum a `bot:` block and a `models:` block, exactly as for native daemon mode:

```yaml
bot:
  name: tech-lead
  type: tech-lead

models:
  default: my-provider
  providers:
    - name: my-provider
      type: anthropic
      model_id: claude-...
```

`SOUL.md` must exist alongside `config.yaml` in the same directory (e.g. `boabot-team/bots/tech-lead/SOUL.md`) — this is where the persona's system prompt comes from, same as native mode.

No `buzz:` block is needed or used in this mode — `buzz-acp` handles the relay connection entirely outside `boabot`'s config.

## Step 2 — Register `boabot` as `buzz-acp`'s agent command

Point `buzz-acp` at the `boabot` binary instead of its default (`goose`):

```bash
buzz-acp \
  --relay-url <your-relay-url> \
  --private-key <your-agent's-nsec-or-hex> \
  --agent-command boabot \
  --agent-args "-acp,-config,/path/to/boabot-team/bots/tech-lead/config.yaml"
```

(Exact flag syntax for `--agent-args` — comma-separated vs. repeated flag — depends on your installed `buzz-acp` version; run `buzz-acp --help` to confirm.) The private key and relay URL here are `buzz-acp`'s own — this is the persona identity *Buzz* manages, entirely separate from `boabot`'s own `buzz_private_key` secret used by the native channel monitor (Step 1 above never touches this).

## Step 3 — Verify the connection

`buzz-acp` will spawn `boabot -acp -config ...` as a subprocess and drive it over stdio. `boabot`'s own logs (stderr) will show:

```
starting boabot acp mode
```

A successful `@mention` in a channel `buzz-acp` is subscribed to should produce a real reply, sourced from the persona's configured model provider and tools — no different from a native-mode reply, just delivered through `buzz-acp`'s relay connection instead of `boabot`'s own.

## Behavior notes

- **One process per persona, reused across many turns.** `buzz-acp` pools and reuses spawned agent processes across turns within a session (confirmed via its `--agents`/`--lazy-pool`/`--idle-pool-sleep` flags) — `boabot -acp` is a long-lived process, not spawned fresh per message.
- **Keep-alive.** A long-running turn (e.g. a multi-step tool-using task) periodically reports progress back to `buzz-acp` so it isn't killed by `--idle-timeout`. This happens automatically; no configuration is needed. The interval can be tuned via `BOABOT_ACP_KEEPALIVE_INTERVAL` (a Go duration string, e.g. `5s`) if a deployment configures a tighter `buzz-acp --idle-timeout` than the default.
- **No usage/budget figures reported yet.** `boabot -acp`'s ACP responses don't include token-usage figures — this isn't ACP-mode-specific: no budget/cost enforcement is currently wired into `boabot`'s live task-execution path in *either* mode (native daemon or ACP). See `docs/architectural-decision-record.md`'s ADR-B026 for detail.
- **Cancellation** (`session/cancel` from `buzz-acp`, e.g. on a user-initiated stop) cancels the in-flight turn promptly.
- **No filesystem/tool differences from native mode.** The MCP client, memory store, and vector store are constructed the same way; the same tools are available.
