# Spec — Plugin Registry Support: Code Review Fixes

**Feature:** plugins-and-registry-support-auto-review
**Created:** 2026-05-07
**Status:** In Progress
**Source PRD:** [plugins-and-registry-support-auto-review-PRD.md](plugins-and-registry-support-auto-review-PRD.md)

---

## Executive Summary

Three code review findings from the initial plugin registry implementation require fixes before the branch is merge-ready: incorrect HTTP status codes for plugin-not-found errors (FR-001), non-atomic plugin update that risks data loss on failure (FR-002), and version-pinned install that ignores the requested version (FR-003). All fixes follow TDD and Clean Architecture.

---

## Problem Statement

The initial plugin registry implementation was reviewed and produced 2 resolved security findings and 3 warning-level correctness/reliability findings. The resolved security issues (wire-size cap, symlink rejection) are documented for traceability. The 3 remaining findings must be fixed before merge.

**Affected systems:** `boabot` agent runtime — plugin lifecycle management HTTP endpoints, plugin store, and install use case.

---

## Goals

- Fix incorrect HTTP status codes: plugin-not-found → 404 (not 500).
- Make plugin `Update` atomic: failed extractions must not corrupt existing plugin.
- Fix version-pinned install: requested version must be fetched, not `LatestVersion`.

## Non-Goals

- Re-implementing the security fixes already merged (SEC-1, SEC-2).
- Changing the plugin manifest schema.
- Changing the registry protocol.
- Addressing INFO-level findings (INFO-1, INFO-2) in this sprint.

---

## Resolved Security Findings (traceability)

Fixed in commit `fix(plugin): address code review Must Fix security findings`:

- **SEC-1 (resolved):** `FetchArchive` had no wire-size cap; 20 MB limit enforced via `io.LimitReader`.
- **SEC-2 (resolved):** `extractArchive` did not reject symlinks/hardlinks; now returns error for `tar.TypeSymlink` and `tar.TypeLink`.

---

## User Requirements

### FR-001 (P0) — HTTP 404 for Plugin Not Found

When `Approve`, `Reject`, `Enable`, `Disable`, `Reload`, or `Remove` is called with a plugin ID that does not exist, the API must return HTTP 404, not HTTP 500.

**Acceptance Criteria:**

- AC-001: `POST /api/v1/plugins/{nonexistent-id}/approve` returns 404 with JSON error body.
- AC-002: `POST /api/v1/plugins/{nonexistent-id}/reject` returns 404 with JSON error body.
- AC-003: `POST /api/v1/plugins/{nonexistent-id}/enable` returns 404 with JSON error body.
- AC-004: `POST /api/v1/plugins/{nonexistent-id}/disable` returns 404 with JSON error body.
- AC-005: `POST /api/v1/plugins/{nonexistent-id}/reload` returns 404 with JSON error body.
- AC-006: `DELETE /api/v1/plugins/{nonexistent-id}` returns 404 with JSON error body.
- AC-007: Tests in `infrastructure/http/server_test.go` verify each endpoint returns 404 for a missing ID.
- AC-008: `ErrPluginNotFound` is NOT logged as `slog.Error` or `writeInternalError`.

---

### FR-002 (P0) — Atomic Plugin Update

Plugin `Update` must be atomic: if the new archive extraction fails for any reason, the currently installed version must remain intact and accessible.

**Acceptance Criteria:**

- AC-009: A test in `infrastructure/local/plugin/store_test.go` verifies: given an active plugin, when `Update` is called with a corrupt archive, the original plugin directory still exists and the plugin is still `active` — no data is lost.
- AC-010: A test verifies that after a successful update, only the new version directory exists (old is cleaned up).
- AC-011: Implementation uses: extract to `<name>-update-tmp`, rename current to `<name>-old`, rename new to `<name>`, remove `<name>-old`. If any step fails after the rename, implementation restores `<name>-old` as `<name>`.

---

### FR-003 (P1) — Version-Pinned Install

When a specific version is requested during install (`version != ""`), the install must fetch the manifest and archive for that exact version, not `LatestVersion`.

**Acceptance Criteria:**

- AC-012: A test in `application/plugin/install_test.go` verifies: when `Install` is called with `version = "1.0.0"` and `entry.LatestVersion = "1.2.0"`, `FetchManifest` and `FetchArchive` calls use URLs containing `"1.0.0"`, not `"1.2.0"`.
- AC-013: If the requested version is not present in `entry.Versions`, an error is returned: `"version 1.0.0 not available in registry"`.
- AC-014: When `version = ""`, behaviour is unchanged (uses `entry.LatestVersion`).

---

## Non-Functional Requirements

### Process
- All fixes use TDD: write failing test first, implement, verify passing.
- Brief code review after each fix before moving to the next.
- P0 items (FR-001, FR-002) before P1 items (FR-003).
- FR-001 and FR-003 are independent — may be worked in parallel git worktrees.
- FR-002 touches the same store code as FR-001's test setup — sequence within one workstream.

### Observability
- `ErrPluginNotFound` must NOT be logged as `slog.Error` — it is a user error, not a server error.

---

## System Architecture

### Affected Layers

| Layer | Component | Change |
|---|---|---|
| Infrastructure/HTTP | `server.go` | Map `ErrPluginNotFound` → 404 in 6 handlers |
| Infrastructure/Local | `store.go` | Implement atomic `Update` with rollback |
| Application | `install.go` | Fix version-pinned URL construction |
| Tests | `server_test.go`, `store_test.go`, `install_test.go` | New test cases for each FR |

### Files to Modify

- `boabot/internal/infrastructure/http/server.go` — add 404 mapping for `ErrPluginNotFound`
- `boabot/internal/infrastructure/local/plugin/store.go` — atomic update implementation
- `boabot/internal/application/plugin/install.go` — version-pinned URL fix
- `boabot/internal/infrastructure/http/server_test.go` — tests for FR-001
- `boabot/internal/infrastructure/local/plugin/store_test.go` — tests for FR-002
- `boabot/internal/application/plugin/install_test.go` — tests for FR-003

### Files to Create

None.

---

## Breaking Changes

None. All changes are internal behaviour fixes that make the API more correct (404 vs 500 is a corrective change, not a breaking change).

---

## Risks and Mitigation

| Risk | Mitigation |
|---|---|
| Atomic update rollback logic introduces new failure modes | Cover rollback path explicitly in tests |
| 404 mapping misses a handler | Test every endpoint individually in server_test.go |
| Version URL construction varies by registry format | Mock FetchManifest/FetchArchive in install_test.go and assert URL content |

---

## References

- Source PRD: [plugins-and-registry-support-auto-review-PRD.md](plugins-and-registry-support-auto-review-PRD.md)
- Original spec (archived): `specs/archive/260507-plugins-and-registry-support/`
