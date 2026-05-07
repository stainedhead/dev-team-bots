# Plan — Plugin Registry Support

**Feature:** plugins-and-registry-support
**Created:** 2026-05-07
**Status:** Planning

---

## Development Approach

- TDD throughout: Red → Green → Refactor for every production file.
- Clean Architecture strictly enforced: no infrastructure imports in domain or application layers.
- Phases are sequential; each phase must have passing tests and clean lint before the next begins.
- Parallel workstreams within a phase are permitted where tasks are independent.

---

## Phase Breakdown

### Phase 1 — Domain and RegistryClient

**Goal:** All domain types defined, PluginStore and RegistryClient interfaces specified, filesystem PluginStore implemented and tested, HTTP RegistryClient implemented and tested.

Tasks:
- P1.1 — Write `domain/plugin.go` (types + interfaces)
- P1.2 — Write `domain/mocks/plugin_mocks.go`
- P1.3 — Write installer.go (atomic extraction, checksum, zip-slip, 50MB cap) + tests
- P1.4 — Write `infrastructure/local/plugin/store.go` + tests
- P1.5 — Write `infrastructure/http/registry_client.go` (fetch + TTL cache) + tests

### Phase 2 — REST API

**Goal:** All plugin and registry REST endpoints implemented, wired into the server, covered by handler tests.

Tasks:
- P2.1 — Write application use cases (install, approve, reject, enable, disable, update, reload, remove, list, get)
- P2.2 — Write application use cases for registry management (list, add, remove, fetch index)
- P2.3 — Add handler groups to `server.go` (`/api/v1/plugins`, `/api/v1/registries`)
- P2.4 — Integration tests for install and approval flows

### Phase 3 — MCP Client Integration

**Goal:** `ListTools` returns active plugin tools; `CallTool` dispatches to plugin entrypoints; collision detection works.

Tasks:
- P3.1 — Modify `infrastructure/local/mcp/client.go` for dynamic tool loading
- P3.2 — Implement subprocess dispatch for plugin entrypoints
- P3.3 — Tool name collision detection + warning
- P3.4 — Unit tests for all three behaviours

### Phase 4 — Admin UI

**Goal:** Unified Plugins & Skills tab live in the admin UI with all defined interactions.

Tasks:
- P4.1 — Replace Skills tab with Plugins & Skills tab skeleton
- P4.2 — Registry Browser section (selector, search, card grid, Add Registry modal)
- P4.3 — Installed Plugins table with per-row actions
- P4.4 — Plugin Detail Panel
- P4.5 — Legacy skills section (unchanged functionality, new label)

### Phase 5 — Default Registry Wiring

Tasks:
- P5.1 — Add `stainedhead/shared-plugins` as default trusted registry in `config.yaml` template
- P5.2 — Publish bootstrap `index.json` to `stainedhead/shared-plugins` on GitHub

### Phase 6 — boabotctl Plugin Subcommands

Tasks:
- P6.1 — `plugin list` command
- P6.2 — `plugin info <name>` command
- P6.3 — `plugin install <name>` command (with `--registry`, `--version` flags)
- P6.4 — `plugin remove <name>` command
- P6.5 — `plugin reload <name>` command
- P6.6 — CLI output formatting tests

---

## Critical Path

Phase 1 → Phase 2 → Phase 3 → Phase 4 (Phases 5 and 6 can run in parallel with Phase 4)

---

## Testing Strategy

- Unit tests: mock all interfaces; test use cases and infrastructure adapters in isolation.
- Integration tests: Phase 2 handler tests spin up an in-process HTTP server.
- Security tests: dedicated tests for zip-slip and 50 MB cap in `installer_test.go`.
- Regression: all existing skill tests must continue to pass throughout.

---

## Rollout Strategy

Feature is guarded by the `orchestrator.plugins` config block. If the block is absent, no plugin infrastructure is initialised. Existing skill behaviour is unaffected.

---

## Success Metrics

- All 22 FRs covered by at least one test.
- Coverage ≥ 90% on domain and application layers.
- Zero linter warnings.
- All existing tests still pass.
