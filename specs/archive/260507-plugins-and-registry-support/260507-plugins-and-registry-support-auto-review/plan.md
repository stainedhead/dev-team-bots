# Plan — plugins-and-registry-support-auto-review

**Feature:** Plugin Registry Support — Code Review Fixes
**Date:** 2026-05-07
**Status:** Planning

---

## Development Approach

TDD (Red → Green → Refactor) for every fix. Brief code review after each fix before moving to the next. P0 items first, then P1.

---

## Phase Breakdown

### Workstream A: FR-001 + FR-002 (sequential — both touch the plugin store test setup)

**A1 — FR-001: HTTP 404 for not-found (TDD)**
1. Red: Add failing tests to `server_test.go` for each of the 6 endpoints returning 404 when plugin ID is unknown.
2. Green: Add `errors.Is(err, domain.ErrPluginNotFound)` check in each handler, return 404 JSON.
3. Refactor: Extract `writeNotFoundError` helper if pattern repeats.
4. Code review A1.

**A2 — FR-002: Atomic update (TDD)**
1. Red: Add failing tests to `store_test.go` for corrupt-archive rollback and successful-update cleanup.
2. Green: Implement atomic rename sequence in `LocalPluginStore.Update`.
3. Refactor: Clean up error paths.
4. Code review A2.

### Workstream B: FR-003 (independent — application layer only)

**B1 — FR-003: Version-pinned install (TDD)**
1. Red: Add failing test to `install_test.go` verifying versioned URLs and not-available error.
2. Green: Add version validation and URL construction in `InstallUseCase.Install`.
3. Refactor: Clean up conditional logic.
4. Code review B1.

---

## Critical Path

FR-001 → FR-002 (sequential, store test setup shared)
FR-003 (parallel with A1/A2)

Merge workstreams after all three are green.

---

## Testing Strategy

- All new tests are behaviour tests, not implementation tests.
- Use httptest.NewRecorder for server_test.go.
- Use real temp dirs for store_test.go (no mocks for filesystem).
- Use mocked RegistryManager in install_test.go to assert URL arguments.

---

## Rollout Strategy

All fixes land on `feat/plugins-and-registry-support` branch. Merged as part of the main feature PR.

---

## Success Metrics

- All 3 FRs have green tests.
- `go test ./...` passes.
- Coverage ≥ 90% on domain and application layers.
- `golangci-lint run` passes.
