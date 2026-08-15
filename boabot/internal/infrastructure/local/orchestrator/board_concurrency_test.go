package orchestrator_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
)

// TestInMemoryBoardStore_ConcurrentCrossProcessWrites_BothItemsSurvive is
// FR-1's (T-FR1, spec.md acceptance criterion) regression test for the
// cross-process board.json clobber hazard: two InMemoryBoardStore instances
// sharing one persistPath -- standing in for two separate `boabot -acp`
// processes sharing an identical board path, per buzz-acp's documented
// agent-pool operation (ADR-B026) -- each Create a distinct item
// concurrently. Neither store instance ever observes the other's item in
// its own in-memory state (each loads persistPath exactly once, at
// construction, before either has written anything), so this is
// deterministic, not scheduler-luck-dependent: pre-fix, InMemoryBoardStore.
// persist() unconditionally serializes only its own in-memory s.items on
// every mutation (no lock, no re-read of the current on-disk state) --
// whichever store's persist() call is the last to complete always
// overwrites the file with only its own single item, discarding the
// other's, regardless of how the two goroutines happen to interleave.
//
// This must fail against the current (unfixed) persist() and pass once
// persist() acquires a cross-process lock, re-reads persistPath immediately
// before writing, and merges by item ID (union of disk state and this
// process's own touched item) instead of blindly overwriting with stale
// in-memory state.
func TestInMemoryBoardStore_ConcurrentCrossProcessWrites_BothItemsSurvive(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "board.json")

	// Two independent store instances against the same path -- simulating
	// two separate boabot -acp processes (or one ACP process plus native
	// mode's TeamManager) sharing the identical board.json path that
	// buzz-acp's documented agent pool (ADR-B026) makes routine, not a
	// contrived edge case.
	storeA := orchestrator.NewInMemoryBoardStore(path)
	storeB := orchestrator.NewInMemoryBoardStore(path)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = storeA.Create(context.Background(), domain.WorkItem{
			Title:  "item-a",
			Status: domain.WorkItemStatusBacklog,
		})
	}()
	go func() {
		defer wg.Done()
		_, _ = storeB.Create(context.Background(), domain.WorkItem{
			Title:  "item-b",
			Status: domain.WorkItemStatusBacklog,
		})
	}()
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading persisted board file: %v", err)
	}
	var items []domain.WorkItem
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("unmarshaling persisted board file: %v", err)
	}

	titles := make(map[string]bool, len(items))
	for _, it := range items {
		titles[it.Title] = true
	}

	if len(items) != 2 || !titles["item-a"] || !titles["item-b"] {
		t.Fatalf("expected persisted board file to contain both item-a and item-b (2 items), got %d items: %+v", len(items), items)
	}
}

// TestInMemoryBoardStore_Persist_CreatesNestedPersistDirectory guards the
// production path team_manager.go/acp.go actually exercise on first boot:
// persistPath's parent directory may not exist yet. Pre-fix, persist()'s
// own os.MkdirAll created it; post-fix, the first filesystem operation for
// a mutation is filelock.AcquireWait's lock-file publish, so directory
// creation must still happen -- verified here rather than assumed, since
// both other tests in this package use an already-existing t.TempDir().
func TestInMemoryBoardStore_Persist_CreatesNestedPersistDirectory(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nested", "does-not-exist-yet", "board.json")

	store := orchestrator.NewInMemoryBoardStore(path)
	created, err := store.Create(context.Background(), domain.WorkItem{
		Title:  "nested-dir-item",
		Status: domain.WorkItemStatusBacklog,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected persist() to create %q's parent directory and write the board file, but reading it failed: %v", path, err)
	}
	var items []domain.WorkItem
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("unmarshaling persisted board file: %v", err)
	}
	if len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("expected persisted board file to contain the single created item, got: %+v", items)
	}
}
