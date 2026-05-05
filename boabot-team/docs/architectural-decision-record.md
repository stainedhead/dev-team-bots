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

**Decision:** New entries in `team.yaml` must start with `enabled: false`. They are flipped to `true` only after review of `SOUL.md`, `AGENTS.md`, and `config.yaml`.

**Rationale:** Prevents accidental deployment of incomplete bot definitions. Provides an explicit review gate before a new bot joins the live cluster.

---

## ADR-T004 — DynamoDB budget table in shared stack, items per bot

**Decision:** The DynamoDB table for budget counters is a single shared table provisioned in the shared CDK stack (`boabot/cdk/`). Each bot's IAM role grants read/write access to its own items only (using a condition on the partition key).

**Rationale:** A single table is simpler to operate than one table per bot. IAM conditions enforce item-level isolation without requiring separate tables or resource policies. The table ARN is exported from the shared stack and imported by each `BotConstruct`.

---

## ADR-T005 — S3 versioning on all memory buckets

**Decision:** S3 object versioning is enabled on all private and team memory buckets provisioned by the CDK stack.

**Rationale:** Versioning provides a durable revision history for memory files without requiring git semantics at the remote. Recovery from accidental overwrites or corrupted writes does not require a separate backup mechanism.
