# Data Dictionary: BaoBot Dev Team

**Feature:** baobot-dev-team
**Date:** 2026-05-05
**Status:** Draft

---

## Purpose

Defines all domain entities, value objects, interfaces, enumerations, and API types for the BaoBot system. Populated progressively during Phase 2 (Research & Data Modeling).

---

## Entities

### WorkItem

| Field | Type | Description |
|-------|------|-------------|
| ID | UUID | Unique identifier |
| Type | WorkItemType | AdHoc or Scheduled |
| Title | string | Short description |
| Description | string | Full context and requirements |
| Status | WorkItemStatus | Current lifecycle state |
| Priority | int | Operator-assigned priority (lower = higher priority) |
| WorkflowStep | string | Current workflow step name |
| AssignedBotID | BotID | Currently assigned bot (nil if unassigned) |
| CreatedAt | time.Time | Creation timestamp |
| UpdatedAt | time.Time | Last modification timestamp |
| FutureStartAt | *time.Time | Earliest start time (nil = immediate) |
| Attachments | []Attachment | Referenced files or context |
| EstimatedMinutes | *float64 | LLM-assessed complexity in agent minutes |
| ActualMinutes | *float64 | Observed completion time (set on completion) |
| ETAStartAt | *time.Time | Estimated start time |
| ETACompleteAt | *time.Time | Estimated completion time |
| RateLimitedMinutes | float64 | Total rate-limited pause time (excluded from ETA) |
| CostUSD | float64 | Accumulated cost for this item |

### Bot

| Field | Type | Description |
|-------|------|-------------|
| ID | BotID | Unique identifier |
| Name | string | Display name |
| Role | BotRole | Assigned role(s) |
| SoulMD | string | Path to SOUL.md file |
| Skills | []string | Assigned skill identifiers |
| ScheduledTasks | []ScheduledTask | Assigned recurring tasks |
| Status | BotStatus | Idle, Working, RateLimited, CapExceeded |
| DailyBudget | Budget | Per-bot daily token + tool-call caps |
| CurrentQueueDepth | int | Number of items waiting for this bot |

### ScheduledTask

| Field | Type | Description |
|-------|------|-------------|
| ID | UUID | Unique identifier |
| BotID | BotID | Assigned bot |
| SystemPrompt | string | Task description for the model |
| Schedule | string | Cron expression |
| FutureStartAt | *time.Time | Earliest execution time |
| LastRunAt | *time.Time | Previous execution timestamp |
| NextRunAt | time.Time | Next scheduled execution |
| Enabled | bool | Whether the task is active |

### WorkflowDefinition

| Field | Type | Description |
|-------|------|-------------|
| Name | string | Workflow identifier |
| Steps | []WorkflowStep | Ordered step definitions |
| DefaultWorkflow | bool | Whether this is the default |

### WorkflowStep

| Field | Type | Description |
|-------|------|-------------|
| Name | string | Step identifier (e.g., "backlog", "implement") |
| RequiredRole | BotRole | Role required to execute this step |
| NextStep | string | Step to advance to on completion |
| NotifyOnEntry | bool | Send notification when item enters this step |

### Budget

| Field | Type | Description |
|-------|------|-------------|
| DailyTokenCap | int64 | Maximum tokens per day (per-bot) |
| DailyToolCallCap | int | Maximum tool calls per day (per-bot) |
| DailySpendCapUSD | float64 | Maximum USD spend per day (per-bot) |
| SystemDailyCapUSD | float64 | System-wide daily spend ceiling |
| SystemMonthlyCapUSD | float64 | System-wide monthly spend ceiling |
| SpikeAlertThresholdPct | float64 | Daily spike alert threshold (default: 30%) |
| FlatCapAlertThresholdPct | float64 | Monthly flat cap alert threshold (default: 80%) |

### ETACalibration

| Field | Type | Description |
|-------|------|-------------|
| TaskType | string | Work item type for calibration |
| SeedMultiplier | float64 | Initial reduction factor (default: 0.015) |
| ObservedRatio | *float64 | actual_agent_minutes ÷ human_estimate_minutes (nil until threshold) |
| CompletedSampleCount | int | Number of completed tasks used for calibration |
| CalibrationThreshold | int | Samples needed to replace seed (default: 10) |

### ViabilityMetrics

| Field | Type | Description |
|-------|------|-------------|
| BotID | BotID | Bot identifier |
| Period | DateRange | Reporting period |
| TasksCompleted | int | Count of completed work items |
| DeliveryAccuracy | float64 | Ratio of estimated to actual completion time |
| CostPerTask | float64 | Average USD cost per completed task |
| StepCycleTimes | map[string]float64 | Average minutes per workflow step |
| RateLimitedMinutes | float64 | Total rate-limited pause time in period |

---

## Value Objects

- `WorkItemID` — UUID string
- `BotID` — string (kebab-case bot name)
- `BotRole` — string enumeration
- `DateRange` — {Start time.Time, End time.Time}
- `Attachment` — {Name string, ContentType string, StorageKey string}

---

## Enumerations

### WorkItemType
```go
type WorkItemType string
const (
    WorkItemTypeAdHoc     WorkItemType = "ad_hoc"
    WorkItemTypeScheduled WorkItemType = "scheduled"
)
```

### WorkItemStatus
```go
type WorkItemStatus string
const (
    StatusBacklog      WorkItemStatus = "backlog"
    StatusInProgress   WorkItemStatus = "in_progress"
    StatusBlocked      WorkItemStatus = "blocked"
    StatusAwaitingPO   WorkItemStatus = "awaiting_po"
    StatusComplete     WorkItemStatus = "complete"
    StatusFailed       WorkItemStatus = "failed"
)
```

### BotStatus
```go
type BotStatus string
const (
    BotStatusIdle         BotStatus = "idle"
    BotStatusWorking      BotStatus = "working"
    BotStatusRateLimited  BotStatus = "rate_limited"
    BotStatusCapExceeded  BotStatus = "cap_exceeded"
)
```

---

## Domain Interfaces

[TBD — populate during Phase 2 with full interface signatures for: WorkItemRepository, WorkflowEngine, Scheduler, CostEnforcer, ContentScreener, AuthProvider, ETAEstimator, RebalancingEngine, MetricsStore, NotificationSender]

---

## API Request/Response Types

[TBD — populate with baobotctl CLI command input/output types and web UI API request/response shapes]

---

## Database Schema

[TBD — PostgreSQL schema for work items, workflow definitions, bot state, viability metrics; DynamoDB schema for budget tracking]
