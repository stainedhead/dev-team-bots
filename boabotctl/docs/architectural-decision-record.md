# Architectural Decision Record — boabotctl

Module-specific decisions. For system-level decisions see root [`docs/architectural-decision-record.md`](../../docs/architectural-decision-record.md).

---

## ADR-C001 — cobra for CLI command structure

**Decision:** Use `cobra` for command and subcommand definition.

**Rationale:** Standard Go CLI library. Provides flag parsing, help generation, and command grouping with minimal boilerplate. Well-understood by Go contributors.

---

## ADR-C002 — OrchestratorClient interface as the test seam

**Decision:** All command handlers depend on an `OrchestratorClient` interface, not a concrete HTTP client.

**Rationale:** Allows unit testing of all command handlers with a mock client. No real HTTP calls in unit tests. The HTTP implementation is integration-tested separately.

---

## ADR-C003 — JWT stored in credentials file, not config

**Decision:** The JWT is stored in `~/.baobotctl/credentials` at mode 0600, separate from `~/.baobotctl/config.yaml`.

**Rationale:** Separates secret (credential) from configuration. Reduces the risk of accidentally sharing config files that contain active session tokens.

---

## ADR-C004 — JSON output flag on all commands

**Decision:** All commands support `--output json` for machine-readable output.

**Rationale:** Enables scripting and integration with other tools without screen-scraping. Default output remains human-readable tables.
