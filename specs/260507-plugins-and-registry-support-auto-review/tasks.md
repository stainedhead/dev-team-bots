# Tasks — plugins-and-registry-support-auto-review

**Feature:** Plugin Registry Support — Code Review Fixes
**Date:** 2026-05-07
**Status:** Planning

---

## Progress Summary

0/6 tasks complete

---

## Phase 5: Implementation Tasks

| ID | Task | Dependencies | Est (min) | Status |
|---|---|---|---|---|
| A1.1 | Red: failing server_test.go tests for 6 endpoints → 404 | — | 15 | ⬜ Pending |
| A1.2 | Green: add ErrPluginNotFound → 404 mapping in 6 handlers | A1.1 | 15 | ⬜ Pending |
| A2.1 | Red: failing store_test.go tests for atomic update (rollback + cleanup) | A1.2 | 20 | ⬜ Pending |
| A2.2 | Green: implement atomic Update with rename rollback in store.go | A2.1 | 25 | ⬜ Pending |
| B1.1 | Red: failing install_test.go test for version-pinned URL + not-available error | — | 20 | ⬜ Pending |
| B1.2 | Green: fix version-pinned URL construction in install.go | B1.1 | 20 | ⬜ Pending |

---

## Acceptance Criteria Reference

- A1 closes: AC-001 through AC-008
- A2 closes: AC-009 through AC-011
- B1 closes: AC-012 through AC-014
