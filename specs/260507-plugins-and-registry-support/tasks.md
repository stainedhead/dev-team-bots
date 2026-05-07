# Tasks — Plugin Registry Support

**Feature:** plugins-and-registry-support
**Created:** 2026-05-07
**Status:** Planning

---

## Progress Summary

**0 / 26 tasks complete**

---

## Phase 1 — Domain and RegistryClient

| ID | Task | Status | Deps | Est |
|----|------|--------|------|-----|
| P1.1 | Write `domain/plugin.go` — all types + PluginStore + RegistryClient interfaces | ⬜ | — | 1h |
| P1.2 | Write `domain/mocks/plugin_mocks.go` — test doubles for PluginStore and RegistryClient | ⬜ | P1.1 | 30m |
| P1.3 | Write `infrastructure/local/plugin/installer.go` + `installer_test.go` — atomic extraction, checksum, zip-slip, 50MB cap | ⬜ | P1.1 | 2h |
| P1.4 | Write `infrastructure/local/plugin/store.go` + `store_test.go` — filesystem PluginStore | ⬜ | P1.1, P1.3 | 2h |
| P1.5 | Write `infrastructure/http/registry_client.go` + `registry_client_test.go` — HTTP fetches, TTL cache, timeouts, wire-size limit | ⬜ | P1.1 | 2h |

**Acceptance:** All P1 tests pass; coverage ≥ 90% on new packages; linter clean.

---

## Phase 2 — REST API

| ID | Task | Status | Deps | Est |
|----|------|--------|------|-----|
| P2.1 | Write `application/plugin/install.go` + `install_test.go` — InstallPlugin use case | ⬜ | P1.2 | 1.5h |
| P2.2 | Write `application/plugin/manage.go` + `manage_test.go` — approve, reject, enable, disable, update, reload, remove | ⬜ | P1.2 | 2h |
| P2.3 | Write `application/plugin/registry.go` + `registry_test.go` — list/add/remove/fetch use cases | ⬜ | P1.2 | 1h |
| P2.4 | Add `/api/v1/plugins` handler group to `server.go` | ⬜ | P2.1, P2.2 | 2h |
| P2.5 | Add `/api/v1/registries` handler group to `server.go` | ⬜ | P2.3 | 1h |
| P2.6 | Integration tests for install + approval flows | ⬜ | P2.4, P2.5 | 1.5h |

**Acceptance:** All 10 plugin endpoints and 4 registry endpoints respond correctly; integration tests pass.

---

## Phase 3 — MCP Client Integration

| ID | Task | Status | Deps | Est |
|----|------|--------|------|-----|
| P3.1 | Modify `infrastructure/local/mcp/client.go` — `ListTools` scans install_dir for active plugins | ⬜ | P1.4 | 1.5h |
| P3.2 | Implement subprocess dispatch for plugin entrypoints in `CallTool` | ⬜ | P3.1 | 1.5h |
| P3.3 | Tool name collision detection + warning log | ⬜ | P3.1 | 30m |
| P3.4 | Unit tests for dynamic loading, dispatch, collision detection | ⬜ | P3.1, P3.2, P3.3 | 1h |

**Acceptance:** Active plugin tools appear in `ListTools`; `CallTool` dispatches to entrypoint; collision is detected and logged.

---

## Phase 4 — Admin UI

| ID | Task | Status | Deps | Est |
|----|------|--------|------|-----|
| P4.1 | Replace Skills tab skeleton with Plugins & Skills tab in `server.go` | ⬜ | P2.4 | 30m |
| P4.2 | Registry Browser section — selector, search, card grid, install badge, Add Registry modal | ⬜ | P4.1 | 2h |
| P4.3 | Installed Plugins table — columns, status badges, per-row action buttons | ⬜ | P4.1 | 2h |
| P4.4 | Plugin Detail Panel — manifest metadata, tools, permissions, checksum, action buttons | ⬜ | P4.3 | 1.5h |
| P4.5 | Legacy skills section — unchanged functionality, "Manually uploaded skills" label | ⬜ | P4.1 | 30m |

**Acceptance:** All admin UI interactions complete without JS errors; installed plugins appear in table; detail panel shows full manifest data.

---

## Phase 5 — Default Registry Wiring

| ID | Task | Status | Deps | Est |
|----|------|--------|------|-----|
| P5.1 | Add `stainedhead/shared-plugins` as default trusted registry in `config.yaml` template | ⬜ | P2.5 | 30m |
| P5.2 | Publish bootstrap `index.json` to `stainedhead/shared-plugins` GitHub repo | ⬜ | P5.1 | 30m |

---

## Phase 6 — boabotctl Plugin Subcommands

| ID | Task | Status | Deps | Est |
|----|------|--------|------|-----|
| P6.1 | `boabotctl plugin list` — calls GET /api/v1/plugins, renders table | ⬜ | P2.4 | 1h |
| P6.2 | `boabotctl plugin info <name>` — calls GET /api/v1/plugins/{id}, renders detail view | ⬜ | P2.4 | 1h |
| P6.3 | `boabotctl plugin install <name>` — with --registry and --version flags | ⬜ | P2.4 | 1h |
| P6.4 | `boabotctl plugin remove <name>` | ⬜ | P2.4 | 30m |
| P6.5 | `boabotctl plugin reload <name>` | ⬜ | P2.4 | 30m |
| P6.6 | CLI output formatting tests | ⬜ | P6.1, P6.2 | 1h |

**Acceptance:** All 5 CLI commands produce correct output; write operations require admin credentials; list/info are read-only.
