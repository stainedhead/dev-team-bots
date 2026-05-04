# Architectural Decision Record — boabot

Module-specific decisions. For system-level decisions see root [`docs/architectural-decision-record.md`](../../docs/architectural-decision-record.md).

---

## ADR-B001 — Worker goroutines recover from panics

**Decision:** Each worker goroutine wraps its execution in a `recover()`. A panicking worker logs the error and exits cleanly without propagating to the main thread.

**Rationale:** Worker tasks are agentic and unpredictable. A single bad task must not crash the agent. The main thread and other workers continue unaffected.

---

## ADR-B002 — Config loaded from filesystem, secrets from Secrets Manager

**Decision:** Non-secret configuration is loaded from `config.yaml` next to the binary. Secrets (API keys, DB credentials) are loaded from AWS Secrets Manager at startup using the bot's IAM role.

**Rationale:** Keeps secrets out of config files and git. Config files are safe to inspect and version (without secrets). The IAM role provides scoped, auditable access to secrets.

---

## ADR-B003 — Orchestrator mode is additive, not a separate binary

**Decision:** Orchestrator features (control plane, Kanban board, REST API, web UI) are activated by a config flag in the standard bot binary — not a separate binary or container image.

**Rationale:** Maintains a single delivery artefact. The orchestrator is operationally a bot with extra responsibilities, not a fundamentally different system. The config flag gates all orchestrator code paths cleanly.

---

## ADR-B004 — MCP config merged from shared and private sources

**Decision:** MCP configuration is loaded from two optional S3 locations and merged at startup. Private config extends (not replaces) shared config.

**Rationale:** Allows team-wide tools to be defined once while enabling role-specific tools without coordination overhead. Missing files are not errors — the system operates on whatever is present.
