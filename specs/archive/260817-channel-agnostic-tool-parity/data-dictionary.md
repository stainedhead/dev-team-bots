# Data Dictionary: Channel-Agnostic Agent Tool Parity

**Created:** 2026-08-17

## Purpose

No new data structures. This feature adds MCP tool wrappers around existing `domain.WorkItem` (board) and `domain.DirectTask` (task) reads, and (FR-603, stretch) existing team-registry data. Tool response shape: minimal, chat-readable summaries (title/status/assignee/ID for board items; status/schedule for tasks) — not full object dumps.
