package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	localmcp "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/mcp"
)

// stubDirectTaskStore is a minimal domain.DirectTaskStore for tests.
type stubDirectTaskStore struct {
	tasks map[string]domain.DirectTask
}

func newStubDirectTaskStore(tasks ...domain.DirectTask) *stubDirectTaskStore {
	s := &stubDirectTaskStore{tasks: make(map[string]domain.DirectTask)}
	for _, t := range tasks {
		s.tasks[t.ID] = t
	}
	return s
}

func (s *stubDirectTaskStore) Create(_ context.Context, task domain.DirectTask) (domain.DirectTask, error) {
	s.tasks[task.ID] = task
	return task, nil
}

func (s *stubDirectTaskStore) Update(_ context.Context, task domain.DirectTask) (domain.DirectTask, error) {
	s.tasks[task.ID] = task
	return task, nil
}

func (s *stubDirectTaskStore) Get(_ context.Context, id string) (domain.DirectTask, error) {
	t, ok := s.tasks[id]
	if !ok {
		return domain.DirectTask{}, errors.New("not found")
	}
	return t, nil
}

func (s *stubDirectTaskStore) List(_ context.Context, botName string) ([]domain.DirectTask, error) {
	result := make([]domain.DirectTask, 0)
	for _, t := range s.tasks {
		if botName == "" || t.BotName == botName {
			result = append(result, t)
		}
	}
	return result, nil
}

func (s *stubDirectTaskStore) ListAll(_ context.Context) ([]domain.DirectTask, error) {
	return s.List(context.Background(), "")
}

func (s *stubDirectTaskStore) ListBySource(_ context.Context, source domain.DirectTaskSource) ([]domain.DirectTask, error) {
	result := make([]domain.DirectTask, 0)
	for _, t := range s.tasks {
		if t.Source == source {
			result = append(result, t)
		}
	}
	return result, nil
}

func (s *stubDirectTaskStore) Delete(_ context.Context, id string) error {
	delete(s.tasks, id)
	return nil
}

func (s *stubDirectTaskStore) ListDue(_ context.Context, _ time.Time) ([]domain.DirectTask, error) {
	return nil, nil
}

func (s *stubDirectTaskStore) ClaimDue(_ context.Context, _ string) (bool, error) {
	return false, nil
}

var _ domain.DirectTaskStore = (*stubDirectTaskStore)(nil)

// --- FR-601: list_board_items ---

func TestClient_ListTools_WithBoardStore_IncludesListTool(t *testing.T) {
	bs := newStubBoardStore()
	c := localmcp.NewClient([]string{"/tmp"}, localmcp.WithBoardStore(bs))
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "list_board_items" {
			found = true
		}
	}
	if !found {
		t.Error("expected list_board_items in tool list when board store is provided")
	}
}

func TestClient_ListTools_WithoutBoardStore_ExcludesListTool(t *testing.T) {
	c := localmcp.NewClient([]string{"/tmp"})
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range tools {
		if tool.Name == "list_board_items" {
			t.Error("list_board_items should not be present without a board store")
		}
	}
}

// TestClient_ListBoardItems_ReturnsRealItems is FR-601's regression test for
// today's confirmed bug: an agent asked what's on the board must get real
// item data back, not a false "no items" answer produced by having no way
// to check at all.
func TestClient_ListBoardItems_ReturnsRealItems(t *testing.T) {
	bs := newStubBoardStore(
		domain.WorkItem{ID: "item-1", Title: "Code review Pong project", Status: domain.WorkItemStatusBlocked, AssignedTo: "reviewer"},
		domain.WorkItem{ID: "item-2", Title: "Synk", Status: domain.WorkItemStatusBacklog, AssignedTo: "maintainer"},
	)
	c := localmcp.NewClient([]string{"/tmp"}, localmcp.WithBoardStore(bs))

	result, err := c.CallTool(context.Background(), "list_board_items", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	var items []map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &items); err != nil {
		t.Fatalf("expected JSON array content, got %q: %v", result.Content, err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d: %v", len(items), items)
	}
	titles := make(map[string]bool, len(items))
	for _, it := range items {
		titles[it["title"].(string)] = true
	}
	if !titles["Code review Pong project"] || !titles["Synk"] {
		t.Errorf("expected both real item titles present, got %v", items)
	}
}

func TestClient_ListBoardItems_EmptyBoard_ReturnsEmptyArrayNotError(t *testing.T) {
	bs := newStubBoardStore()
	c := localmcp.NewClient([]string{"/tmp"}, localmcp.WithBoardStore(bs))

	result, err := c.CallTool(context.Background(), "list_board_items", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success for a genuinely empty board, got error: %v", result.Content)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &items); err != nil {
		t.Fatalf("expected JSON array content, got %q: %v", result.Content, err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestClient_ListBoardItems_NoBoardStore_ReturnsError(t *testing.T) {
	c := localmcp.NewClient([]string{"/tmp"})
	result, err := c.CallTool(context.Background(), "list_board_items", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true when board store is unavailable")
	}
}

// --- FR-602: list_my_tasks ---

func TestClient_ListTools_WithDirectTaskStore_IncludesListTasksTool(t *testing.T) {
	ts := newStubDirectTaskStore()
	c := localmcp.NewClient([]string{"/tmp"}, localmcp.WithDirectTaskStore(ts, "orchestrator"))
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "list_my_tasks" {
			found = true
		}
	}
	if !found {
		t.Error("expected list_my_tasks in tool list when a direct task store is provided")
	}
}

func TestClient_ListTools_WithoutDirectTaskStore_ExcludesListTasksTool(t *testing.T) {
	c := localmcp.NewClient([]string{"/tmp"})
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range tools {
		if tool.Name == "list_my_tasks" {
			t.Error("list_my_tasks should not be present without a direct task store")
		}
	}
}

// TestClient_ListMyTasks_ReturnsOnlyThisBotsRealTasks is FR-602's regression
// test: an agent asked what's scheduled/running must get real DirectTask
// state back, scoped to its own bot name (ACP mode is single-persona --
// architecture.md AD-1/AD-2 -- so cross-bot visibility is out of scope).
func TestClient_ListMyTasks_ReturnsOnlyThisBotsRealTasks(t *testing.T) {
	nextRun := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	ts := newStubDirectTaskStore(
		domain.DirectTask{ID: "t1", BotName: "orchestrator", Status: domain.DirectTaskStatusPending, Instruction: "daily status report", NextRunAt: &nextRun},
		domain.DirectTask{ID: "t2", BotName: "architect", Status: domain.DirectTaskStatusRunning, Instruction: "review PRD"},
	)
	c := localmcp.NewClient([]string{"/tmp"}, localmcp.WithDirectTaskStore(ts, "orchestrator"))

	result, err := c.CallTool(context.Background(), "list_my_tasks", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	var tasks []map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &tasks); err != nil {
		t.Fatalf("expected JSON array content, got %q: %v", result.Content, err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected exactly 1 task (scoped to orchestrator), got %d: %v", len(tasks), tasks)
	}
	if tasks[0]["id"] != "t1" {
		t.Errorf("expected task t1 (the orchestrator's own), got %v", tasks[0])
	}
}

func TestClient_ListMyTasks_NoDirectTaskStore_ReturnsError(t *testing.T) {
	c := localmcp.NewClient([]string{"/tmp"})
	result, err := c.CallTool(context.Background(), "list_my_tasks", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true when no direct task store is configured")
	}
}
