# Plugin Registry — REST API Reference

All plugin and registry endpoints are under `/api/v1/`. They are registered only when the plugin system is configured (`orchestrator.plugins.install_dir` is set).

Authentication uses a Bearer JWT (obtained from `POST /api/v1/auth/login`). Endpoints marked **Admin only** return 403 for non-admin callers.

---

## Registry Endpoints

### GET /api/v1/registries

List all configured registries.

**Auth:** None required.

**Response 200:**

```json
[
  {
    "name": "shared-plugins",
    "url": "https://raw.githubusercontent.com/stainedhead/shared-plugins/main",
    "trusted": true
  }
]
```

---

### POST /api/v1/registries

Add a new registry. The registry index is fetched immediately to validate the URL.

**Auth:** Admin only.

**Request body:**

```json
{
  "name": "my-registry",
  "url": "https://plugins.example.com",
  "trusted": false
}
```

`url` must start with `https://`. HTTP URLs are rejected with 400.

**Response 201:** The created `PluginRegistry` object.

**Response 400:** Invalid request body or HTTP URL.

---

### DELETE /api/v1/registries/{name}

Remove a registry by name.

**Auth:** Admin only.

**Response 204:** Registry removed.

**Response 404:** Registry not found.

---

### GET /api/v1/registries/{name}/index

Fetch the registry catalog for the named registry. Uses the cached index if it is less than 5 minutes old. Pass `?force=true` to bypass the cache.

**Auth:** Admin only.

**Query params:**

| Param | Type | Default | Description |
|---|---|---|---|
| `force` | bool | `false` | Bypass the 5-minute TTL cache and fetch fresh from the registry |

**Response 200:**

```json
{
  "registry": "stainedhead/shared-plugins",
  "generated_at": "2026-05-07T00:00:00Z",
  "plugins": [
    {
      "name": "my-plugin",
      "description": "Does something useful",
      "author": "your-name",
      "latest_version": "1.0.0",
      "tags": ["utility"],
      "versions": ["1.0.0", "0.9.0"],
      "manifest_url": "https://.../my-plugin/1.0.0/plugin.yaml",
      "download_url": "https://.../my-plugin/1.0.0/my-plugin.tar.gz"
    }
  ]
}
```

**Response 404:** Registry not found.

**Response 502:** Registry fetch failed (network error or non-2xx response).

---

## Plugin Endpoints

### GET /api/v1/plugins

List all installed plugins.

**Auth:** None required.

**Response 200:** Array of `Plugin` objects, sorted by `installed_at` descending.

```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "my-plugin",
    "version": "1.0.0",
    "description": "Does something useful",
    "author": "your-name",
    "registry": "shared-plugins",
    "status": "active",
    "installed_at": "2026-05-07T12:00:00Z",
    "manifest": { ... }
  }
]
```

---

### GET /api/v1/plugins/{id}

Get a single installed plugin by ID.

**Auth:** None required.

**Response 200:** `Plugin` object (same shape as above, with full `manifest` field).

**Response 404:** Plugin not found.

---

### POST /api/v1/plugins

Install a plugin from a registry.

**Auth:** Admin only.

**Request body:**

```json
{
  "registry": "shared-plugins",
  "name": "my-plugin",
  "version": ""
}
```

`version` is optional. When empty, the latest version from the registry index is installed.

**Response 201:** The created `Plugin` object. `status` is `active` for trusted registries or `staged` for untrusted registries.

**Response 400:** Missing required fields.

**Response 404:** Registry or plugin not found in the registry index.

**Response 409:** A plugin with the same name is already installed in a non-terminal state.

**Response 500:** Checksum mismatch, zip-slip detected, size limit exceeded, or write failure.

---

### POST /api/v1/plugins/{id}/approve

Approve a staged plugin, transitioning its status to `active`.

**Auth:** Admin only.

**Response 200:** Empty body.

**Response 404:** Plugin not found.

---

### POST /api/v1/plugins/{id}/reject

Reject a staged plugin. The plugin files are deleted and the plugin record is removed.

**Auth:** Admin only.

**Response 200:** Empty body.

**Response 404:** Plugin not found.

---

### POST /api/v1/plugins/{id}/enable

Enable a disabled plugin, transitioning its status to `active`.

**Auth:** Admin only.

**Response 200:** Empty body.

**Response 404:** Plugin not found.

---

### POST /api/v1/plugins/{id}/disable

Disable an active plugin. Files are retained; the plugin's tools are hidden from `ListTools`.

**Auth:** Admin only.

**Response 200:** Empty body.

**Response 404:** Plugin not found.

---

### POST /api/v1/plugins/{id}/reload

Re-read the plugin's `plugin.yaml` from disk and update the in-memory manifest. No process restart required. If the entrypoint file does not exist at the declared path, the plugin is moved to `disabled` status and the response is 500.

**Auth:** Admin only.

**Response 200:** Empty body.

**Response 404:** Plugin not found.

**Response 500:** Entrypoint file missing after reload.

---

### DELETE /api/v1/plugins/{id}

Remove an installed plugin. The plugin directory under `install_dir` is deleted and the plugin is removed from the index.

**Auth:** Admin only.

**Response 204:** Plugin removed.

**Response 404:** Plugin not found.

---

## Common Response Shapes

### Plugin Object

```json
{
  "id": "<uuid>",
  "name": "<string>",
  "version": "<semver>",
  "description": "<string>",
  "author": "<string>",
  "registry": "<registry-name>",
  "status": "active | staged | disabled | update_available | rejected | checksum_fail | downloading",
  "installed_at": "<RFC3339>",
  "manifest": {
    "name": "<string>",
    "version": "<semver>",
    "description": "<string>",
    "author": "<string>",
    "homepage": "<url>",
    "license": "<string>",
    "tags": ["<string>"],
    "min_runtime": "<semver>",
    "entrypoint": "<filename>",
    "checksums": {"sha256": "<hex>"},
    "provides": {
      "tools": [
        {
          "name": "<string>",
          "description": "<string>",
          "inputSchema": {}
        }
      ]
    },
    "permissions": {
      "network": ["<host>"],
      "env_vars": ["<var>"],
      "filesystem": false
    }
  }
}
```

### Error Response

```json
{"error": "<message>"}
```
