# Data Dictionary — plugins-and-registry-support-auto-review

**Feature:** Plugin Registry Support — Code Review Fixes
**Date:** 2026-05-07

---

## Purpose

Documents the data structures relevant to the three review fixes. No new entities are introduced — these fixes correct behaviour in existing types.

---

## Entities

### `domain.Plugin`

No schema change. Status field transitions must remain consistent after atomic update rollback.

### `domain.RegistryEntry`

| Field | Type | Relevant to |
|---|---|---|
| `LatestVersion` | string | FR-003: currently misused as the version URL segment for pinned installs |
| `Versions` | []string | FR-003: must be checked before accepting a pinned version request |
| `ManifestURL` | string | FR-003: used when version = "" (latest path) |
| `DownloadURL` | string | FR-003: used when version = "" (latest path) |

---

## Sentinel Errors

| Error | Package | Used by |
|---|---|---|
| `ErrPluginNotFound` | `domain` | Returned by store on missing ID; must map to HTTP 404 in handlers |

---

## HTTP Response Shape

For 404 responses, the JSON body must follow the existing error envelope:

```json
{"error": "plugin not found"}
```

This matches the pattern used by other not-found responses in `server.go`.

---

## Version URL Pattern

When a specific version is requested, the install use case must construct version-specific URLs. The pattern (derived from the registry protocol) is:

- Manifest: `<registry-base>/<plugin-name>/<version>/plugin.yaml`
- Archive: `<registry-base>/<plugin-name>/<version>/<plugin-name>-<version>.tar.gz`

These are constructed by the install use case, not stored pre-computed in `RegistryEntry`.
