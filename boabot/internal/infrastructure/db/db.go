// Package db provides PostgreSQL-backed repository adapters for the boabot
// domain. All SQL interactions go through the DB interface so tests can
// substitute sqlmock without a live server.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/metrics"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"
)

// ErrConflict is returned by Update when the optimistic version check fails.
var ErrConflict = errors.New("db: version conflict")

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("db: not found")

// DB is the subset of *sql.DB used by the repositories. It allows tests to
// inject sqlmock without touching a real database.
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// ---------------------------------------------------------------------------
// Schema migration
// ---------------------------------------------------------------------------

// Migrate creates all required tables if they do not already exist. It should
// be called once at application startup.
func Migrate(ctx context.Context, db DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS work_items (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT,
			status TEXT NOT NULL DEFAULT 'backlog',
			priority INTEGER NOT NULL DEFAULT 100,
			workflow_step TEXT,
			assigned_bot_id TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			future_start_at TIMESTAMPTZ,
			estimated_minutes DOUBLE PRECISION,
			actual_minutes DOUBLE PRECISION,
			eta_start_at TIMESTAMPTZ,
			eta_complete_at TIMESTAMPTZ,
			rate_limited_minutes DOUBLE PRECISION NOT NULL DEFAULT 0,
			cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
			heartbeat_at TIMESTAMPTZ,
			version INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS workflow_definitions (
			name TEXT PRIMARY KEY,
			definition_json TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS metric_events (
			id TEXT PRIMARY KEY,
			event_type TEXT NOT NULL,
			bot_id TEXT,
			item_id TEXT,
			step_name TEXT,
			duration_minutes DOUBLE PRECISION,
			cost_usd DOUBLE PRECISION,
			timestamp TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			username TEXT PRIMARY KEY,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'viewer',
			disabled BOOLEAN NOT NULL DEFAULT false,
			must_change_password BOOLEAN NOT NULL DEFAULT false,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db: migrate: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// WorkItem
// ---------------------------------------------------------------------------

// WorkItem is the DB representation of a board work item.
type WorkItem struct {
	ID                 string
	Type               string
	Title              string
	Description        string
	Status             domain.WorkItemStatus
	Priority           int
	WorkflowStep       string
	AssignedBotID      string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	FutureStartAt      *time.Time
	EstimatedMinutes   *float64
	ActualMinutes      *float64
	ETAStartAt         *time.Time
	ETACompleteAt      *time.Time
	RateLimitedMinutes float64
	CostUSD            float64
	HeartbeatAt        *time.Time
	Version            int
}

// WorkItemRepository defines the data access contract for work items.
type WorkItemRepository interface {
	CreateWorkItem(ctx context.Context, item WorkItem) error
	GetWorkItem(ctx context.Context, id string) (WorkItem, error)
	UpdateWorkItem(ctx context.Context, item WorkItem) error
	ListByStatus(ctx context.Context, status domain.WorkItemStatus) ([]WorkItem, error)
	ListByBot(ctx context.Context, botID string) ([]WorkItem, error)
	ListStalled(ctx context.Context, heartbeatCutoff time.Time) ([]WorkItem, error)
	UpdateHeartbeat(ctx context.Context, id string) error
	DeleteWorkItem(ctx context.Context, id string) error
}

const workItemColumns = `id, type, title, description, status, priority,
	workflow_step, assigned_bot_id,
	created_at, updated_at, future_start_at,
	estimated_minutes, actual_minutes,
	eta_start_at, eta_complete_at,
	rate_limited_minutes, cost_usd, heartbeat_at, version`

// WorkItemRepo provides work-item storage backed by a DB.
type WorkItemRepo struct {
	db DB
}

// NewWorkItemRepo creates a WorkItemRepo using db.
func NewWorkItemRepo(db DB) *WorkItemRepo {
	return &WorkItemRepo{db: db}
}

func scanWorkItem(row interface {
	Scan(dest ...any) error
}) (WorkItem, error) {
	var item WorkItem
	err := row.Scan(
		&item.ID, &item.Type, &item.Title, &item.Description,
		&item.Status, &item.Priority,
		&item.WorkflowStep, &item.AssignedBotID,
		&item.CreatedAt, &item.UpdatedAt, &item.FutureStartAt,
		&item.EstimatedMinutes, &item.ActualMinutes,
		&item.ETAStartAt, &item.ETACompleteAt,
		&item.RateLimitedMinutes, &item.CostUSD, &item.HeartbeatAt,
		&item.Version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkItem{}, ErrNotFound
	}
	return item, err
}

// CreateWorkItem inserts a new work item row.
func (r *WorkItemRepo) CreateWorkItem(ctx context.Context, item WorkItem) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO work_items (
			id, type, title, description, status, priority,
			workflow_step, assigned_bot_id,
			created_at, updated_at, future_start_at,
			estimated_minutes, actual_minutes,
			eta_start_at, eta_complete_at,
			rate_limited_minutes, cost_usd, heartbeat_at, version
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19
		)`,
		item.ID, item.Type, item.Title, item.Description,
		item.Status, item.Priority,
		item.WorkflowStep, item.AssignedBotID,
		item.CreatedAt, item.UpdatedAt, item.FutureStartAt,
		item.EstimatedMinutes, item.ActualMinutes,
		item.ETAStartAt, item.ETACompleteAt,
		item.RateLimitedMinutes, item.CostUSD, item.HeartbeatAt,
		item.Version,
	)
	if err != nil {
		return fmt.Errorf("db: create work item: %w", err)
	}
	return nil
}

// GetWorkItem retrieves a single work item by ID.
func (r *WorkItemRepo) GetWorkItem(ctx context.Context, id string) (WorkItem, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+workItemColumns+` FROM work_items WHERE id = $1`, id)
	item, err := scanWorkItem(row)
	if err != nil {
		return WorkItem{}, fmt.Errorf("db: get work item %q: %w", id, err)
	}
	return item, nil
}

// UpdateWorkItem replaces a work item using optimistic locking on version.
// Returns ErrConflict if the version does not match.
func (r *WorkItemRepo) UpdateWorkItem(ctx context.Context, item WorkItem) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE work_items SET
			type=$1, title=$2, description=$3, status=$4, priority=$5,
			workflow_step=$6, assigned_bot_id=$7,
			updated_at=$8, future_start_at=$9,
			estimated_minutes=$10, actual_minutes=$11,
			eta_start_at=$12, eta_complete_at=$13,
			rate_limited_minutes=$14, cost_usd=$15, heartbeat_at=$16,
			version=version+1
		WHERE id=$17 AND version=$18`,
		item.Type, item.Title, item.Description, item.Status, item.Priority,
		item.WorkflowStep, item.AssignedBotID,
		item.UpdatedAt, item.FutureStartAt,
		item.EstimatedMinutes, item.ActualMinutes,
		item.ETAStartAt, item.ETACompleteAt,
		item.RateLimitedMinutes, item.CostUSD, item.HeartbeatAt,
		item.ID, item.Version,
	)
	if err != nil {
		return fmt.Errorf("db: update work item: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("db: update work item rows affected: %w", err)
	}
	if n == 0 {
		return ErrConflict
	}
	return nil
}

// ListByStatus returns all work items with the given status.
func (r *WorkItemRepo) ListByStatus(ctx context.Context, status domain.WorkItemStatus) ([]WorkItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+workItemColumns+` FROM work_items WHERE status = $1 ORDER BY priority`, status)
	if err != nil {
		return nil, fmt.Errorf("db: list by status: %w", err)
	}
	defer rows.Close()
	return scanWorkItems(rows)
}

// ListByBot returns all work items assigned to the given bot ID.
func (r *WorkItemRepo) ListByBot(ctx context.Context, botID string) ([]WorkItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+workItemColumns+` FROM work_items WHERE assigned_bot_id = $1 ORDER BY priority`, botID)
	if err != nil {
		return nil, fmt.Errorf("db: list by bot: %w", err)
	}
	defer rows.Close()
	return scanWorkItems(rows)
}

// ListStalled returns in_progress work items whose heartbeat_at is before
// heartbeatCutoff.
func (r *WorkItemRepo) ListStalled(ctx context.Context, heartbeatCutoff time.Time) ([]WorkItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+workItemColumns+` FROM work_items
		 WHERE status = 'in-progress' AND (heartbeat_at IS NULL OR heartbeat_at < $1)
		 ORDER BY updated_at`, heartbeatCutoff)
	if err != nil {
		return nil, fmt.Errorf("db: list stalled: %w", err)
	}
	defer rows.Close()
	return scanWorkItems(rows)
}

func scanWorkItems(rows *sql.Rows) ([]WorkItem, error) {
	var items []WorkItem
	for rows.Next() {
		item, err := scanWorkItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpdateHeartbeat sets heartbeat_at = now() for the given item ID.
func (r *WorkItemRepo) UpdateHeartbeat(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE work_items SET heartbeat_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("db: update heartbeat: %w", err)
	}
	return nil
}

// DeleteWorkItem removes a work item by ID.
func (r *WorkItemRepo) DeleteWorkItem(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM work_items WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("db: delete work item: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Workflow repository
// ---------------------------------------------------------------------------

// WorkflowRepository defines the data access contract for workflow definitions.
type WorkflowRepository interface {
	SaveWorkflow(ctx context.Context, def workflow.WorkflowDefinition) error
	GetWorkflow(ctx context.Context, name string) (workflow.WorkflowDefinition, error)
	ListWorkflows(ctx context.Context) ([]workflow.WorkflowDefinition, error)
}

// WorkflowRepo provides workflow-definition storage.
type WorkflowRepo struct {
	db DB
}

// NewWorkflowRepo creates a WorkflowRepo using db.
func NewWorkflowRepo(db DB) *WorkflowRepo {
	return &WorkflowRepo{db: db}
}

// SaveWorkflow inserts or replaces a workflow definition as JSON.
func (r *WorkflowRepo) SaveWorkflow(ctx context.Context, def workflow.WorkflowDefinition) error {
	data, err := marshalWorkflow(def)
	if err != nil {
		return fmt.Errorf("db: save workflow: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO workflow_definitions (name, definition_json, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (name) DO UPDATE
		  SET definition_json = EXCLUDED.definition_json,
		      updated_at = EXCLUDED.updated_at`,
		def.Name, data)
	if err != nil {
		return fmt.Errorf("db: save workflow: %w", err)
	}
	return nil
}

// GetWorkflow retrieves a workflow definition by name.
func (r *WorkflowRepo) GetWorkflow(ctx context.Context, name string) (workflow.WorkflowDefinition, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT definition_json FROM workflow_definitions WHERE name = $1`, name)
	var data string
	if err := row.Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return workflow.WorkflowDefinition{}, ErrNotFound
		}
		return workflow.WorkflowDefinition{}, fmt.Errorf("db: get workflow %q: %w", name, err)
	}
	return unmarshalWorkflow(data)
}

// ListWorkflows returns all stored workflow definitions.
func (r *WorkflowRepo) ListWorkflows(ctx context.Context) ([]workflow.WorkflowDefinition, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT definition_json FROM workflow_definitions ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("db: list workflows: %w", err)
	}
	defer rows.Close()

	var defs []workflow.WorkflowDefinition
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("db: list workflows scan: %w", err)
		}
		def, err := unmarshalWorkflow(data)
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	return defs, rows.Err()
}

// ---------------------------------------------------------------------------
// MetricEvent repository
// ---------------------------------------------------------------------------

// MetricsRepository defines the data access contract for metric events.
type MetricsRepository interface {
	RecordEvent(ctx context.Context, event MetricEvent) error
	QueryEvents(ctx context.Context, botID string, from, to time.Time) ([]MetricEvent, error)
}

// MetricEvent is the DB representation of a metric event.
type MetricEvent struct {
	ID              string
	EventType       string
	BotID           string
	ItemID          string
	StepName        string
	DurationMinutes float64
	CostUSD         float64
	Timestamp       time.Time
}

// ToMetricEvent converts the DB MetricEvent to the domain type.
func (e MetricEvent) ToMetricEvent() metrics.MetricEvent {
	return metrics.MetricEvent{
		EventType:       e.EventType,
		BotID:           metrics.BotID(e.BotID),
		ItemID:          metrics.WorkItemID(e.ItemID),
		StepName:        e.StepName,
		DurationMinutes: e.DurationMinutes,
		CostUSD:         e.CostUSD,
		Timestamp:       e.Timestamp,
	}
}

// MetricRepo provides metric-event storage.
type MetricRepo struct {
	db DB
}

// NewMetricRepo creates a MetricRepo using db.
func NewMetricRepo(db DB) *MetricRepo {
	return &MetricRepo{db: db}
}

// RecordEvent inserts a metric event row.
func (r *MetricRepo) RecordEvent(ctx context.Context, event MetricEvent) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO metric_events (id, event_type, bot_id, item_id, step_name,
			duration_minutes, cost_usd, timestamp)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		event.ID, event.EventType, event.BotID, event.ItemID, event.StepName,
		event.DurationMinutes, event.CostUSD, event.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("db: record event: %w", err)
	}
	return nil
}

// QueryEvents returns metric events for botID within [from, to].
func (r *MetricRepo) QueryEvents(ctx context.Context, botID string, from, to time.Time) ([]MetricEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, event_type, bot_id, item_id, step_name,
		       duration_minutes, cost_usd, timestamp
		FROM metric_events
		WHERE bot_id = $1 AND timestamp >= $2 AND timestamp <= $3
		ORDER BY timestamp`, botID, from, to)
	if err != nil {
		return nil, fmt.Errorf("db: query events: %w", err)
	}
	defer rows.Close()

	var events []MetricEvent
	for rows.Next() {
		var e MetricEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.BotID, &e.ItemID, &e.StepName,
			&e.DurationMinutes, &e.CostUSD, &e.Timestamp); err != nil {
			return nil, fmt.Errorf("db: query events scan: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
