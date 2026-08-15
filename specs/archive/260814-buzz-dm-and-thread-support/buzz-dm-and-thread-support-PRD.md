# PRD: Buzz DM Support and Full Threaded-Reply Support

**Created:** 2026-08-14
**Jira:** N/A
**Status:** Draft

## Problem Statement

`boabot`'s native-mode Buzz integration (shipped in the multi-agent Buzz feature) currently only dispatches on an explicit channel/group `@mention` (kind:9, `#p`-tagged) — this is a firm product requirement gap on two fronts:

1. **No DM support at all.** Buzz agents cannot be reached via a direct 1:1 message. This was explicitly deferred as a Non-Goal in the prior feature (`trigger.go:7` flags kind:1059/NIP-17 DMs as "out of scope this run"), but is now required.
2. **Threaded replies are only partially implemented.** Inbound: the code can resolve a NIP-10 thread's root event, but only runs *after* a message already passes the explicit `@mention` gate — a human replying in-thread without re-mentioning the bot is silently dropped. Outbound: replies are tagged with a root `e` tag (so root-keyed clients group them), but are missing the NIP-10 `reply` marker for the immediate parent and the `p` tag back to the original author. Separately, `DirectTask.ThreadID` for Buzz tasks is keyed by *channel*, not by *thread* — two concurrent threads in the same channel currently share one scheduling-confirmation slot, a real correctness gap independent of the mention issue.

Both gaps prevent Buzz from behaving like a natural conversational participant: users must re-mention the bot on every message in an ongoing thread, and cannot reach a persona privately at all.

## Goals

- Any Buzz-enabled persona with a provisioned key can be reached via a direct 1:1 message (NIP-17 gift-wrap DM), using the same identity it already uses for channel participation.
- A human replying in-thread to a bot's prior message continues that conversation without needing to re-`@mention` the bot on every turn.
- Outbound Buzz replies (channel and DM) carry complete, correct NIP-10 threading metadata so Buzz/Nostr clients render them properly threaded, not as disconnected top-level messages.
- Concurrent threads within the same channel are tracked independently — no shared/colliding scheduling-confirmation state across threads.
- DM-triggered and thread-continued work is fully visible in the orchestrator UI (Kanban board + Tasks list), consistent with how channel-triggered work already behaves — same observability standard, no second-class visibility for DMs.

## Non-Goals

- Not building group/multi-party encrypted DM support (NIP-17 technically supports multiple recipients) — this feature is scoped to 1:1 DMs only.
- Not adding new orchestrator UI screens or widgets — DM- and thread-triggered work populates the existing Kanban board and Tasks list, the same infrastructure the prior feature already wired up.
- Not changing ACP mode (`boabot -acp`) — it must keep building and passing its existing tests, untouched.
- Not building DM support for personas without `buzz.enabled: true`/without a provisioned key — same precondition as the existing channel path.
- Not re-litigating channel `@mention` dispatch, which already works correctly and is out of scope for changes here except where thread-continuation logic needs to interact with it.

## Functional Requirements

**FR-201:** Each Buzz-enabled persona subscribes to NIP-17 gift-wrapped DM events (kind:1059, `#p`-tagged to its own pubkey) using the same relay connection and identity it already uses for channel participation — no separate DM-specific identity or key.

**FR-202:** Inbound DM events are unwrapped (kind:1059 → decrypt → kind:13 sealed rumor → decrypt → kind:14 DM) using the vendored `nip44`/`nip59` primitives already present in the pinned `fiatjaf.com/nostr` dependency.

**FR-203:** A successfully decrypted DM dispatches through the same `BuzzTaskBridge`/`Dispatcher` pipeline channel messages already use — creating a real `DirectTask` and Kanban board item, tagged with the persona's `bot_name`, per the existing observability standard. DM-originated tasks are visibly distinguishable from channel-originated tasks in the UI (e.g., an origin label), so operators can tell which is which without ambiguity.

**FR-204:** DMs from senders not on the same authorization/allow-list mechanism already gating channel dispatch (author gate) do not trigger dispatch. Unauthorized DM handling (silent ignore vs. a polite decline reply) is a design decision to be resolved during the spec/research phase — see Open Questions.

**FR-205:** A human replying in-thread to a bot's own prior channel message is recognized as directed at the bot and dispatched, without requiring a fresh `@mention` on that reply.

**FR-206:** An in-thread reply recognized under FR-205 continues the same underlying task/conversation (the one the thread's root message originally dispatched), carrying forward conversation context, rather than spawning an unrelated new task. This applies symmetrically to DM conversations — an unprompted DM reply within an ongoing DM conversation continues that conversation the same way.

**FR-207:** Outbound Buzz replies (both channel and DM) are tagged with complete NIP-10 threading metadata: the root `e` tag (already correct today), the `reply`-marked `e` tag for the immediate parent, and a `p` tag addressed back to the original author.

**FR-208:** `DirectTask.ThreadID` for Buzz-originated tasks is keyed by the actual NIP-10 thread root (or, for DMs, the DM conversation), not by channel UUID — two concurrent threads in the same channel (or two concurrent DM conversations) get independent scheduling-confirmation state, no cross-thread collision.

**FR-209:** DM reply publishing uses its own outbound path (encrypted kind:14 rumor → seal → gift-wrap), distinct from the existing channel-shaped `publishReply` (which hardcodes an `h` channel tag that does not apply to DMs).

## Non-Functional Requirements

- **Performance:** DM decrypt-and-dispatch latency is comparable to the existing channel-mention dispatch path — no materially new lag from the added unwrap/decrypt steps.
- **Reliability:** A DM subscription/decrypt failure for one persona is isolated — it must not crash the shared `TeamManager` process, and must not block any other persona's channel or DM monitor, extending the existing per-monitor failure-isolation pattern.
- **Security:**
  - DM decryption must never leak decrypted plaintext, private keys, or `nip44` conversation keys into logs.
  - NIP-17's privacy properties (ephemeral gift-wrap keys, randomized timestamps) must be preserved by the implementation, not defeated by, e.g., logging real receipt timestamps in a way that re-correlates gift-wrap metadata.
  - DM dispatch is gated by the same author-authorization mechanism as channel dispatch — an unsolicited DM from an arbitrary Nostr identity must not trigger unrestricted task execution.
- **Observability:** DM- and thread-triggered `DirectTask`/board items are tagged per-persona and per-origin (DM vs. channel vs. thread-continuation), traceable in both logs and the UI — consistent with the existing multi-agent observability standard, per the user's explicit choice that DM tasks are NOT treated as second-class/hidden from the shared board.

## Acceptance Criteria

- [ ] A Buzz-enabled persona receives and responds to a direct 1:1 message sent to its own pubkey, without requiring any `@mention` (a DM is inherently addressed).
- [ ] A DM-triggered task appears in the Tasks UI and creates a Kanban board item, tagged with the correct `bot_name` and visibly labeled as DM-originated, within the UI's existing refresh latency.
- [ ] A DM from a sender not on the authorized allow-list does not trigger task dispatch.
- [ ] Replying in-thread to a bot's prior channel message, without re-mentioning the bot, is recognized and dispatched.
- [ ] An in-thread reply (channel or DM) continues the same task/conversation's context rather than starting an unrelated new one — verified by the reply's response reflecting context from the earlier turn.
- [ ] Two concurrent threads in the same channel (or two concurrent DM conversations) each maintain independent scheduling-confirmation state — a pending "confirm this schedule?" in one thread does not leak into or get confused with a different thread's confirmation.
- [ ] Outbound channel replies carry root `e`, reply `e`, and `p` tags, verified against a NIP-10-aware client or direct event inspection.
- [ ] Outbound DM replies are correctly gift-wrapped (kind:1059) and decrypt correctly for the original DM sender.
- [ ] `boabot -acp` still builds and passes its existing tests, untouched by this work.
- [ ] No private key, `nip44` conversation key, or decrypted DM plaintext appears in logs.

## Dependencies and Risks

| Item | Type | Notes |
|------|------|-------|
| `fiatjaf.com/nostr`'s vendored `nip44`/`nip59` packages | Dependency | Already present in the pinned dependency (confirmed via module cache) — this is wiring work, not building NIP-17 crypto from scratch. |
| Existing author-gating/allow-list mechanism (`guard.go`) | Dependency | This work extends, not replaces, the existing p-gate/authorization pattern already used for channel dispatch. |
| NIP-17 privacy properties | Risk | Implementation must preserve gift-wrap ephemeral-key and timestamp-randomization properties — a naive implementation could inadvertently leak correlatable metadata. |
| Unauthorized/spam DMs | Risk | DMs are reachable from any Nostr identity on the relay, unlike channel membership (presumably curated) — author-gating is a hard requirement, not a nice-to-have, to avoid arbitrary unauthenticated task execution. |
| Thread-continuation state design | Risk | FR-206 (continuing the same task's conversation) is a materially larger behavioral change than simple dispatch — requires correlating a new inbound event to existing task/conversation state. Whether this reuses an existing multi-turn conversation-continuation mechanism (if one exists in the web-UI chat path) or requires new machinery is unresolved — flagged for the research phase. |
| `DirectTask.ThreadID` correctness fix (FR-208) | Risk | This is a fix to existing, already-shipped behavior (channel-UUID-keyed, not thread-keyed) — must be verified not to break the existing channel-mention path's current behavior while fixing the collision. |
| Buzz relay support for NIP-17 | Dependency | Assumes the configured relay (`wss://feral-sysd.communities.buzz.xyz`) supports gift-wrap event kinds — not independently verified in this PRD; flagged for research phase. |
| Unauthorized-DM handling decision (FR-204) | Team dependency | Repo owner/operator must decide silent-ignore vs. polite-decline-reply for DMs from unauthorized senders before FR-204 can be finalized — a UX/security posture call, not a technical unknown. See Open Questions. |

## Open Questions

- Should an unauthorized DM (sender not on the allow-list) receive a polite decline reply, or be silently ignored with no reply at all? (FR-204 — security/UX judgment call, not yet decided.)
- Does the existing worker/agent execution path already support resuming a task's conversation with new context (multi-turn continuation), or does FR-206 require new conversation-state machinery? (Research-phase question — significantly affects implementation size.)
- Should DM conversations that go dormant (no reply for N hours/days) eventually be treated as "new conversation" rather than indefinitely continuing the same task — is there a natural conversation-boundary/timeout, or does every reply to an old DM thread always continue the original task? Not yet decided.
- Exact label/UI treatment for distinguishing DM-originated vs. channel-originated vs. thread-continued tasks in the board/Tasks list (FR-203) — cosmetic detail, deferred to implementation.
