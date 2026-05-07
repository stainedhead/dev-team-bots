# Plugin System Configuration Reference

This document covers all configuration options for the BaoBot plugin system.

## The `orchestrator.plugins` Block

The plugin system is configured under the orchestrator section of `config.yaml`. It is active only when `orchestrator.enabled: true`.

```yaml
orchestrator:
  enabled: true
  api_port: 8080
  plugins:
    install_dir: ./plugins
    registries:
      - name: shared-plugins
        url: https://raw.githubusercontent.com/stainedhead/shared-plugins/main
        trusted: true
    auto_update: false
```

### Fields

| Field | Type | Default | Description |
|---|---|---|---|
| `install_dir` | string | `""` | Directory where plugin archives are extracted. Must be set if the plugin system is used. Relative paths are resolved relative to the config file. |
| `registries` | list | `[]` | Statically configured registries. These take precedence over runtime-added registries of the same name. |
| `auto_update` | bool | `false` | Reserved for future use. No automatic updates are performed at runtime; all updates require explicit admin action. |

If `install_dir` is empty or omitted, the plugin system is disabled and no plugin routes are registered.

## Per-Registry Fields

Each entry in `registries` has three fields:

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Short identifier. Used in the CLI (`--registry <name>`) and the API path (`/api/v1/registries/{name}/index`). Must be unique. |
| `url` | string | yes | HTTPS base URL of the registry. The runtime appends `/index.json` to this URL to fetch the catalog. Must start with `https://`. |
| `trusted` | bool | no (default: `false`) | When `true`, plugins from this registry activate immediately after checksum verification. When `false`, plugins land in `staged` status and require admin approval. |

Example with two registries:

```yaml
plugins:
  install_dir: ./plugins
  registries:
    - name: shared-plugins
      url: https://raw.githubusercontent.com/stainedhead/shared-plugins/main
      trusted: true
    - name: community
      url: https://plugins.example.com
      trusted: false
```

## ECS Persistent Volume Requirement

`install_dir` stores extracted plugin files and `registries.json`. These must survive process restarts.

On ECS, `install_dir` **must be mounted on a persistent volume** (EFS or equivalent). If `install_dir` is on ephemeral container storage, all plugins and runtime-added registries are lost when the task is replaced.

The persistent volume requirement for `install_dir` is the same as for `memory.path` — both directories store state that must outlive any single task instance.

On a persistent volume, the typical layout is:

```
/data/
  memory/          # memory.path
  plugins/         # orchestrator.plugins.install_dir
```

## Runtime Registry Additions

Registries added via `POST /api/v1/registries` or via the admin UI at runtime are persisted to `<install_dir>/registries.json`. This file is loaded on startup and merged with the statically configured registries. If a runtime-added registry has the same name as a statically configured one, the static configuration takes precedence.

The `registries.json` file is managed automatically by the runtime. Do not edit it by hand while the process is running.

## Startup Behaviour

On startup, the runtime:

1. Calls `os.MkdirAll(install_dir)` to create the directory if it does not exist. If this fails, the plugin system is disabled and existing skills continue to work.
2. Scans `install_dir` subdirectories for `status.json` files and loads them into the in-memory plugin index. Subdirectories with missing or corrupt `status.json` are skipped with a warning log — they do not prevent startup.
3. Loads `registries.json` and merges runtime-added registries with statically configured ones.

## Credential Note

Registries must be publicly accessible via anonymous HTTPS. Credentials for accessing private registries are not supported.
