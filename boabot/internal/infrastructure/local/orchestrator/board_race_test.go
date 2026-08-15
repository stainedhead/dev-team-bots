package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// TestInMemoryBoardStore_Persist_LockForcesSerialization is FR-1/T-FR1's
// stronger companion to TestInMemoryBoardStore_ConcurrentCrossProcessWrites_
// BothItemsSurvive (board_concurrency_test.go): rather than merely
// asserting the correct final outcome regardless of how two goroutines
// happen to interleave, this test deliberately forces the exact race
// window persist()'s cross-process lock exists to close, mirroring
// internal/infrastructure/buzz/lock_race_test.go's
// TestAcquireLock_ConcurrentRace_OnlyOneWinner technique: a package-level,
// test-only hook (persistAfterLockHook) stalls the FIRST persist() call to
// acquire the lock -- while it still holds it -- so the SECOND concurrent
// persist() call (from a different InMemoryBoardStore instance sharing the
// same persistPath) is deterministically driven onto filelock.AcquireWait's
// retry/backoff wait path, not merely lucky scheduling.
//
// This is a white-box test (package orchestrator, not orchestrator_test)
// because persistAfterLockHook is unexported, exactly as lock_race_test.go
// is package buzz to reach acquireLockAfterCreateHook.
func TestInMemoryBoardStore_Persist_LockForcesSerialization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.json")

	storeA := NewInMemoryBoardStore(path)
	storeB := NewInMemoryBoardStore(path)

	started := make(chan struct{})
	proceed := make(chan struct{})
	var stalled atomic.Bool

	prevHook := persistAfterLockHook
	persistAfterLockHook = func() {
		// Only the first persist() call to reach this hook -- i.e. the
		// first to actually win the lock -- stalls. A later call (the
		// second store's own persist(), once it finally acquires the
		// lock after the first releases) must pass straight through, or
		// this test would deadlock.
		if stalled.CompareAndSwap(false, true) {
			close(started)
			<-proceed
		}
	}
	t.Cleanup(func() { persistAfterLockHook = prevHook })

	type result struct {
		item domain.WorkItem
		err  error
	}
	aCh := make(chan result, 1)
	bCh := make(chan result, 1)

	go func() {
		item, err := storeA.Create(context.Background(), domain.WorkItem{
			Title:  "item-a",
			Status: domain.WorkItemStatusBacklog,
		})
		aCh <- result{item, err}
	}()

	<-started // whichever store's persist() won the lock is now stalled, holding it.

	go func() {
		item, err := storeB.Create(context.Background(), domain.WorkItem{
			Title:  "item-b",
			Status: domain.WorkItemStatusBacklog,
		})
		bCh <- result{item, err}
	}()

	// Give storeB's Create a real chance to reach filelock.AcquireWait and
	// enter its retry/backoff wait loop -- proving the lock actually
	// serializes the two calls, not merely that the eventual outcome
	// happens to be correct regardless of interleaving (which the
	// simpler, hook-free board_concurrency_test.go already covers). If B
	// completes before A releases the lock, the lock never blocked
	// anything and this test would be validating the wrong mechanism.
	time.Sleep(30 * time.Millisecond)
	select {
	case res := <-bCh:
		t.Fatalf("storeB.Create (item=%+v, err=%v) completed while storeA's persist() still held the lock -- the lock did not serialize the two calls", res.item, res.err)
	default:
	}

	close(proceed) // release the stalled persist() to finish its write and release the lock.

	aRes := <-aCh
	bRes := <-bCh

	if aRes.err != nil {
		t.Fatalf("storeA.Create: %v", aRes.err)
	}
	if bRes.err != nil {
		t.Fatalf("storeB.Create: %v", bRes.err)
	}

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
		t.Fatalf("expected persisted board file to contain both item-a and item-b (2 items) after forced lock contention, got %d items: %+v", len(items), items)
	}
}
