# Feature Spec — Plugin Registry Support

**Feature:** plugins-and-registry-support
**Created:** 2026-05-07
**Status:** Draft
**Source PRD:** [plugins-and-registry-support-prd.md](./plugins-and-registry-support-prd.md)

---

## Executive Summary

This feature introduces a plugin registry system for BaoBot, allowing admins to browse, install, update, and remove versioned capability packages from one or more hosted registries. Plugins are served as static HTTPS catalogs (GitHub raw content, S3, etc.) and conform to a standardised `plugin.yaml` manifest. A trust model differentiates first-party registries (auto-activate after checksum verification) from third-party registries (require admin approval). The default configuration ships with `stainedhead/shared-plugins` as the first trusted registry. A unified "Plugins & Skills" admin UI and a `boabotctl plugin` CLI subgroup complete the surface.

---

## Problem Statement

Skills today are manually uploaded zip archives requiring admin approval before bots can use them. There is no discovery mechanism, no versioning, no sharing between teams, and no way to distribute capabilities from a central catalog. Each installation is a one-off upload. As the number of bots and capability packages grows this model does not scale.

Teams need a way to browse, install, update, and remove named, versioned plugins from one or more hosted registries — with a trust model that allows first-party plugins to activate without friction and third-party plugins to go through an approval gate.

---

## Goals

- Define a standard plugin manifest format (`plugin.yaml`) that any registry can serve.
- Define a simple, static-file-compatible registry protocol so a GitHub repo (or any HTTPS host) can act as a registry.
- Provide a unified "Plugins & Skills" admin UI supporting browsing, installing, updating, reloading, and removing both plugins and legacy skills.
- Support multiple registries simultaneously, each with an independent trust level.
- Default configuration points to `stainedhead/shared-plugins` as a trusted first-party registry.
- Preserve full backwards compatibility with the existing `SkillRegistry` and uploaded skills.

## Non-Goals

- A hosted registry service is not built — registries are static HTTP catalogs.
- Plugins do not auto-update without explicit admin action.
- Plugin sandboxing beyond the existing subprocess isolation model is out of scope.
- Bot-initiated plugin installation is out of scope. Only admin actions install or update plugins.
- Private/authenticated registries are out of scope. Only anonymous HTTPS registries are supported.
- `min_runtime` version enforcement is out of scope; the field is advisory metadata only.
- Per-bot plugin scoping is out of scope; all active plugins are available to all bots.

---

## User Requirements

### Functional Requirements

**FR-001** — Admin can add a registry by HTTPS URL with a name and trust level; the registry index is fetched and validated immediately.

**FR-002** — Admin can browse the plugin catalog for any configured registry from the admin UI, filtered by name, tag, or author.

**FR-003** — Admin can install a plugin from a trusted registry in one action; the plugin is active and its tools available to bots without additional approval.

**FR-004** — Admin can install a plugin from an untrusted registry; it lands in `staged` status and requires explicit admin approval before tools are available.

**FR-005** — Checksum (SHA-256) of every downloaded archive is verified against the value in `plugin.yaml` before installation; mismatch aborts with no files written.

**FR-006** — Archive extraction enforces zip-slip protection; any member path that escapes the target directory aborts the install and deletes the temp directory.

**FR-007** — Extraction is aborted and the temp directory deleted if total extracted size exceeds 50 MB.

**FR-008** — Admin can reload a plugin without restarting the boabot process; bots pick up changes on the next `ListTools` call.

**FR-009** — Admin can update a specific plugin to a newer version from the same registry; the old version is replaced atomically.

**FR-010** — Admin can disable a plugin (tools hidden from bots, files retained) and re-enable it later.

**FR-011** — Admin can remove a plugin; its files are deleted and tools are no longer returned by `ListTools`.

**FR-012** — Runtime-added registries are persisted to `install_dir/registries.json` and loaded on process restart.

**FR-013** — A structured `slog` line is emitted for every plugin lifecycle event (install, approve, reject, disable, enable, update, reload, remove) with fields: `plugin_name`, `version`, `registry`, `actor`, `status`, `timestamp`.

**FR-014** — Plugin detail view (admin UI) shows full manifest metadata, tool list with schemas, permissions, and checksum.

**FR-015** — `boabotctl plugin list` lists all installed plugins with name, version, registry, status, and installed date.

**FR-016** — `boabotctl plugin info <name>` prints full manifest detail, tool list, and permissions for an installed plugin.

**FR-017** — `boabotctl plugin install <name>` installs the latest version (or `--version`) from the first matching registry (or `--registry`).

**FR-018** — `boabotctl plugin remove <name>` removes an installed plugin.

**FR-019** — `boabotctl plugin reload <name>` reloads a plugin from disk without restarting the runtime.

**FR-020** — All existing skill upload, approve, reject, revoke flows continue to work without change.

**FR-021** — Tool name collisions across active plugins are detected at activation time; the conflicting plugin is disabled with an admin warning.

**FR-022** — `stainedhead/shared-plugins` is the default trusted registry in the default `config.yaml` template.

---

## Non-Functional Requirements

### Performance
- Registry index fetches: 10-second timeout.
- Plugin archive downloads: 60-second timeout.
- Maximum wire size: 20 MB compressed. Requests exceeding this are rejected before extraction.
- Maximum extracted size: 50 MB (see FR-007).
- Registry index cached in memory with 5-minute TTL. "Reload" action forces a fresh fetch.

### Reliability
- Plugin install is atomic: extract to temp dir → validate → rename. Any failure deletes the temp dir. No partially-extracted plugin ever exists in `install_dir`.
- `install_dir` is persistent; for ECS deployments it must be mounted on a persistent volume (same contract as `memory/`).
- Registries persisted in `install_dir/registries.json` survive process restarts.

### Security
- All registry fetches over HTTPS only; HTTP URLs rejected at config validation.
- Checksum verification for every install (FR-005).
- Zip-slip protection (FR-006).
- Plugin entrypoints run in existing subprocess sandbox; network limited to `permissions.network` hosts; env vars limited to `permissions.env_vars` declarations.
- Trusted registries skip approval gate but not checksum verification.

### Observability
- Structured `slog` audit log for all lifecycle events (FR-013).

---

## System Architecture

### Affected Layers

| Layer | Changes |
|---|---|
| **Domain** | New: `plugin.go` — Plugin, PluginStatus, PluginManifest, PluginStore, RegistryClient, RegistryIndex, RegistryEntry types |
| **Application** | New use cases: InstallPlugin, ApprovePlugin, RejectPlugin, EnablePlugin, DisablePlugin, UpdatePlugin, ReloadPlugin, RemovePlugin, ListRegistries, AddRegistry, RemoveRegistry, FetchRegistryIndex |
| **Infrastructure/local** | New: `infrastructure/local/plugin/` — filesystem PluginStore; `infrastructure/http/registry_client.go` — HTTP RegistryClient |
| **Infrastructure/mcp** | Modified: `infrastructure/local/mcp/client.go` — dynamic tool loading from `install_dir/` |
| **Infrastructure/http** | Modified: `infrastructure/http/server.go` — new REST endpoint group; admin UI tab replacement |
| **boabotctl** | New: `plugin` subcommand group |

### New/Modified Components

- `boabot/internal/domain/plugin.go` — all plugin domain types and interfaces
- `boabot/internal/application/plugin/` — plugin use case package
- `boabot/internal/infrastructure/local/plugin/store.go` — filesystem PluginStore
- `boabot/internal/infrastructure/local/plugin/installer.go` — atomic archive extraction, checksum, zip-slip
- `boabot/internal/infrastructure/http/registry_client.go` — HTTP RegistryClient with cache
- `boabot/internal/infrastructure/http/server.go` — new `/api/v1/registries` and `/api/v1/plugins` handler groups; admin UI tab
- `boabot/internal/infrastructure/local/mcp/client.go` — dynamic `ListTools` / `CallTool` for installed plugins
- `boabotctl/cmd/boabotctl/plugin.go` — `plugin` subcommand group

---

## Scope of Changes

### Files to Create

| File | Purpose |
|---|---|
| `boabot/internal/domain/plugin.go` | Domain types, PluginStore, RegistryClient interfaces |
| `boabot/internal/domain/mocks/plugin_mocks.go` | Test doubles for PluginStore and RegistryClient |
| `boabot/internal/application/plugin/install.go` | InstallPlugin use case |
| `boabot/internal/application/plugin/install_test.go` | TDD tests |
| `boabot/internal/application/plugin/manage.go` | Approve/reject/enable/disable/update/reload/remove use cases |
| `boabot/internal/application/plugin/manage_test.go` | TDD tests |
| `boabot/internal/application/plugin/registry.go` | Registry list/add/remove/fetch use cases |
| `boabot/internal/application/plugin/registry_test.go` | TDD tests |
| `boabot/internal/infrastructure/local/plugin/store.go` | PluginStore filesystem implementation |
| `boabot/internal/infrastructure/local/plugin/store_test.go` | Unit tests |
| `boabot/internal/infrastructure/local/plugin/installer.go` | Atomic extraction, checksum, zip-slip |
| `boabot/internal/infrastructure/local/plugin/installer_test.go` | Unit tests |
| `boabot/internal/infrastructure/http/registry_client.go` | HTTP RegistryClient with TTL cache |
| `boabot/internal/infrastructure/http/registry_client_test.go` | Unit tests (HTTP mock) |
| `boabotctl/cmd/boabotctl/plugin.go` | boabotctl plugin subcommand group |
| `boabotctl/cmd/boabotctl/plugin_test.go` | CLI output formatting tests |

### Files to Modify

| File | Change |
|---|---|
| `boabot/internal/infrastructure/local/mcp/client.go` | Dynamic tool loading from `install_dir/`; subprocess dispatch for plugin tools |
| `boabot/internal/infrastructure/http/server.go` | New API handler groups; unified Plugins & Skills admin UI tab |
| `boabot/internal/infrastructure/local/config/config.go` | New `plugins` config block |
| `boabot/cmd/boabot/main.go` | Wire PluginStore and RegistryClient into use cases and MCP client |
| `boabot/docs/technical-details.md` | Document plugin architecture |
| `boabot/docs/architectural-decision-record.md` | ADR for plugin manifest format and registry protocol |
| `boabot/README.md` | Plugin system overview |

### New Dependencies

- No new external Go modules expected. HTTP client uses stdlib `net/http`. YAML parsing uses `gopkg.in/yaml.v3` (already in tree via config). Archive extraction uses stdlib `archive/tar` + `compress/gzip`.

---

## Breaking Changes

None. All changes are additive. The existing `SkillRegistry` interface and endpoints are unchanged.

---

## Success Criteria and Acceptance Criteria

See PRD Acceptance Criteria section. Quality gates:

- All tests pass: `go test -race ./...`
- Coverage ≥ 90% on `internal/domain/...` and `internal/application/...` (excluding `mocks/`)
- Linter clean: `golangci-lint run`
- All 22 FRs implemented and verified by at least one test each

---

## Risks and Mitigation

| Risk | Likelihood | Mitigation |
|---|---|---|
| Archive extraction security (zip-slip) | Medium | Explicit path validation in installer.go before each member write |
| Tool name collision across plugins | Low | Detected at activation; conflicting plugin disabled with warning |
| Registry unavailable at install time | Medium | Timeout + clear error message; no partial state left |
| ECS `install_dir` not on persistent volume | Medium | Document as operational requirement; log a warning on startup if dir is tmpfs |

---

## Timeline and Milestones

| Phase | Deliverable |
|---|---|
| Phase 1 | Domain types + PluginStore + RegistryClient (with tests) |
| Phase 2 | REST API endpoints (with integration tests) |
| Phase 3 | MCP client dynamic tool loading (with tests) |
| Phase 4 | Admin UI — Plugins & Skills tab |
| Phase 5 | Default registry wiring (`stainedhead/shared-plugins`) |
| Phase 6 | boabotctl plugin subcommands |

---

## References

- Source PRD: [plugins-and-registry-support-prd.md](./plugins-and-registry-support-prd.md)
- Existing skill implementation: `boabot/internal/domain/skill.go`, `boabot/internal/infrastructure/local/mcp/client.go`
- Existing config: `boabot/internal/infrastructure/local/config/config.go`
