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

// TestInMemoryDirectTaskStore_ConcurrentCrossProcessWrites_BothTasksSurvive
// is FR-502's regression test for the same cross-process clobber hazard
// board.json had (specs/archive/260815-acp-harness-feature-parity-auto-review/):
// InMemoryDirectTaskStore.persist() unconditionally serializes only its own
// in-memory s.tasks on every mutation, with no lock and no re-read of the
// current on-disk state. Two store instances sharing one persistPath --
// standing in for native mode's TeamManager and an ACP-mode process now
// sharing tasks.json under FR-502's shared-state root -- each Create a
// distinct task concurrently; whichever store's persist() call finishes
// last always overwrites the file with only its own single task, discarding
// the other's, regardless of interleaving. Must fail against the unfixed
// persist() and pass once persist() acquires the shared filelock, re-reads
// persistPath immediately before writing, and merges by task ID.
func TestInMemoryDirectTaskStore_ConcurrentCrossProcessWrites_BothTasksSurvive(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "tasks.json")

	storeA := orchestrator.NewInMemoryDirectTaskStore(path)
	storeB := orchestrator.NewInMemoryDirectTaskStore(path)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = storeA.Create(context.Background(), domain.DirectTask{
			BotName:     "orchestrator",
			Source:      domain.DirectTaskSourceChat,
			Instruction: "task-a",
		})
	}()
	go func() {
		defer wg.Done()
		_, _ = storeB.Create(context.Background(), domain.DirectTask{
			BotName:     "orchestrator",
			Source:      domain.DirectTaskSourceChat,
			Instruction: "task-b",
		})
	}()
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading persisted tasks file: %v", err)
	}
	var tasks []domain.DirectTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		t.Fatalf("unmarshaling persisted tasks file: %v", err)
	}

	instructions := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		instructions[task.Instruction] = true
	}

	if len(tasks) != 2 || !instructions["task-a"] || !instructions["task-b"] {
		t.Fatalf("expected persisted tasks file to contain both task-a and task-b (2 tasks), got %d tasks: %+v", len(tasks), tasks)
	}
}
