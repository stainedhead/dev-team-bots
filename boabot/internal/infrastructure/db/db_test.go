package db_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain/workflow"
	infradb "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/db"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

var testItem = infradb.WorkItem{
	ID:                 "item-1",
	Type:               "feature",
	Title:              "Add login page",
	Description:        "OAuth2 flow",
	Status:             domain.WorkItemStatusBacklog,
	Priority:           10,
	WorkflowStep:       "design",
	AssignedBotID:      "bot-1",
	CreatedAt:          time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	UpdatedAt:          time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	RateLimitedMinutes: 0,
	CostUSD:            0,
	Version:            1,
}

// workItemCols is the column list matching scanWorkItem.
var workItemCols = []string{
	"id", "type", "title", "description", "status", "priority",
	"workflow_step", "assigned_bot_id",
	"created_at", "updated_at", "future_start_at",
	"estimated_minutes", "actual_minutes",
	"eta_start_at", "eta_complete_at",
	"rate_limited_minutes", "cost_usd", "heartbeat_at", "version",
}

// itemRow converts a WorkItem into the []driver.Value slice that sqlmock needs.
func itemRow(item infradb.WorkItem) []driver.Value {
	return []driver.Value{
		item.ID, item.Type, item.Title, item.Description,
		string(item.Status), item.Priority,
		item.WorkflowStep, item.AssignedBotID,
		item.CreatedAt, item.UpdatedAt,
		nil, // future_start_at
		nil, // estimated_minutes
		nil, // actual_minutes
		nil, // eta_start_at
		nil, // eta_complete_at
		item.RateLimitedMinutes, item.CostUSD,
		nil, // heartbeat_at
		item.Version,
	}
}

func newMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

// ---------------------------------------------------------------------------
// WorkItemRepo — Create
// ---------------------------------------------------------------------------

func TestWorkItemRepo_Create(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	mock.ExpectExec(`INSERT INTO work_items`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.CreateWorkItem(context.Background(), testItem); err != nil {
		t.Fatalf("CreateWorkItem: unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestWorkItemRepo_Create_DBError(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	mock.ExpectExec(`INSERT INTO work_items`).
		WillReturnError(errors.New("connection refused"))

	if err := repo.CreateWorkItem(context.Background(), testItem); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// WorkItemRepo — Get
// ---------------------------------------------------------------------------

func TestWorkItemRepo_Get(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	rows := sqlmock.NewRows(workItemCols).AddRow(itemRow(testItem)...)
	mock.ExpectQuery(`SELECT .+ FROM work_items WHERE id`).
		WithArgs("item-1").
		WillReturnRows(rows)

	got, err := repo.GetWorkItem(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if got.ID != testItem.ID {
		t.Errorf("ID: got %q want %q", got.ID, testItem.ID)
	}
	if got.Title != testItem.Title {
		t.Errorf("Title: got %q want %q", got.Title, testItem.Title)
	}
}

func TestWorkItemRepo_Get_NotFound(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	// Empty rows → sql.ErrNoRows on Scan
	mock.ExpectQuery(`SELECT .+ FROM work_items WHERE id`).
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows(workItemCols))

	_, err := repo.GetWorkItem(context.Background(), "missing")
	if !errors.Is(err, infradb.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// WorkItemRepo — Update
// ---------------------------------------------------------------------------

func TestWorkItemRepo_Update_Success(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	mock.ExpectExec(`UPDATE work_items SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateWorkItem(context.Background(), testItem); err != nil {
		t.Fatalf("UpdateWorkItem: %v", err)
	}
}

func TestWorkItemRepo_Update_Conflict(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	mock.ExpectExec(`UPDATE work_items SET`).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows = version mismatch

	err := repo.UpdateWorkItem(context.Background(), testItem)
	if !errors.Is(err, infradb.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestWorkItemRepo_Update_DBError(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	mock.ExpectExec(`UPDATE work_items SET`).
		WillReturnError(errors.New("deadlock"))

	if err := repo.UpdateWorkItem(context.Background(), testItem); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// WorkItemRepo — ListByStatus
// ---------------------------------------------------------------------------

func TestWorkItemRepo_ListByStatus(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	rows := sqlmock.NewRows(workItemCols).AddRow(itemRow(testItem)...)
	mock.ExpectQuery(`SELECT .+ FROM work_items WHERE status`).
		WithArgs(domain.WorkItemStatusBacklog).
		WillReturnRows(rows)

	items, err := repo.ListByStatus(context.Background(), domain.WorkItemStatusBacklog)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestWorkItemRepo_ListByStatus_Empty(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	mock.ExpectQuery(`SELECT .+ FROM work_items WHERE status`).
		WithArgs(domain.WorkItemStatusDone).
		WillReturnRows(sqlmock.NewRows(workItemCols))

	items, err := repo.ListByStatus(context.Background(), domain.WorkItemStatusDone)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestWorkItemRepo_ListByStatus_Error(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	mock.ExpectQuery(`SELECT .+ FROM work_items WHERE status`).
		WillReturnError(errors.New("timeout"))

	_, err := repo.ListByStatus(context.Background(), domain.WorkItemStatusBacklog)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// WorkItemRepo — ListByBot
// ---------------------------------------------------------------------------

func TestWorkItemRepo_ListByBot(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	rows := sqlmock.NewRows(workItemCols).AddRow(itemRow(testItem)...)
	mock.ExpectQuery(`SELECT .+ FROM work_items WHERE assigned_bot_id`).
		WithArgs("bot-1").
		WillReturnRows(rows)

	items, err := repo.ListByBot(context.Background(), "bot-1")
	if err != nil {
		t.Fatalf("ListByBot: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestWorkItemRepo_ListByBot_Error(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	mock.ExpectQuery(`SELECT .+ FROM work_items WHERE assigned_bot_id`).
		WillReturnError(errors.New("network error"))

	_, err := repo.ListByBot(context.Background(), "bot-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// WorkItemRepo — ListStalled
// ---------------------------------------------------------------------------

func TestWorkItemRepo_ListStalled(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	cutoff := time.Now().Add(-5 * time.Minute)
	inProgress := testItem
	inProgress.Status = domain.WorkItemStatusInProgress

	rows := sqlmock.NewRows(workItemCols).AddRow(itemRow(inProgress)...)
	mock.ExpectQuery(`SELECT .+ FROM work_items`).
		WithArgs(cutoff).
		WillReturnRows(rows)

	items, err := repo.ListStalled(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("ListStalled: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 stalled item, got %d", len(items))
	}
}

func TestWorkItemRepo_ListStalled_Error(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	cutoff := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM work_items`).
		WillReturnError(errors.New("db down"))

	_, err := repo.ListStalled(context.Background(), cutoff)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// WorkItemRepo — UpdateHeartbeat
// ---------------------------------------------------------------------------

func TestWorkItemRepo_UpdateHeartbeat(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	mock.ExpectExec(`UPDATE work_items SET heartbeat_at`).
		WithArgs("item-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.UpdateHeartbeat(context.Background(), "item-1"); err != nil {
		t.Fatalf("UpdateHeartbeat: %v", err)
	}
}

func TestWorkItemRepo_UpdateHeartbeat_Error(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	mock.ExpectExec(`UPDATE work_items SET heartbeat_at`).
		WillReturnError(errors.New("disk full"))

	if err := repo.UpdateHeartbeat(context.Background(), "item-1"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// WorkItemRepo — Delete
// ---------------------------------------------------------------------------

func TestWorkItemRepo_Delete(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	mock.ExpectExec(`DELETE FROM work_items WHERE id`).
		WithArgs("item-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.DeleteWorkItem(context.Background(), "item-1"); err != nil {
		t.Fatalf("DeleteWorkItem: %v", err)
	}
}

func TestWorkItemRepo_Delete_Error(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkItemRepo(db)

	mock.ExpectExec(`DELETE FROM work_items WHERE id`).
		WillReturnError(errors.New("foreign key constraint"))

	if err := repo.DeleteWorkItem(context.Background(), "item-1"); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// WorkflowRepo
// ---------------------------------------------------------------------------

func sampleWorkflowDef() workflow.WorkflowDefinition {
	return workflow.WorkflowDefinition{
		Name: "feature",
		Steps: []workflow.WorkflowStep{
			{Name: "design", RequiredRole: "architect", NextStep: "implement"},
			{Name: "implement", RequiredRole: "coder", NextStep: ""},
		},
	}
}

func marshalWorkflowForTest(def workflow.WorkflowDefinition) (string, error) {
	b, err := json.Marshal(def)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func TestWorkflowRepo_Save(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkflowRepo(db)

	def := sampleWorkflowDef()
	mock.ExpectExec(`INSERT INTO workflow_definitions`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.SaveWorkflow(context.Background(), def); err != nil {
		t.Fatalf("SaveWorkflow: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestWorkflowRepo_Save_Error(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkflowRepo(db)

	mock.ExpectExec(`INSERT INTO workflow_definitions`).
		WillReturnError(errors.New("unique violation"))

	if err := repo.SaveWorkflow(context.Background(), sampleWorkflowDef()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWorkflowRepo_Get(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkflowRepo(db)

	def := sampleWorkflowDef()
	jsonStr, err := marshalWorkflowForTest(def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	mock.ExpectQuery(`SELECT definition_json FROM workflow_definitions WHERE name`).
		WithArgs("feature").
		WillReturnRows(sqlmock.NewRows([]string{"definition_json"}).AddRow(jsonStr))

	got, err := repo.GetWorkflow(context.Background(), "feature")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.Name != "feature" {
		t.Errorf("workflow name: got %q want %q", got.Name, "feature")
	}
	if len(got.Steps) != 2 {
		t.Errorf("steps: got %d want 2", len(got.Steps))
	}
}

func TestWorkflowRepo_Get_NotFound(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkflowRepo(db)

	mock.ExpectQuery(`SELECT definition_json FROM workflow_definitions WHERE name`).
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows([]string{"definition_json"}))

	_, err := repo.GetWorkflow(context.Background(), "missing")
	if !errors.Is(err, infradb.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWorkflowRepo_ListAll(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkflowRepo(db)

	def := sampleWorkflowDef()
	jsonStr, _ := marshalWorkflowForTest(def)

	mock.ExpectQuery(`SELECT definition_json FROM workflow_definitions ORDER BY name`).
		WillReturnRows(sqlmock.NewRows([]string{"definition_json"}).AddRow(jsonStr))

	defs, err := repo.ListWorkflows(context.Background())
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(defs))
	}
}

func TestWorkflowRepo_ListAll_Error(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewWorkflowRepo(db)

	mock.ExpectQuery(`SELECT definition_json FROM workflow_definitions ORDER BY name`).
		WillReturnError(errors.New("connection reset"))

	_, err := repo.ListWorkflows(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// MetricRepo
// ---------------------------------------------------------------------------

var evCols = []string{
	"id", "event_type", "bot_id", "item_id", "step_name",
	"duration_minutes", "cost_usd", "timestamp",
}

func TestMetricRepo_RecordEvent(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewMetricRepo(db)

	ev := infradb.MetricEvent{
		ID:              "ev-1",
		EventType:       "task.completed",
		BotID:           "bot-1",
		ItemID:          "item-1",
		StepName:        "implement",
		DurationMinutes: 30,
		CostUSD:         0.05,
		Timestamp:       time.Now().UTC(),
	}

	mock.ExpectExec(`INSERT INTO metric_events`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.RecordEvent(context.Background(), ev); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
}

func TestMetricRepo_RecordEvent_Error(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewMetricRepo(db)

	ev := infradb.MetricEvent{ID: "ev-1", Timestamp: time.Now()}
	mock.ExpectExec(`INSERT INTO metric_events`).
		WillReturnError(errors.New("duplicate key"))

	if err := repo.RecordEvent(context.Background(), ev); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMetricRepo_QueryEvents(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewMetricRepo(db)

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	ts := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT .+ FROM metric_events`).
		WithArgs("bot-1", from, to).
		WillReturnRows(
			sqlmock.NewRows(evCols).
				AddRow("ev-1", "task.completed", "bot-1", "item-1", "implement", 30.0, 0.05, ts),
		)

	events, err := repo.QueryEvents(context.Background(), "bot-1", from, to)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != "task.completed" {
		t.Errorf("EventType: got %q", events[0].EventType)
	}
}

func TestMetricRepo_QueryEvents_Error(t *testing.T) {
	db, mock := newMock(t)
	repo := infradb.NewMetricRepo(db)

	from := time.Now()
	to := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM metric_events`).
		WillReturnError(errors.New("index missing"))

	_, err := repo.QueryEvents(context.Background(), "bot-1", from, to)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMetricEvent_ToMetricEvent(t *testing.T) {
	ts := time.Now().UTC()
	ev := infradb.MetricEvent{
		ID:              "ev-1",
		EventType:       "task.completed",
		BotID:           "bot-1",
		ItemID:          "item-1",
		StepName:        "design",
		DurationMinutes: 15,
		CostUSD:         0.01,
		Timestamp:       ts,
	}
	dom := ev.ToMetricEvent()
	if dom.EventType != ev.EventType {
		t.Errorf("EventType mismatch: got %q want %q", dom.EventType, ev.EventType)
	}
	if string(dom.BotID) != ev.BotID {
		t.Errorf("BotID mismatch: got %q want %q", string(dom.BotID), ev.BotID)
	}
	if string(dom.ItemID) != ev.ItemID {
		t.Errorf("ItemID mismatch")
	}
}

// ---------------------------------------------------------------------------
// Migrate
// ---------------------------------------------------------------------------

func TestMigrate(t *testing.T) {
	db, mock := newMock(t)

	for i := 0; i < 4; i++ {
		mock.ExpectExec(`CREATE TABLE IF NOT EXISTS`).
			WillReturnResult(sqlmock.NewResult(0, 0))
	}

	if err := infradb.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMigrate_Error(t *testing.T) {
	db, mock := newMock(t)

	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS`).
		WillReturnError(errors.New("permission denied"))

	if err := infradb.Migrate(context.Background(), db); err == nil {
		t.Fatal("expected error, got nil")
	}
}
