# PRD: Task Scheduling and Notifications

**Created:** 2026-05-11
**Jira:** N/A
**Status:** Draft

## Problem Statement

Operators can currently only schedule a task to run once — either immediately or at a single future time. There is no way to express recurring work (daily standups, weekly reviews, periodic maintenance), which forces users to re-enter the same task repeatedly. The Orchestrator has no awareness of recurrence and cannot proactively queue upcoming runs. Additionally, when an agent encounters a blocker or decision point that requires human input, there is no structured channel to surface that to the user — work stalls silently. This PRD addresses both gaps: giving users expressive task scheduling and giving agents a durable, actionable notification channel back to the human.

## Goals

- Reduce user friction by eliminating manual re-entry of repetitive tasks
- Enable more fully autonomous agent operation by allowing lower-value, trusted, repeating work to be delegated and self-managing
- Allow users to focus on high-value decisions by surfacing agent blockers as structured, actionable notifications

## Non-Goals

- External calendar sync (Google Calendar, Outlook, etc.)
- Agent-initiated recurring task creation without human involvement (agents may request via Chat, but a human must approve)
- Cross-bot recurring task coordination beyond the subagent thread model described here

## Functional Requirements

**FR-001:** The Add Task dialog presents three scheduling modes: **ASAP**, **Future**, and **Recurring**.

**FR-002:** ASAP mode queues the task for the next available execution slot with no date/time input required.

**FR-003:** Future mode accepts a date and time; the task runs at the next available slot on or after that time.

**FR-004:** Recurring mode presents a visual recurrence builder with frequency options (Daily, Weekly, Monthly) and day-level inclusion/exclusion (e.g. Monday and Wednesday only).

**FR-005:** Recurring mode also accepts a plain-text natural language input as an alternative to the visual builder (e.g. "every Monday and Wednesday at 9am"); the Orchestrator parses this into a structured recurrence rule and presents a confirmation preview before saving.

**FR-006:** Terminology is consistent across all UI surfaces and the codebase: **ASAP**, **Future**, **Recurring** — not "immediate", "scheduled", "repeating", or other variants.

**FR-007:** The Task detail screen reflects the scheduling mode. For Future and Recurring tasks, the displayed date is the **next scheduled run time**, not a static creation or last-run date.

**FR-008:** On save or edit of any task with a Future or Recurring schedule, the Orchestrator immediately recalculates and persists the next scheduled run time.

**FR-009:** The Orchestrator runs a scheduling loop on a cadence that ensures no scheduled run is delayed more than **30 seconds** beyond its scheduled time.

**FR-010:** Agents can raise a notification to the user, attaching a message, a context summary, and a reference to the originating task or work item.

**FR-011:** A **Notifications** tab is added to the main navigation bar with a badge showing the count of unread/unactioned notifications.

**FR-012:** The Notifications list screen mirrors the Tasks list layout and includes: All / Single / Recurring filters, a bot filter, a search field, Refresh, and Delete Selected.

**FR-013:** Clicking a notification opens a detail screen showing the notification message, agent output context, and a **Discuss** panel (in place of "Ask") where the user can respond to the agent.

**FR-014:** Responses entered in Discuss are appended to the originating task's context; the task becomes eligible for re-queuing so the agent can continue with the enriched information.

**FR-015:** The Orchestrator bot can create, edit, and manage tasks when instructed via natural language in Chat (e.g. "schedule a weekly code review every Monday at 9am"). The Orchestrator must request explicit human confirmation before saving any task creation or modification.

**FR-016:** Bot-to-bot collaboration is supported via a subagent thread: the requesting bot shares relevant task context with the assisting bot, and the assisting bot's response is returned into the requester's active task context.

## Non-Functional Requirements

- **Performance:** The Orchestrator scheduling loop must fire within **30 seconds** of any task's scheduled time.
- **Reliability:** Notifications are durably persisted in the system store and are not lost if the UI is offline at the time of delivery.
- **Security:** Only users with the **human** or **orchestrator** role may create or modify tasks of any type. The orchestrator bot requires explicit human approval before any task creation or edit takes effect. Attempts by other bots to create tasks directly are rejected.
- **Observability:** Scheduling loop execution, missed-run detection, and notification delivery events must be logged and visible in Orchestrator telemetry.
- **Consistency:** The terms ASAP, Future, and Recurring must be used consistently in UI labels, API field names, database column names, and log messages.

## Acceptance Criteria

- [ ] A task can be created with ASAP, Future, or Recurring scheduling from the Add Task dialog
- [ ] A recurring task configured for Monday and Wednesday runs on those days and not on others
- [ ] The Task detail screen shows "Next run: \<datetime\>" for Future and Recurring tasks
- [ ] Editing a recurrence rule immediately recalculates and updates the next scheduled run time
- [ ] The Orchestrator scheduling loop fires within 30 seconds of a task's scheduled time
- [ ] A notification raised by an agent is persisted and visible in the Notifications tab after a UI reconnect
- [ ] The Notifications tab badge increments when a new notification arrives and clears when actioned
- [ ] A Discuss response on a notification is appended to the originating task's context and the task can be re-queued
- [ ] A user can instruct the Orchestrator via Chat to create a recurring task; the Orchestrator requests confirmation before saving
- [ ] Only users with the human or orchestrator role can create tasks; attempts by other bots are rejected
- [ ] If the Orchestrator restarts and one or more scheduled runs were missed, those tasks execute immediately on restart (collapsing multiple missed occurrences of the same task into a single catch-up run)

## Dependencies and Risks

| Item | Type | Notes |
|------|------|-------|
| Orchestrator scheduling loop | Dependency | New infrastructure required to tick on a sub-minute cadence |
| Notification persistence | Dependency | New `notifications` table (or equivalent store) required in the DB layer |
| Bot-to-bot subagent threading | Dependency | Requires the existing task/worker context model to support shared context injection |
| Natural language recurrence parsing (FR-005) | Risk | Ambiguous input could produce incorrect schedules; confirmation preview mitigates but does not eliminate |
| Orchestrator task approval flow (FR-015) | Risk | Confirmation UX in Chat is new interaction territory; UX complexity may require iteration |
| Missed run behaviour on restart | Risk | On restart, missed runs execute immediately (catch-up); multiple missed occurrences of the same recurring task collapse into a single catch-up run |

## Open Questions

None.
