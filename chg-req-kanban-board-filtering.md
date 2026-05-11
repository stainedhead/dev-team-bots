# Change Request: Kanban Board Filtering

**Created:** 2026-05-11
**Status:** Draft

---

## Summary

The Board tab has no filter controls. The Tasks and Notifications tabs have bot and search filters but no Directory filter. This change adds a consistent filter bar to the Board tab and adds a Directory filter to the Tasks and Notifications tabs. All new filters persist between tab switches, matching the existing behaviour of the other filter controls.

---

## Current State

### Board tab
- No filter bar. All items returned by `GET /api/v1/board` are rendered directly into the kanban columns.
- `renderBoard()` iterates the full `allItems` array with no predicate.
- Cards already show `leafDir(work_dir)` as a folder badge on each card.

### Tasks tab
| Filter | Control | How it works |
|--------|---------|--------------|
| Status | All / Immediate / Scheduled buttons | Client-side on `allTasksList` via `getFilteredTasks()` |
| Bot | `#tf-bot` select (built from task list) | Client-side |
| Search | `#tf-text` input (title + instruction) | Client-side |
| **Directory** | **missing** | — |

### Notifications tab
| Filter | Control | How it works |
|--------|---------|--------------|
| Type | All / Single / Recurring buttons | Server-side query param `type=` |
| Bot | `#nf-bot` select | Server-side query param `bot=` |
| Search | `#nf-text` input | Server-side query param `q=` (note: handler reads `search=` — existing bug, fix in this change) |
| **Directory** | **missing** | — |

### `AgentNotification` domain type
`AgentNotification` has no `work_dir` field today. It has `TaskID` and `WorkItemID` but does not capture the working directory at creation time. A directory filter requires adding this field.

---

## Proposed Changes

### 1. Board tab — new filter bar

Add a filter row between the tab bar and the kanban columns (inside `#pane-board`, above `#board`).

**Controls:**
- **All bots** dropdown (`#bf-bot`) — values built from unique `assigned_to` values across `allItems`; rebuilt on each `loadBoard()` call
- **Search** text input (`#bf-text`) — filters `title` and `description` fields of `WorkItem`
- **Directory** dropdown (`#bf-dir`) — values built from unique `leafDir(work_dir)` values across `allItems`; `"" → "All dirs"`

**Filtering approach:** client-side. Introduce `getFilteredItems()` that applies all three predicates to `allItems` and returns a filtered slice. `renderBoard()` calls `getFilteredItems()` instead of iterating `allItems` directly.

**Persistence:** DOM elements inside `#pane-board` are hidden/shown on tab switch — they are never destroyed. Values persist for free, matching how Tasks and Notifications filters work. No additional JS state variables are needed beyond what is read from the DOM elements directly.

**JS changes in `server.go`:**
- Add `boardBotFilter`, `boardTextFilter`, `boardDirFilter` state vars (initialized `''`) for any code that needs to reset them programmatically (e.g., logout)
- Add `getFilteredItems()` function
- Update `renderBoard()` to call `getFilteredItems()`
- Update `loadBoard()` to rebuild the `#bf-bot` and `#bf-dir` option lists from `allItems` after each fetch (same pattern as `loadTasks()` rebuilding `#tf-bot`)

**No Go backend changes required** for the Board filter.

---

### 2. Tasks tab — add Directory filter

Add a **Directory** dropdown (`#tf-dir`) to the existing filter bar, after the `#tf-text` search input.

- Values: unique `leafDir(work_dir)` values from `allTasksList`; `"" → "All dirs"`
- Rebuilt on each `loadTasks()` call alongside the bot dropdown
- Predicate added to `getFilteredTasks()`:
  ```
  var dir = (ge('tf-dir') || {}).value || '';
  if (dir) list = list.filter(function(t) { return leafDir(t.work_dir) === dir; });
  ```

**No Go backend changes required** for the Tasks directory filter.

---

### 3. Notifications tab — add Directory filter

Add a **Directory** dropdown (`#nf-dir`) to the existing filter bar, after the `#nf-text` search input.

Because notifications are fetched with server-side filtering (all existing notification filters are query params), the Directory filter follows the same pattern:

- Client sends `dir=<leafDir>` as a query param on `GET /api/v1/notifications`
- The option list is built from unique `leafDir(work_dir)` values in the currently loaded `allNotifList` (same pattern as `#nf-bot`)
- Server filters by comparing `leafDir(n.WorkDir)` against the `dir` param

This requires the following Go backend changes:

#### 3a. Domain: add `WorkDir` to `AgentNotification`
```go
// agent_notification.go
type AgentNotification struct {
    ...
    WorkDir string `json:"work_dir,omitempty"` // populated at raise time
}

type AgentNotificationFilter struct {
    BotName string
    Status  AgentNotificationStatus
    Search  string
    WorkDir string // leaf match: filter where leafDir(n.WorkDir) == WorkDir
}
```

#### 3b. Application: populate `WorkDir` in `RaiseNotification`
`NotificationService.RaiseNotification` accepts the notification payload. The call sites that build the `AgentNotification` struct must supply `WorkDir` from the task or board item context where it is available. Specifically, `RaiseNotification` should accept a `workDir string` parameter (or the caller sets the field before passing).

#### 3c. Infrastructure: filter in `InMemoryAgentNotificationStore.List()`
When `filter.WorkDir != ""`, skip entries where `leafDir(n.WorkDir) != filter.WorkDir`.

Add `leafDir` helper to the store package (matches JS implementation: take last `/`-delimited segment).

#### 3d. HTTP handler: read `dir` query param
```go
// notifications.go — handleNotificationList
filter := domain.AgentNotificationFilter{
    BotName: r.URL.Query().Get("bot"),
    Status:  domain.AgentNotificationStatus(r.URL.Query().Get("status")),
    Search:  r.URL.Query().Get("q"),    // fix: was "search", client sends "q"
    WorkDir: r.URL.Query().Get("dir"),
}
```

**Bug fix bundled in this change:** the existing `#nf-text` search passes `q=` in the query string but `handleNotificationList` reads `r.URL.Query().Get("search")` — the search param has never worked. Fix the handler to read `q=` instead.

---

## Filter Persistence

All three views use the same persistence model: the filter HTML elements live inside a `.pane` div that is toggled `display:block/none` on tab switch. Element values (select option, input text) survive tab switches without any explicit save/restore logic. This is how the Tasks and Notifications filters already behave and the Board filters will inherit it automatically.

---

## Affected Components

| Layer | File | Change |
|-------|------|--------|
| Domain | `internal/domain/agent_notification.go` | Add `WorkDir` to `AgentNotification`; add `WorkDir` to `AgentNotificationFilter` |
| Application | `internal/application/notifications/notification_service.go` | Accept/pass `WorkDir` through `RaiseNotification` |
| Infrastructure — store | `internal/infrastructure/local/orchestrator/notification_store.go` | Filter by `WorkDir` leaf in `List()` |
| Infrastructure — HTTP handler | `internal/infrastructure/http/notifications.go` | Read `dir=` param; fix `search` → `q` param name |
| Infrastructure — HTTP UI | `internal/infrastructure/http/server.go` | Board filter bar HTML + JS; Tasks `#tf-dir` HTML + `getFilteredTasks()` update; Notifications `#nf-dir` HTML + `loadNotifications()` update |
| Tests | `internal/domain/agent_notification_test.go` | Tests for `WorkDir` filtering |
| Tests | `internal/application/notifications/notification_service_test.go` | Tests for `WorkDir` propagation |
| Tests | `internal/infrastructure/local/orchestrator/notification_store_test.go` | Tests for `WorkDir` filter |

---

## Out of Scope

- Server-side filtering for the Board or Tasks views — both already fetch the full list; client-side filtering is sufficient
- Persistent filter state across page reloads (browser localStorage/sessionStorage)
- Hierarchical directory filtering (parent path matching) — leaf match is sufficient
- Adding Directory filter to the Chat tab or any other view
- Changing how `AllowedWorkDirs` are used for validation

---

## Acceptance Criteria

- [ ] Board tab: filter bar appears between the tab bar and kanban columns and contains All bots, Search, and Directory controls
- [ ] Board: All bots dropdown is populated from the `assigned_to` values of loaded items and is rebuilt after each `loadBoard()` call
- [ ] Board: Search filters items by `title` and `description` (case-insensitive substring)
- [ ] Board: Directory dropdown is populated from the unique leaf directories of loaded items and is rebuilt after each `loadBoard()` call
- [ ] Board: all three filters compose (applying bot + search + directory simultaneously narrows correctly)
- [ ] Tasks: Directory dropdown appears after the Search input, populated from leaf directories of the current task list, rebuilt on each `loadTasks()` call
- [ ] Tasks: Directory filter composes with the existing status, bot, and search filters
- [ ] Notifications: Directory dropdown appears after the Search input; selecting a directory sends `dir=<leaf>` as a query param to `GET /api/v1/notifications`
- [ ] Notifications: text search filter works end-to-end (fix the `q` vs `search` mismatch)
- [ ] All new filter controls retain their selected value when switching between tabs and returning
- [ ] `AgentNotification.WorkDir` is populated when a notification is raised from a task or board item context that has a work directory
- [ ] `InMemoryAgentNotificationStore.List()` returns only notifications matching `filter.WorkDir` when the field is non-empty
- [ ] All new Go code follows TDD (failing test first); coverage on domain and application packages remains ≥ 90%
