package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
)

func TestInMemoryBoardStore_Create(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore()
	ctx := context.Background()

	item := domain.WorkItem{
		Title:       "test item",
		Description: "desc",
		Status:      domain.WorkItemStatusBacklog,
		CreatedBy:   "alice",
	}
	created, err := store.Create(ctx, item)
	if err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Error("expected non-empty ID after Create")
	}
	if created.Title != item.Title {
		t.Errorf("Title mismatch: got %q, want %q", created.Title, item.Title)
	}
	if created.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set after Create")
	}
}

func TestInMemoryBoardStore_Create_SetsUpdatedAt(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore()
	ctx := context.Background()

	before := time.Now()
	item := domain.WorkItem{Title: "t", Status: domain.WorkItemStatusBacklog}
	created, err := store.Create(ctx, item)
	after := time.Now()

	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.UpdatedAt.Before(before) || created.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt %v not in expected range [%v, %v]", created.UpdatedAt, before, after)
	}
}

func TestInMemoryBoardStore_Create_UniqueIDs(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore()
	ctx := context.Background()

	ids := make(map[string]bool)
	for range 10 {
		item := domain.WorkItem{Title: "item", Status: domain.WorkItemStatusBacklog}
		created, err := store.Create(ctx, item)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if ids[created.ID] {
			t.Errorf("duplicate ID generated: %q", created.ID)
		}
		ids[created.ID] = true
	}
}

func TestInMemoryBoardStore_Get(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore()
	ctx := context.Background()

	item := domain.WorkItem{Title: "get me", Status: domain.WorkItemStatusBacklog}
	created, _ := store.Create(ctx, item)

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get returned unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, created.ID)
	}
}

func TestInMemoryBoardStore_Get_NotFound(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore()
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent ID, got nil")
	}
}

func TestInMemoryBoardStore_Update(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore()
	ctx := context.Background()

	item := domain.WorkItem{Title: "original", Status: domain.WorkItemStatusBacklog}
	created, _ := store.Create(ctx, item)

	created.Title = "updated"
	created.Status = domain.WorkItemStatusInProgress
	updated, err := store.Update(ctx, created)
	if err != nil {
		t.Fatalf("Update returned unexpected error: %v", err)
	}
	if updated.Title != "updated" {
		t.Errorf("expected Title=updated, got %q", updated.Title)
	}
	if updated.Status != domain.WorkItemStatusInProgress {
		t.Errorf("expected Status=in-progress, got %q", updated.Status)
	}
}

func TestInMemoryBoardStore_Update_NotFound(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore()
	ctx := context.Background()

	item := domain.WorkItem{ID: "nonexistent", Title: "x", Status: domain.WorkItemStatusBacklog}
	_, err := store.Update(ctx, item)
	if err == nil {
		t.Error("expected error when updating nonexistent item, got nil")
	}
}

func TestInMemoryBoardStore_List_NoFilter(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore()
	ctx := context.Background()

	for range 3 {
		_, _ = store.Create(ctx, domain.WorkItem{Title: "item", Status: domain.WorkItemStatusBacklog})
	}

	items, err := store.List(ctx, domain.WorkItemFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestInMemoryBoardStore_List_FilterByStatus(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore()
	ctx := context.Background()

	_, _ = store.Create(ctx, domain.WorkItem{Title: "backlog", Status: domain.WorkItemStatusBacklog})
	_, _ = store.Create(ctx, domain.WorkItem{Title: "in-progress", Status: domain.WorkItemStatusInProgress})
	_, _ = store.Create(ctx, domain.WorkItem{Title: "done", Status: domain.WorkItemStatusDone})

	items, err := store.List(ctx, domain.WorkItemFilter{Status: domain.WorkItemStatusInProgress})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 in-progress item, got %d", len(items))
	}
	if items[0].Status != domain.WorkItemStatusInProgress {
		t.Errorf("unexpected status: %q", items[0].Status)
	}
}

func TestInMemoryBoardStore_List_FilterByAssignedTo(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore()
	ctx := context.Background()

	_, _ = store.Create(ctx, domain.WorkItem{Title: "alice item", Status: domain.WorkItemStatusBacklog, AssignedTo: "alice"})
	_, _ = store.Create(ctx, domain.WorkItem{Title: "bob item", Status: domain.WorkItemStatusBacklog, AssignedTo: "bob"})

	items, err := store.List(ctx, domain.WorkItemFilter{AssignedTo: "alice"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item for alice, got %d", len(items))
	}
	if items[0].AssignedTo != "alice" {
		t.Errorf("unexpected AssignedTo: %q", items[0].AssignedTo)
	}
}

func TestInMemoryBoardStore_List_FilterByStatusAndAssignedTo(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore()
	ctx := context.Background()

	_, _ = store.Create(ctx, domain.WorkItem{Title: "alice backlog", Status: domain.WorkItemStatusBacklog, AssignedTo: "alice"})
	_, _ = store.Create(ctx, domain.WorkItem{Title: "alice in-progress", Status: domain.WorkItemStatusInProgress, AssignedTo: "alice"})
	_, _ = store.Create(ctx, domain.WorkItem{Title: "bob backlog", Status: domain.WorkItemStatusBacklog, AssignedTo: "bob"})

	items, err := store.List(ctx, domain.WorkItemFilter{
		Status:     domain.WorkItemStatusBacklog,
		AssignedTo: "alice",
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}

func TestInMemoryBoardStore_List_ReturnsNonNilSliceWhenEmpty(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore()
	ctx := context.Background()

	items, err := store.List(ctx, domain.WorkItemFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if items == nil {
		t.Error("expected non-nil slice from List on empty store")
	}
}
