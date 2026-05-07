# Data Dictionary — Plugin Registry Support

**Feature:** plugins-and-registry-support
**Created:** 2026-05-07

---

## Purpose

This document defines all data structures introduced or modified by the plugin registry feature. Entries are grouped by layer.

---

## Domain Entities

### Plugin

Represents an installed plugin (registry-sourced or manually uploaded).

| Field | Type | Description |
|---|---|---|
| ID | string | UUID, assigned at install time |
| Name | string | Plugin name from manifest |
| Version | string | Semver string |
| Description | string | Human-readable description |
| Author | string | Publisher identifier |
| Registry | string | Registry name; empty for manual uploads |
| Status | PluginStatus | Current lifecycle state |
| InstalledAt | time.Time | Wall-clock install time |
| Manifest | PluginManifest | Full parsed manifest |

### PluginManifest

The in-memory representation of `plugin.yaml`.

| Field | Type | YAML key |
|---|---|---|
| Name | string | `name` |
| Version | string | `version` |
| Description | string | `description` |
| Author | string | `author` |
| Homepage | string | `homepage` |
| License | string | `license` |
| Tags | []string | `tags` |
| MinRuntime | string | `min_runtime` |
| Provides | PluginProvides | `provides` |
| Permissions | PluginPermissions | `permissions` |
| Entrypoint | string | `entrypoint` |
| Checksums | map[string]string | `checksums` |

### PluginProvides

| Field | Type | YAML key |
|---|---|---|
| Tools | []MCPTool | `tools` |

### PluginPermissions

| Field | Type | YAML key |
|---|---|---|
| Network | []string | `network` |
| EnvVars | []string | `env_vars` |
| Filesystem | bool | `filesystem` |

---

## Enumerations

### PluginStatus

| Value | Description |
|---|---|
| `downloading` | Archive fetch in progress |
| `staged` | Downloaded + checksum verified; awaiting admin approval (untrusted registry) |
| `active` | Approved or trusted-registry install; tools visible to bots |
| `disabled` | Admin-disabled; tools hidden, files retained |
| `update_available` | Newer version detected in registry index |
| `rejected` | Admin rejected; terminal state |
| `checksum_fail` | SHA-256 mismatch; terminal state |

---

## Registry Types

### PluginRegistry

Runtime representation of a configured registry.

| Field | Type | Config key |
|---|---|---|
| Name | string | `name` |
| URL | string | `url` |
| Trusted | bool | `trusted` |

### RegistryIndex

In-memory parse of a registry's `index.json`.

| Field | Type | JSON key |
|---|---|---|
| Registry | string | `registry` |
| GeneratedAt | time.Time | `generated_at` |
| Plugins | []RegistryEntry | `plugins` |

### RegistryEntry

One entry in the registry index.

| Field | Type | JSON key |
|---|---|---|
| Name | string | `name` |
| Description | string | `description` |
| Author | string | `author` |
| LatestVersion | string | `latest_version` |
| Tags | []string | `tags` |
| Versions | []string | `versions` |
| ManifestURL | string | `manifest_url` |
| DownloadURL | string | `download_url` |

---

## Interfaces

### PluginStore

```go
type PluginStore interface {
    List(ctx context.Context) ([]Plugin, error)
    Get(ctx context.Context, id string) (Plugin, error)
    Install(ctx context.Context, manifest PluginManifest, archive []byte, registry string) (Plugin, error)
    Approve(ctx context.Context, id string) error
    Reject(ctx context.Context, id string) error
    Disable(ctx context.Context, id string) error
    Enable(ctx context.Context, id string) error
    Update(ctx context.Context, id string, manifest PluginManifest, archive []byte) error
    Reload(ctx context.Context, id string) error
    Remove(ctx context.Context, id string) error
}
```

### RegistryClient

```go
type RegistryClient interface {
    FetchIndex(ctx context.Context, registryURL string) (RegistryIndex, error)
    FetchManifest(ctx context.Context, manifestURL string) (PluginManifest, error)
    FetchArchive(ctx context.Context, downloadURL string) ([]byte, error)
}
```

---

## Config Types

### PluginsConfig (new block in config.yaml)

```go
type PluginsConfig struct {
    InstallDir string           `yaml:"install_dir"`
    Registries []PluginRegistry `yaml:"registries"`
    AutoUpdate bool             `yaml:"auto_update"`
}
```

---

## Persistence

### `install_dir/registries.json`

Array of `PluginRegistry` objects, JSON-encoded. Written whenever a registry is added or removed at runtime. Registries declared in `config.yaml` are not written here — they are always loaded from config on startup.

### `install_dir/<plugin-name>/status.json`

Sidecar file per installed plugin. Contains the `Plugin` struct (JSON-encoded, sans `Manifest` which is re-read from `plugin.yaml` on demand).

---

## API Request/Response Types

### POST /api/v1/plugins (install request)

```go
type InstallPluginRequest struct {
    Registry string `json:"registry"`
    Name     string `json:"name"`
    Version  string `json:"version,omitempty"` // empty = latest
}
```

### POST /api/v1/registries (add registry request)

```go
type AddRegistryRequest struct {
    Name    string `json:"name"`
    URL     string `json:"url"`
    Trusted bool   `json:"trusted"`
}
```
