# PRD — Plugin Registry Support: Code Review Fixes

**Status:** Draft
**Author:** dev-flow review agent
**Feature branch:** feat/plugins-and-registry-support
**Source spec:** specs/260507-plugins-and-registry-support/

---

## Executive Summary

Code review of the plugin registry implementation identified 2 security issues (resolved in the review commit), 3 correctness/reliability warnings, and 2 informational findings. The resolved security issues are documented for traceability. The 3 warning-level findings require implementation fixes before this branch is merge-ready.

All fixes must follow TDD (Red → Green → Refactor). Each fix should be reviewed before moving to the next. Use agent teammates and git worktrees for parallel workstreams where dependencies allow.

---

## Problem

The initial plugin registry implementation has three unfixed issues that affect API correctness, update reliability, and install correctness. These need to be addressed before merge.

---

## Goals

- Fix incorrect HTTP status codes for plugin-not-found errors.
- Make plugin update atomic to prevent data loss on failed updates.
- Fix version-pinned plugin install to actually fetch the requested version.

## Non-Goals

- Re-implementing the security fixes already merged (symlink rejection, wire-size limit).
- Changing the plugin manifest schema.
- Changing the registry protocol.

---

## Resolved Security Findings (for traceability)

These were fixed in commit `fix(plugin): address code review Must Fix security findings`:

- **SEC-1 (resolved)**: `FetchArchive` had no wire-size cap; 20 MB limit enforced via `io.LimitReader`.
- **SEC-2 (resolved)**: `extractArchive` did not reject symlinks/hardlinks; now returns error for `tar.TypeSymlink` and `tar.TypeLink`.

---

## Functional Requirements

**FR-001 (P0)** — When `Approve`, `Reject`, `Enable`, `Disable`, `Reload`, or `Remove` is called with a plugin ID that does not exist, the API must return HTTP 404 (not HTTP 500).

**Acceptance criteria:**
- `POST /api/v1/plugins/{nonexistent-id}/approve` returns 404 with a JSON error body.
- `POST /api/v1/plugins/{nonexistent-id}/reject` returns 404.
- `POST /api/v1/plugins/{nonexistent-id}/enable` returns 404.
- `POST /api/v1/plugins/{nonexistent-id}/disable` returns 404.
- `POST /api/v1/plugins/{nonexistent-id}/reload` returns 404.
- `DELETE /api/v1/plugins/{nonexistent-id}` returns 404.
- Tests in `infrastructure/http/server_test.go` verify each endpoint returns 404 for a missing ID.
- `ErrPluginNotFound` is **not** logged as an internal server error.

---

**FR-002 (P0)** — Plugin `Update` must be atomic: if the new archive extraction fails for any reason, the currently installed version must remain intact and accessible.

**Acceptance criteria:**
- A test in `infrastructure/local/plugin/store_test.go` verifies: given an active plugin, when `Update` is called with a corrupt archive (bad checksum or unparseable tar.gz), the original plugin directory still exists and the plugin is still `active` (or reverts to `active` if it was in another state) — no data is lost.
- A test verifies that after a successful update, only the new version directory exists (old is cleaned up).
- The implementation uses: extract to `<name>-update-tmp`, rename current to `<name>-old`, rename new to `<name>`, remove `<name>-old`. If any step fails after the rename, the implementation restores `<name>-old` as `<name>`.

---

**FR-003 (P1)** — When a specific version is requested during plugin install (`version != ""`), the install must fetch the manifest and archive for that exact version, not the latest version's URLs.

**Acceptance criteria:**
- A test in `application/plugin/install_test.go` verifies: when `Install` is called with `version = "1.0.0"` and the index entry's `LatestVersion` is `"1.2.0"`, the `FetchManifest` and `FetchArchive` calls use URLs containing `1.0.0`, not `1.2.0`.
- The registry entry's `Versions` list is checked; if the requested version is not present, an error is returned: `"version 1.0.0 not available in registry"`.
- When `version = ""`, behaviour is unchanged (uses `entry.LatestVersion`).

---

## Non-Functional Requirements

### Process
- All fixes use TDD: write the failing test first, then implement the fix, then verify the test passes.
- After each fix is implemented, conduct a brief code review of the change before moving to the next fix.
- Prioritise P0 items (FR-001, FR-002) before P1 items (FR-003).
- FR-001 (HTTP status codes) and FR-003 (version-pinned install) are independent and may be worked in parallel git worktrees to reduce elapsed time. FR-002 (atomic update) touches the same store code as FR-001's test setup, so sequence those within one workstream.
- Use agent teammates for parallel workstreams where possible. Worker agents must use `claude-sonnet-4-6` as their model backend.

### Observability
- `ErrPluginNotFound` errors must NOT be logged as `slog.Error` or `writeInternalError` — they are user errors, not server errors.

---

## Open Questions

None. All findings have clear resolutions.

---

## Informational Findings (no fix required)

These do not block merge but should be tracked for future improvement:

- **INFO-1**: `readManifest` silently swallows JSON unmarshal errors with `_ = json.Unmarshal(...)`. A corrupt manifest returns a zero-value silently. Future: return an error and surface it.
- **INFO-2**: `loadFromDisk` skips corrupt `status.json` entries with no log line. Future: add `slog.Warn` so operators can detect corrupted plugin state at startup.
