package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
)

func TestInMemoryDirectTaskStore_Create(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	task := domain.DirectTask{
		BotName:     "dev-1",
		Instruction: "write tests",
		Status:      domain.DirectTaskStatusPending,
	}
	created, err := store.Create(ctx, task)
	if err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Error("expected non-empty ID after Create")
	}
	if created.BotName != task.BotName {
		t.Errorf("BotName mismatch: got %q, want %q", created.BotName, task.BotName)
	}
	if created.Instruction != task.Instruction {
		t.Errorf("Instruction mismatch: got %q, want %q", created.Instruction, task.Instruction)
	}
	if created.Status != domain.DirectTaskStatusPending {
		t.Errorf("Status mismatch: got %q, want %q", created.Status, domain.DirectTaskStatusPending)
	}
}

func TestInMemoryDirectTaskStore_Create_SetsTimestamps(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	before := time.Now()
	task := domain.DirectTask{BotName: "dev-1", Status: domain.DirectTaskStatusPending}
	created, err := store.Create(ctx, task)
	after := time.Now()

	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.CreatedAt.Before(before) || created.CreatedAt.After(after) {
		t.Errorf("CreatedAt %v not in expected range [%v, %v]", created.CreatedAt, before, after)
	}
	if created.UpdatedAt.Before(before) || created.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt %v not in expected range [%v, %v]", created.UpdatedAt, before, after)
	}
}

func TestInMemoryDirectTaskStore_Create_UniqueIDs(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	ids := make(map[string]bool)
	for range 10 {
		task := domain.DirectTask{BotName: "dev-1", Status: domain.DirectTaskStatusPending}
		created, err := store.Create(ctx, task)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if ids[created.ID] {
			t.Errorf("duplicate ID generated: %q", created.ID)
		}
		ids[created.ID] = true
	}
}

func TestInMemoryDirectTaskStore_Get(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	task := domain.DirectTask{BotName: "dev-1", Status: domain.DirectTaskStatusPending}
	created, _ := store.Create(ctx, task)

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get returned unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, created.ID)
	}
}

func TestInMemoryDirectTaskStore_Get_NotFound(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent ID, got nil")
	}
}

func TestInMemoryDirectTaskStore_Update(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	task := domain.DirectTask{BotName: "dev-1", Status: domain.DirectTaskStatusPending}
	created, _ := store.Create(ctx, task)

	now := time.Now().UTC()
	created.Status = domain.DirectTaskStatusDispatched
	created.DispatchedAt = &now
	updated, err := store.Update(ctx, created)
	if err != nil {
		t.Fatalf("Update returned unexpected error: %v", err)
	}
	if updated.Status != domain.DirectTaskStatusDispatched {
		t.Errorf("expected Status=dispatched, got %q", updated.Status)
	}
	if updated.DispatchedAt == nil {
		t.Error("expected DispatchedAt to be set")
	}
}

func TestInMemoryDirectTaskStore_Update_NotFound(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	task := domain.DirectTask{ID: "nonexistent", BotName: "dev-1", Status: domain.DirectTaskStatusPending}
	_, err := store.Update(ctx, task)
	if err == nil {
		t.Error("expected error when updating nonexistent item, got nil")
	}
}

func TestInMemoryDirectTaskStore_List_FilterByBotName(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	_, _ = store.Create(ctx, domain.DirectTask{BotName: "dev-1", Status: domain.DirectTaskStatusPending})
	_, _ = store.Create(ctx, domain.DirectTask{BotName: "dev-1", Status: domain.DirectTaskStatusPending})
	_, _ = store.Create(ctx, domain.DirectTask{BotName: "qa-1", Status: domain.DirectTaskStatusPending})

	items, err := store.List(ctx, "dev-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items for dev-1, got %d", len(items))
	}
	for _, item := range items {
		if item.BotName != "dev-1" {
			t.Errorf("unexpected BotName: %q", item.BotName)
		}
	}
}

func TestInMemoryDirectTaskStore_List_EmptyBotName_ReturnsAll(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	_, _ = store.Create(ctx, domain.DirectTask{BotName: "dev-1", Status: domain.DirectTaskStatusPending})
	_, _ = store.Create(ctx, domain.DirectTask{BotName: "qa-1", Status: domain.DirectTaskStatusPending})

	items, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items with empty botName, got %d", len(items))
	}
}

func TestInMemoryDirectTaskStore_List_ReturnsNonNilSliceWhenEmpty(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	items, err := store.List(ctx, "dev-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if items == nil {
		t.Error("expected non-nil slice from List on empty store")
	}
}

func TestInMemoryDirectTaskStore_ListAll(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	_, _ = store.Create(ctx, domain.DirectTask{BotName: "dev-1", Status: domain.DirectTaskStatusPending})
	_, _ = store.Create(ctx, domain.DirectTask{BotName: "qa-1", Status: domain.DirectTaskStatusDispatched})
	_, _ = store.Create(ctx, domain.DirectTask{BotName: "dev-2", Status: domain.DirectTaskStatusFailed})

	items, err := store.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items from ListAll, got %d", len(items))
	}
}

func TestInMemoryDirectTaskStore_ListAll_ReturnsNonNilSliceWhenEmpty(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	items, err := store.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if items == nil {
		t.Error("expected non-nil slice from ListAll on empty store")
	}
}

func TestInMemoryDirectTaskStore_ListAll_SortedNewestFirst(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryDirectTaskStore()
	ctx := context.Background()

	// Create tasks with distinct timestamps by setting CreatedAt explicitly.
	t1 := time.Now().Add(-2 * time.Second)
	t2 := time.Now().Add(-1 * time.Second)
	t3 := time.Now()

	task1, _ := store.Create(ctx, domain.DirectTask{BotName: "dev-1", Status: domain.DirectTaskStatusPending})
	task2, _ := store.Create(ctx, domain.DirectTask{BotName: "dev-1", Status: domain.DirectTaskStatusPending})
	task3, _ := store.Create(ctx, domain.DirectTask{BotName: "dev-1", Status: domain.DirectTaskStatusPending})

	// Update with explicit CreatedAt to control ordering.
	task1.CreatedAt = t1
	_, _ = store.Update(ctx, task1)
	task2.CreatedAt = t2
	_, _ = store.Update(ctx, task2)
	task3.CreatedAt = t3
	_, _ = store.Update(ctx, task3)

	items, err := store.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// Newest first: task3 (t3) > task2 (t2) > task1 (t1)
	if !items[0].CreatedAt.Equal(t3) {
		t.Errorf("expected first item to have CreatedAt=%v, got %v", t3, items[0].CreatedAt)
	}
	if !items[2].CreatedAt.Equal(t1) {
		t.Errorf("expected last item to have CreatedAt=%v, got %v", t1, items[2].CreatedAt)
	}
}
