# Implementation Notes — plugins-and-registry-support-auto-review

**Feature:** Plugin Registry Support — Code Review Fixes
**Date:** 2026-05-07

---

## Purpose

Records technical decisions, edge cases, and deviations made during implementation of the three review fixes.

---

## Technical Decisions

[To be populated during implementation]

---

## Edge Cases & Solutions

### FR-001
- If a handler calls multiple store methods and the first returns `ErrPluginNotFound`, subsequent calls must be skipped. Check `errors.Is` early and return immediately.

### FR-002
- If `os.Rename(<name>-update-tmp, <name>-old)` fails (e.g., `<name>-old` already exists from a previous crashed update), clean it up first.
- If rollback itself fails (rename old back), log `slog.Error` and return the rollback error — plugin is in a potentially inconsistent state.

### FR-003
- `entry.Versions` may be nil or empty if the registry index was fetched before per-version tracking was added. Treat nil `Versions` as not having the requested version and return the not-available error.

---

## Deviations from Plan

[To be populated during implementation]

---

## Lessons Learned

[To be populated after completion]
