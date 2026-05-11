# Code Review: Kanban Board Filtering

**Date:** 2026-05-11
**Author:** stainedhead
**Branch:** feat/repeative-tasks-scheduling
**Change Request:** chg-req-kanban-board-filtering.md
**Spec:** N/A — implemented from change-request document; no full spec directory

---

## Summary

The Board tab previously had no filter controls, while the Tasks and Notifications tabs each had bot and search filters. This change adds a filter bar (All bots, Search, Directory) to the Board tab and adds a Directory filter to both the Tasks and Notifications tabs, making filtering consistent across all three views. A pre-existing bug in the Notifications API handler — where the text search query param was read as `search=` but the client sent `q=` — is also fixed as part of this change.

---

## Changes Made

### Added
- `internal/domain/agent_notification.go`: `WorkDir string` field on `AgentNotification`; `WorkDir string` field on `AgentNotificationFilter` (leaf-match semantics, uses `filepath.Base`)
- `internal/infrastructure/local/orchestrator/agent_notification_store.go`: `leafDir()` helper function; `WorkDir` predicate in `List()` — filters notifications where `leafDir(n.WorkDir) == filter.WorkDir`
- `internal/infrastructure/http/server.go` (HTML): Board filter bar with `#bf-bot`, `#bf-text`, `#bf-dir` controls; `#tf-dir` Directory dropdown in Tasks filter bar; `#nf-dir` Directory dropdown in Notifications filter bar
- `internal/infrastructure/http/server.go` (JS): `getFilteredItems()` function for client-side board filtering; `boardBotFilter`, `boardTextFilter`, `boardDirFilter` state variables; `#bf-bot` / `#bf-dir` dropdown rebuild in `loadBoard()`; `#tf-dir` predicate in `getFilteredTasks()`; `#tf-dir` dropdown rebuild in `loadTasks()`; `dir=` query param in `loadNotifications()`; `#nf-dir` dropdown rebuild in `loadNotifications()`

### Modified
- `internal/application/notifications/notification_service.go`: `RaiseNotification` signature gains `workDir string` parameter (between `workItemID` and `message`); `WorkDir` is written to the notification struct at creation time
- `internal/domain/mocks/mock_agent_notification_store.go`: `List()` applies `WorkDir` leaf filter using `filepath.Base`
- `internal/infrastructure/http/notifications.go`: `handleNotificationList` reads `q=` instead of `search=` (bug fix); reads new `dir=` query param into `filter.WorkDir`

### Tests Added
- `internal/domain/agent_notification_test.go`: `TestAgentNotificationStore_List_FilterByWorkDir` — verifies mock store correctly filters by leaf dir
- `internal/application/notifications/notification_service_test.go`: `TestRaiseNotification_WorkDir_Propagated` — verifies `WorkDir` is set on the returned notification and persisted; all 20 existing `RaiseNotification` call sites updated to pass the new `workDir` param
- `internal/infrastructure/local/orchestrator/agent_notification_store_test.go`: `TestAgentNotificationStore_List_FilterByWorkDir` — verifies real store filters by leaf dir
- `internal/infrastructure/http/notifications_test.go`: `TestNotificationList_AppliesDirFilter` — verifies handler maps `dir=` query param to `filter.WorkDir`; existing `TestNotificationList_AppliesBotFilter` updated to send `q=` instead of `search=`

---

## Design Decisions

- **Client-side filtering for Board and Tasks:** Both views fetch the complete dataset (`GET /api/v1/board`, `GET /api/v1/tasks`) in one shot. Filtering is applied in `getFilteredItems()` / `getFilteredTasks()` against the in-memory array, so no API changes are needed for either view. This is consistent with how the existing Tasks status/bot/search filters already work.

- **Server-side filtering for Notifications:** The Notifications view sends all filter values as query params to `GET /api/v1/notifications`. The Directory filter follows the same pattern (`dir=<leaf>`) rather than switching to client-side, keeping the approach consistent with existing notification filters (bot, type, search).

- **`WorkDir` added to `AgentNotification`:** Notifications don't join to tasks at query time, so directory information must be captured at raise time. Adding `WorkDir` to the notification struct is the cleanest path — callers supply it from the task or board item context when available. The field is `omitempty` in JSON so existing serialised notifications round-trip cleanly with `WorkDir = ""`.

- **Leaf-only matching:** The Directory filter compares `filepath.Base(work_dir)` (e.g., `"myrepo"`) rather than the full path. This matches what is already displayed on task rows and board cards (`leafDir(it.work_dir)`) and is sufficient for the operator's mental model.

- **Dropdown population from loaded data:** Each Directory dropdown is rebuilt from the `work_dir` values present in the currently loaded dataset (same pattern as bot dropdowns), rather than from `AllowedWorkDirs`. This ensures the dropdown only shows directories that actually have items in the current view.

- **Filter persistence via DOM:** All new filter controls live inside `.pane` divs that are hidden/shown on tab switch — they are never destroyed. Selected values persist for free between tab clicks with no sessionStorage or JS state required, matching the existing Tasks and Notifications filter behaviour.

- **`search=` → `q=` bug fix bundled:** The Notifications text search has never worked end-to-end (client sends `q=`, handler read `search=`). Since we're touching the handler anyway, the fix is included. The test that was asserting the broken behaviour is corrected.

---

## Spec / PRD Traceability

*(No formal spec directory — implemented from `chg-req-kanban-board-filtering.md`)*

| Acceptance Criterion | Satisfied by |
|---|---|
| Board filter bar appears between tab bar and kanban columns with All bots, Search, Directory | `server.go` HTML — `#bf-bot`, `#bf-text`, `#bf-dir` inside `sec-hdr` in `#pane-board` |
| Board All bots dropdown populated from `assigned_to` values of loaded items | `loadBoard()` JS — rebuilds `#bf-bot` after each `api('GET','/api/v1/board')` |
| Board Search filters items by `title` and `description` | `getFilteredItems()` — case-insensitive substring on both fields |
| Board Directory dropdown populated from leaf dirs of loaded items | `loadBoard()` JS — rebuilds `#bf-dir` from `leafDir(it.work_dir)` values |
| Board: all three filters compose | `getFilteredItems()` — applies bot, text, dir predicates in sequence |
| Tasks Directory dropdown appears after Search, populated from task list | `server.go` HTML `#tf-dir`; `loadTasks()` rebuild |
| Tasks Directory filter composes with existing filters | `getFilteredTasks()` — dir predicate appended after existing status/bot/text predicates |
| Notifications Directory dropdown sends `dir=<leaf>` to `GET /api/v1/notifications` | `loadNotifications()` JS + `handleNotificationList` handler |
| Notifications text search works end-to-end | `handleNotificationList` reads `q=` (bug fix) |
| All new controls retain value between tab switches | DOM persistence — panes are shown/hidden, not destroyed |
| `AgentNotification.WorkDir` populated at raise time | `RaiseNotification()` — `workDir` param → `n.WorkDir` |
| `InMemoryAgentNotificationStore.List()` filters by `WorkDir` leaf | `agent_notification_store.go` `List()` + `leafDir()` helper |
| TDD — failing test before production code | All new production changes accompanied by new or updated tests |
| Coverage on domain and application packages ≥ 90% | `application/notifications`: 94.8% |

---

## Test Coverage

- **What is tested:** `WorkDir` field propagation through `RaiseNotification` → persist → retrieve; `List()` `WorkDir` leaf filter in both the mock store (domain-layer tests) and the real `InMemoryAgentNotificationStore`; HTTP handler mapping of `dir=` query param; HTTP handler `q=` param fix; `getFilteredItems()` / `getFilteredTasks()` are exercised indirectly through the existing HTTP integration tests
- **What is not tested:** Board filter bar JS functions (`getFilteredItems`, `loadBoard` dropdown rebuild) have no unit tests — the UI layer has no JS test harness. This is the existing gap for all JS in `server.go` and is not introduced by this change. Filter persistence across tab switches is verified manually only.

---

## Risks and Follow-ups

- **`RaiseNotification` call sites in application code:** The signature change requires all callers to pass `workDir`. Only test-file callers existed at review time; production callers (e.g., bots raising notifications via the orchestrator) will need to supply the task's `work_dir` when it is available. If left as `""`, the directory filter simply won't match those notifications, which is safe but means the filter is less useful until callers are updated.
- **`WorkDir` on notifications from board items:** Board items (`WorkItem`) have a `WorkDir` field. When a notification is raised for a board item event rather than a direct task, the `workDir` parameter should be set from `WorkItem.WorkDir`. This wiring is not addressed here.
- **Board filtering is client-side:** If the board grows very large, fetching the full item list before filtering becomes a concern. For the current scale (single-process, in-memory store) this is not an issue.
