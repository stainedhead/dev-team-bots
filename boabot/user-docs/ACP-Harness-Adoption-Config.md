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

Point `buzz-acp` at the `boabot` binary instead of its default (`goose`). Two ways to select which persona `-acp` mode loads:

**By full path (`-config`)** — points directly at one persona's `config.yaml`:

```bash
buzz-acp \
  --relay-url <your-relay-url> \
  --private-key <your-agent's-nsec-or-hex> \
  --agent-command boabot \
  --agent-args "-acp,-config,/path/to/boabot-team/bots/tech-lead/config.yaml"
```

**By name (`-agent`/`-bots-dir`)** — if you already have a `boabot-team/bots/` checkout, select a persona by name instead of typing out the full path:

```bash
buzz-acp \
  --relay-url <your-relay-url> \
  --private-key <your-agent's-nsec-or-hex> \
  --agent-command boabot \
  --agent-args "-acp,-agent,tech-lead,-bots-dir,/path/to/boabot-team/bots"
```

`-agent` defaults to `orchestrator` if omitted. `-bots-dir` defaults to `<directory-the-boabot-binary-lives-in>/bots` (the same convention native daemon mode's own `team.bots_dir` default uses) — pass it explicitly when your `boabot-team/bots/` checkout isn't in that location, which is the common case for a source checkout like this one. Running **more than one bot** this way means running **one `buzz-acp` process per bot**, each with its own Nostr identity/private key and its own `-agent`/`-bots-dir` (or `-config`) pointed at that persona — there is no single flag that lets one `buzz-acp` process route to multiple different bots dynamically.

If both `-config` and `-agent` are passed, `-config` wins — it's treated as an explicit override.

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
- **Same tool/provider mechanisms as native mode, with scope differences (see below).** The MCP client, memory store, and vector store are constructed the same way; the same tools are available — including a configured `models.chat_provider`, the persona's board-completion tool (unless `bot.type` is `tech-lead`), plugin tools (when `orchestrator.plugins.install_dir` is set), and CLI-agent tools (per `orchestrator.cli_tools.<tool>.enabled`) — activated by reading the same config field names native mode reads, no separate ACP-specific configuration schema needed. What differs is *scope*: two of these are read from a different place than native mode reads them from, and can produce different activation results for the same persona depending on which mode runs it. See the next two bullets.
- **The board is per-ACP-process, not automatically the team's shared board.** ACP mode's board lives at `<memory-root>/<bot.name>/board.json` — for the `orchestrator` persona specifically, this is the same path native daemon mode's shared team board uses (its `bot.name` matches `team.yaml`'s orchestrator entry name), so `complete_board_item` calls there land on the same Kanban board the native web dashboard shows, when both point at the same memory root. For any other persona (`architect`, `reviewer`, etc.), running it under `boabot -acp` gives it its own private, empty board at that persona's own memory path — *not* the team's shared board native mode's non-orchestrator bots all point at. If you need a non-orchestrator persona's ACP-mode board tool to reflect the team's real Kanban board, this isn't currently supported — run that persona under native daemon mode instead.

  **Concurrent-write caution:** because ACP mode's board path coincides with native mode's shared board path for the `orchestrator` persona, running that persona under `boabot -acp` at the same time native daemon mode is also running it means `board.json` has two independently-writing processes, not one. `boabot`'s board store handles this safely (a cross-process file lock plus re-read-and-merge before every write, so one process's write cannot silently discard another's), but two processes editing the *same* board item, or reordering the *same* column, at the same moment can still race on that one item/column — treat simultaneous edits to the identical work item from two processes as unsupported, not merely undocumented.
- **Plugin/CLI-tool activation is scoped to the running persona's own `config.yaml`, not the team's orchestrator config.** Native mode resolves `orchestrator.plugins.install_dir`/`orchestrator.cli_tools.*` **once**, from the team's orchestrator-entry persona's own config, and shares that single result across every bot on the team — so in native mode, every persona gets the same plugin/CLI tools the orchestrator persona's config activates, even if that specific persona's own `config.yaml` never mentions `plugins`/`cli_tools` at all. ACP mode has no `team.yaml`/orchestrator-entry concept to replicate that team-wide sourcing, so `boabot -acp -agent <persona>` reads `orchestrator.plugins.install_dir`/`orchestrator.cli_tools.*` from **that persona's own `config.yaml` only**. Concretely: if only your `orchestrator` persona's config sets `plugins.install_dir` (a common setup), running `boabot -acp -agent architect` activates zero plugin tools, even though native mode's `architect` bot has them. **If you want a non-orchestrator persona's ACP-mode process to activate plugin/CLI tools, copy `orchestrator.plugins.install_dir`/`orchestrator.cli_tools.*` into that persona's own `config.yaml`** — ACP mode will not inherit them from the team's orchestrator config the way native mode does.
- **No mid-task clarifying questions.** Unlike native mode's chat/Buzz ask-channel, an ACP-mode task cannot pause partway through to ask a clarifying question — `Prompt` is a single synchronous request/response turn with no interrupt/resume point, and closing this gap would require an unstable ACP protocol extension (`elicitation`) `buzz-acp` doesn't currently implement. See `docs/architectural-decision-record.md`'s ADR-B030.
- **Fallback publish.** `boabot -acp`'s own system prompt tells the persona it must call `buzz messages send` to publish a reply, but a model can sometimes produce a good answer and skip that tool call anyway (a known function-calling weakness — a casual-seeming reply doesn't always "feel" like it needs publishing). If a turn ends with real output text and the model never called `buzz messages send` itself, `boabot` publishes it on the persona's behalf as a safety net, so a reply is never silently stranded as an ACP-only notification the host can't persist. See `docs/architectural-decision-record.md`'s ADR-B027.
