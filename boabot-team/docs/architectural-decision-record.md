# Architectural Decision Record — boabot-team

Module-specific decisions. For system-level decisions see root [`docs/architectural-decision-record.md`](../../docs/architectural-decision-record.md).

---

## ADR-T001 — team.yaml as the single deployment manifest

**Decision:** `team.yaml` is the only mechanism for declaring which bots are started. No manual per-bot configuration outside this file and the `bots/<type>/` directory.

**Rationale:** Keeps the team definition in one place. Adding, removing, or disabling a bot is a one-line change in a single file. Prevents configuration drift between what is defined and what is started.

---

## ADR-T002 — New bots default to enabled: false

**Decision:** New entries in `team.yaml` must start with `enabled: false`. They are flipped to `true` only after review of `SOUL.md`, `AGENTS.md`, and `config.yaml`.

**Rationale:** Prevents accidental start of incomplete bot definitions. Provides an explicit review gate before a new bot joins the live team.

---

## ADR-T003 — CDK removed; local runtime only

**Decision:** The `cdk/` directory and all AWS CDK infrastructure have been removed. Bots run as goroutines inside the `boabot` process on the local filesystem, with no cloud infrastructure required.

**Rationale:** Zero-infrastructure developer experience. Anyone can run the full team on a laptop without an AWS account. The domain interface layer ensures cloud-backed adapters can be reintroduced in future without changes to bot personalities or application logic.

**Rejected:** Keeping CDK as optional (maintenance overhead, confusing to have infrastructure code for infrastructure that is not used; the interface layer already provides the upgrade path).
