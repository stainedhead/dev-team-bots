# Architectural Decision Record — boabot-team

Module-specific decisions. For system-level decisions see root [`docs/architectural-decision-record.md`](../../docs/architectural-decision-record.md).

---

## ADR-T001 — team.yaml as the single deployment manifest

**Decision:** `team.yaml` is the only mechanism for declaring which bots are deployed. No manual CDK stack modifications per bot.

**Rationale:** Keeps the team definition in one place. Adding, removing, or disabling a bot is a one-line change in a single file, followed by a CDK deploy. Prevents configuration drift between what is defined and what is deployed.

---

## ADR-T002 — BotConstruct as a reusable CDK construct

**Decision:** All per-bot resources are encapsulated in a single `BotConstruct` TypeScript class. The team stack instantiates one per enabled bot.

**Rationale:** Ensures every bot gets the same resource set with the same configuration pattern. New resource types are added to `BotConstruct` once and automatically applied to all bots.

---

## ADR-T003 — New bots default to enabled: false

**Decision:** New entries in `team.yaml` must start with `enabled: false`. They are flipped to `true` only after review of SOUL.md, AGENTS.md, and config.yaml.

**Rationale:** Prevents accidental deployment of incomplete bot definitions. Provides an explicit review gate before a new bot joins the live cluster.
