package orchestrator_test

// This file covers P2.3's acceptance criteria: mentioning persona A
// dispatches only to persona A's bridge (FR-004), and concurrent dispatch
// from 2+ personas does not block, drop, or cross-interleave DirectTask/
// board writes (FR-006). It runs against the REAL infrastructure stores
// (InMemoryDirectTaskStore/InMemoryBoardStore/LocalTaskDispatcher/Router) --
// not fakes -- specifically so `go test -race` exercises their actual
// sync.RWMutex-protected concurrent access, per architecture.md's "add
// -race test coverage to lock in the guarantee" decision. It deliberately
// avoids internal/infrastructure/buzz (whose real relay-client JSON path
// hits an unrelated, pre-existing third-party checkptr crash under -race,
// confirmed present before this feature on the base commit -- see
// specs/260814-boabot-native-daemon-mode/implementation-notes.md) by
// simulating "N monitors" as N goroutines calling BuzzTaskBridge.Dispatch
// directly, which is the exact seam a real Buzz Monitor calls through.

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/orchestrator"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	orchestratorlocal "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/orchestrator"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/queue"
)

// TestBuzzTaskBridge_ConcurrentMultiPersonaDispatch_NoCrossTalk simulates
// two Buzz-enabled personas ("architect" and "tech-lead"), each with its own
// isolated BuzzTaskBridge/ChatTaskManager (matching the per-persona
// isolation decision in implementation-notes.md), dispatching many
// concurrent immediate instructions against one shared DirectTaskStore/
// BoardStore/Dispatcher -- the same shared stores TeamManager.Run() wires
// for real. Run with -race.
func TestBuzzTaskBridge_ConcurrentMultiPersonaDispatch_NoCrossTalk(t *testing.T) {
	router := queue.NewRouter()
	router.Register("architect", 0)
	router.Register("tech-lead", 0)
	router.Register("orchestrator", 0)

	store := orchestratorlocal.NewInMemoryDirectTaskStore("")
	board := orchestratorlocal.NewInMemoryBoardStore("")
	dispatcher := orchestratorlocal.NewLocalTaskDispatcher(store, router.QueueFor("orchestrator"), "orchestrator")

	// Per-persona isolated bridge instances, as production wiring
	// (TeamManager.Run()'s BuzzMonitorBuilder loop) constructs one per
	// Buzz-enabled team.yaml entry.
	architectBridge := orchestrator.NewBuzzTaskBridge(dispatcher, board, orchestrator.NewChatTaskManager(dispatcher))
	techLeadBridge := orchestrator.NewBuzzTaskBridge(dispatcher, board, orchestrator.NewChatTaskManager(dispatcher))

	const perPersonaDispatches = 25
	var wg sync.WaitGroup
	errs := make(chan error, perPersonaDispatches*2)

	dispatchN := func(bridge *orchestrator.BuzzTaskBridge, botName string) {
		defer wg.Done()
		for i := 0; i < perPersonaDispatches; i++ {
			ctx := context.Background()
			eventID := fmt.Sprintf("%s-evt-%d", botName, i)
			threadID := fmt.Sprintf("%s-chan", botName)
			instruction := fmt.Sprintf("do task %d for %s", i, botName)
			result, err := bridge.Dispatch(ctx, botName, eventID, threadID, instruction)
			if err != nil {
				errs <- fmt.Errorf("%s dispatch %d: %w", botName, i, err)
				continue
			}
			if result.TaskID == "" {
				errs <- fmt.Errorf("%s dispatch %d: expected a TaskID", botName, i)
			}
		}
	}

	wg.Add(2)
	go dispatchN(architectBridge, "architect")
	go dispatchN(techLeadBridge, "tech-lead")
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	all, err := store.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != perPersonaDispatches*2 {
		t.Fatalf("expected %d total DirectTasks, got %d", perPersonaDispatches*2, len(all))
	}

	// No cross-talk: every task's BotName must match the persona whose
	// bridge dispatched it (never the other persona).
	counts := map[string]int{}
	for _, task := range all {
		counts[task.BotName]++
		if task.BotName != "architect" && task.BotName != "tech-lead" {
			t.Errorf("unexpected BotName on dispatched task: %+v", task)
		}
		if task.Source != domain.DirectTaskSourceBuzz {
			t.Errorf("expected DirectTaskSourceBuzz, got %q on task %+v", task.Source, task)
		}
	}
	if counts["architect"] != perPersonaDispatches {
		t.Errorf("expected %d architect tasks, got %d (cross-talk?)", perPersonaDispatches, counts["architect"])
	}
	if counts["tech-lead"] != perPersonaDispatches {
		t.Errorf("expected %d tech-lead tasks, got %d (cross-talk?)", perPersonaDispatches, counts["tech-lead"])
	}

	items, err := board.List(context.Background(), domain.WorkItemFilter{})
	if err != nil {
		t.Fatalf("board List: %v", err)
	}
	if len(items) != perPersonaDispatches*2 {
		t.Fatalf("expected %d total board items, got %d", perPersonaDispatches*2, len(items))
	}
	boardCounts := map[string]int{}
	for _, item := range items {
		boardCounts[item.AssignedTo]++
	}
	if boardCounts["architect"] != perPersonaDispatches || boardCounts["tech-lead"] != perPersonaDispatches {
		t.Errorf("expected %d board items per persona, got %+v (cross-talk?)", perPersonaDispatches, boardCounts)
	}
}

// TestBuzzTaskBridge_ConcurrentDispatch_SameEventID_ExactlyOneProceeds is an
// FR-101 follow-up: checkAndMarkSeen's check-and-set must be atomic under
// one lock acquisition, not two separate calls, so that two goroutines
// calling Dispatch with the IDENTICAL event ID concurrently cannot both
// observe "not a duplicate" and both dispatch. Only one may proceed; the
// other must be reported Duplicate. Run with -race.
func TestBuzzTaskBridge_ConcurrentDispatch_SameEventID_ExactlyOneProceeds(t *testing.T) {
	router := queue.NewRouter()
	router.Register("architect", 0)
	router.Register("orchestrator", 0)

	store := orchestratorlocal.NewInMemoryDirectTaskStore("")
	board := orchestratorlocal.NewInMemoryBoardStore("")
	dispatcher := orchestratorlocal.NewLocalTaskDispatcher(store, router.QueueFor("orchestrator"), "orchestrator")
	bridge := orchestrator.NewBuzzTaskBridge(dispatcher, board, orchestrator.NewChatTaskManager(dispatcher))

	const concurrent = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	var duplicates, dispatched int

	wg.Add(concurrent)
	for i := 0; i < concurrent; i++ {
		go func() {
			defer wg.Done()
			result, err := bridge.Dispatch(context.Background(), "architect", "evt-same", "chan-1", "review the PR")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if result.Duplicate {
				duplicates++
			} else {
				dispatched++
			}
		}()
	}
	wg.Wait()

	if dispatched != 1 {
		t.Fatalf("expected exactly 1 goroutine to actually dispatch, got %d", dispatched)
	}
	if duplicates != concurrent-1 {
		t.Fatalf("expected %d goroutines to be reported Duplicate, got %d", concurrent-1, duplicates)
	}

	all, err := store.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected exactly 1 DirectTask created for the shared event ID, got %d", len(all))
	}
}
