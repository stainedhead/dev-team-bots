# Architecture — plugins-and-registry-support-auto-review

**Feature:** Plugin Registry Support — Code Review Fixes
**Date:** 2026-05-07
**Status:** Draft

---

## Architecture Overview

All three fixes are internal corrections within existing components. No new layers, packages, or interfaces are introduced. The Clean Architecture boundaries remain unchanged.

---

## Component Architecture

### FR-001 — HTTP Layer Fix

**Component:** `infrastructure/http/server.go`

The 6 plugin action handlers (`handlePluginApprove`, `handlePluginReject`, `handlePluginEnable`, `handlePluginDisable`, `handlePluginReload`, `handlePluginRemove`) currently call `writeInternalError` for all non-nil errors. The fix adds an `errors.Is(err, domain.ErrPluginNotFound)` check before the default 500 path.

```
handler receives request
  → call use case
  → if errors.Is(err, ErrPluginNotFound) → writeNotFoundError(w, "plugin not found") [NEW]
  → else if err != nil → writeInternalError(w, err)
  → else → writeSuccess(w)
```

### FR-002 — Store Atomic Update

**Component:** `infrastructure/local/plugin/store.go`

Current `Update` likely calls `installer.Extract` directly into the plugin's existing directory or replaces it without rollback protection. The fix implements the safe rename sequence:

```
1. Extract archive → <name>-update-tmp/          (Extract to fresh temp)
2. os.Rename <name>/ → <name>-old/               (Save current)
3. os.Rename <name>-update-tmp/ → <name>/         (Promote new)
4. os.RemoveAll <name>-old/                       (Clean up saved)

On failure at step 3 or later:
  os.Rename <name>-old/ → <name>/                (Restore)
```

`os.Rename` within the same filesystem is atomic on POSIX. The plugin status must be preserved (remains `active` on rollback).

### FR-003 — Install Use Case Version URL

**Component:** `application/plugin/install.go`

When `version != ""`, the install use case must not use `entry.ManifestURL`/`entry.DownloadURL` (which point to the latest version). Instead it constructs version-specific URLs and validates the version exists in `entry.Versions`.

---

## Layer Responsibilities

| Layer | Package | Change |
|---|---|---|
| Infrastructure/HTTP | `server.go` | 404 mapping for `ErrPluginNotFound` |
| Infrastructure/Local | `store.go` | Atomic update with rollback |
| Application | `install.go` | Version-pinned URL construction |
| Domain | — | No change (ErrPluginNotFound already defined) |

---

## Data Flow

### FR-001 Error Path
```
HTTP handler → ManageUseCase.{Approve/Reject/Enable/Disable/Reload/Remove}
            → LocalPluginStore.{Approve/...} → ErrPluginNotFound
            → handler: errors.Is → http.StatusNotFound
```

### FR-002 Update Path
```
ManageUseCase.Update → LocalPluginStore.Update
  → installer.Extract(tmpDir) → rename current → rename new → remove old
  → on failure: restore old
```

### FR-003 Install Path
```
InstallUseCase.Install(name, version)
  → if version != "": validate in entry.Versions, construct versioned URLs
  → if version == "": use entry.ManifestURL / entry.DownloadURL (unchanged)
  → RegistryManager.FetchManifest(versionedURL)
  → RegistryManager.FetchArchive(versionedURL)
```

---

## Architectural Decisions

- **No new interfaces:** All fixes operate within existing interface contracts. `ErrPluginNotFound` is already defined; no new error types needed.
- **Rollback by rename, not copy:** Rename is atomic on POSIX. Copying would introduce a window of inconsistency and be slower.
- **Version URL construction in application layer:** Consistent with Clean Architecture — application use cases orchestrate how domain data maps to infrastructure calls.
