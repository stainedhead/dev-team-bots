# Architecture — Plugin Registry Support

**Feature:** plugins-and-registry-support
**Created:** 2026-05-07
**Status:** Draft

---

## Architecture Overview

The plugin registry system adds a new capability layer between the domain interfaces and the existing MCP tool dispatch. Plugins are filesystem-resident packages; their lifecycle is managed through domain interfaces that the infrastructure layer implements. The MCP client remains the single entry point for tool dispatch — it now scans the plugin directory dynamically on each `ListTools` call.

Clean Architecture boundaries are strictly maintained:
- Domain layer: types + interfaces only (`plugin.go`)
- Application layer: use cases orchestrating PluginStore and RegistryClient
- Infrastructure layer: filesystem PluginStore, HTTP RegistryClient, modified MCP client, new REST handlers

---

## Component Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  boabotctl CLI                                              │
│  plugin {list,info,install,remove,reload}                   │
└──────────────────────┬──────────────────────────────────────┘
                       │ REST API calls
┌──────────────────────▼──────────────────────────────────────┐
│  HTTP Server (infrastructure/http/server.go)                │
│  /api/v1/registries  /api/v1/plugins                        │
└──────────┬──────────────────────────────────────────────────┘
           │
┌──────────▼──────────────────────────────────────────────────┐
│  Application Layer (application/plugin/)                    │
│  InstallPlugin  ApprovePlugin  RejectPlugin                 │
│  EnablePlugin   DisablePlugin  UpdatePlugin                 │
│  ReloadPlugin   RemovePlugin                                │
│  ListRegistries AddRegistry    RemoveRegistry               │
│  FetchRegistryIndex                                         │
└──────────┬───────────────────────────────┬──────────────────┘
           │ PluginStore                   │ RegistryClient
┌──────────▼──────────────┐  ┌────────────▼────────────────────┐
│  local/plugin/store.go  │  │  http/registry_client.go        │
│  filesystem PluginStore │  │  HTTP RegistryClient + TTL cache │
│  install_dir/ layout    │  │  FetchIndex/FetchManifest/       │
│  atomic install         │  │  FetchArchive                    │
│  status.json sidecar    │  └─────────────────────────────────┘
└─────────────────────────┘
           │
┌──────────▼──────────────────────────────────────────────────┐
│  local/mcp/client.go (modified)                             │
│  ListTools: scans install_dir/ for active plugins           │
│  CallTool: dispatches to plugin entrypoint subprocess       │
│  (existing built-in tools unchanged)                        │
└─────────────────────────────────────────────────────────────┘
```

---

## Layer Responsibilities

### Domain (`domain/plugin.go`)

- Defines `Plugin`, `PluginStatus`, `PluginManifest`, `PluginProvides`, `PluginPermissions`, `PluginRegistry`, `RegistryIndex`, `RegistryEntry`.
- Defines `PluginStore` and `RegistryClient` interfaces.
- No imports outside stdlib and other domain packages.

### Application (`application/plugin/`)

- Use cases accept `PluginStore` and `RegistryClient` as constructor parameters (dependency injection).
- `InstallPlugin` use case: fetches manifest from registry, verifies checksum, calls `PluginStore.Install`, emits audit log.
- `FetchRegistryIndex` use case: wraps `RegistryClient.FetchIndex`; index cache lives in the RegistryClient adapter.
- All audit logging via `slog` happens in the use cases (not the infrastructure adapters).

### Infrastructure — local/plugin/

- `store.go`: filesystem PluginStore. Reads/writes `status.json` per plugin. Implements all lifecycle transitions.
- `installer.go`: atomic extraction. Extracts tar.gz to a temp dir, validates all member paths (zip-slip), enforces 50 MB cap, verifies SHA-256. On success: renames temp dir to final. On failure: deletes temp dir.

### Infrastructure — http/registry_client.go

- HTTP client with configurable timeouts (10s for index/manifest, 60s for archive).
- In-memory index cache: `map[string]cachedIndex` guarded by `sync.RWMutex`; 5-minute TTL per registry URL.
- Wire-size check: rejects archive responses exceeding 20 MB before reading body.

### Infrastructure — local/mcp/client.go (modified)

- `ListTools`: after returning built-in tools, scans `install_dir/` for subdirectories containing `status.json` with `status: active`. For each, parses `plugin.yaml` and appends tool entries.
- `CallTool`: if the tool name matches an active plugin tool, launches the plugin's entrypoint subprocess (inheriting the existing sandbox model) rather than the built-in dispatch.
- Tool name collision: at `ListTools` time, if two active plugins declare the same tool name, the second is excluded and a warning is logged.

---

## Data Flow

### Plugin Install (trusted registry)

```
Admin clicks Install
  → POST /api/v1/plugins {registry, name, version}
  → InstallPlugin use case
  → RegistryClient.FetchManifest(manifestURL)
  → RegistryClient.FetchArchive(downloadURL)
  → verify sha256(archive) == manifest.Checksums["sha256"]
  → PluginStore.Install(manifest, archive, registry)
      → installer.Extract(archive, tempDir)     [zip-slip + 50MB check]
      → write status.json {status: "active"}
      → rename tempDir → install_dir/<name>/
  → slog.Info("plugin.install", ...)
  → return Plugin{Status: active}
```

### Plugin Install (untrusted registry)

Same as above but `status.json` is written with `status: "staged"`. Admin must call `POST /api/v1/plugins/{id}/approve` to transition to `active`.

### ListTools (with plugins)

```
MCP client.ListTools(ctx)
  → return built-in tools
  → scan install_dir/ for active plugins
  → for each active plugin: parse plugin.yaml, append provides.tools
  → return merged tool list
```

---

## Sequence Diagrams

[TBD — to be expanded during Phase 1 implementation]

---

## Integration Points

| System | Integration |
|---|---|
| Registry (GitHub raw) | HTTPS GET via RegistryClient |
| Existing SkillRegistry | Unchanged; surfaced alongside plugins in UI |
| boabotctl | Calls boabot REST API using existing token mechanism |
| ECS `install_dir` volume | Must be persistent; operational concern documented in user-docs |

---

## Architectural Decisions

**AD-1 — Static file registry protocol:** Chosen to minimise operational cost. Any HTTPS file host works as a registry; no dedicated registry service to operate.

**AD-2 — In-memory index cache in RegistryClient adapter:** Placing cache in the infrastructure adapter keeps the application use cases stateless and simpler to test. The cache is implementation detail of the HTTP adapter.

**AD-3 — Atomic install via temp-dir rename:** Prevents partial plugin state in `install_dir`. A rename within the same filesystem is atomic on Linux/macOS.

**AD-4 — Tool collision resolved at ListTools time:** Simplest point to enforce: plugins are loaded dynamically; collision is a runtime concern, not a store concern.
