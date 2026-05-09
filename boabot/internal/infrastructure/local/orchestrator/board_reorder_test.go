package orchestrator_test

import (
	"context"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
)

// TestReorder_SetsPositionsInOrder creates 3 backlog items, calls Reorder with
// reversed IDs, and verifies List returns them in the new order.
func TestReorder_SetsPositionsInOrder(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore("")
	ctx := context.Background()

	a, _ := store.Create(ctx, domain.WorkItem{Title: "A", Status: domain.WorkItemStatusBacklog})
	b, _ := store.Create(ctx, domain.WorkItem{Title: "B", Status: domain.WorkItemStatusBacklog})
	c, _ := store.Create(ctx, domain.WorkItem{Title: "C", Status: domain.WorkItemStatusBacklog})

	// Reorder as C, B, A.
	if err := store.Reorder(ctx, []string{c.ID, b.ID, a.ID}); err != nil {
		t.Fatalf("Reorder: %v", err)
	}

	items, err := store.List(ctx, domain.WorkItemFilter{Status: domain.WorkItemStatusBacklog})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	wantOrder := []string{c.ID, b.ID, a.ID}
	for i, it := range items {
		if it.ID != wantOrder[i] {
			t.Errorf("position %d: expected ID %q, got %q", i, wantOrder[i], it.ID)
		}
	}
}

// TestCreate_SetsPositionAtBottom verifies that each new item in a column gets
// a SortPosition equal to the number of existing items + 1.
func TestCreate_SetsPositionAtBottom(t *testing.T) {
	t.Parallel()
	store := orchestrator.NewInMemoryBoardStore("")
	ctx := context.Background()

	first, _ := store.Create(ctx, domain.WorkItem{Title: "first", Status: domain.WorkItemStatusBacklog})
	second, _ := store.Create(ctx, domain.WorkItem{Title: "second", Status: domain.WorkItemStatusBacklog})
	third, _ := store.Create(ctx, domain.WorkItem{Title: "third", Status: domain.WorkItemStatusBacklog})

	if first.SortPosition != 1 {
		t.Errorf("first item: expected SortPosition=1, got %d", first.SortPosition)
	}
	if second.SortPosition != 2 {
		t.Errorf("second item: expected SortPosition=2, got %d", second.SortPosition)
	}
	if third.SortPosition != 3 {
		t.Errorf("third item: expected SortPosition=3, got %d", third.SortPosition)
	}
}
