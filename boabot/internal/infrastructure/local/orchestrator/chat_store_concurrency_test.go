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

// TestInMemoryChatStore_ConcurrentCrossProcessAppends_BothMessagesSurvive is
// FR-503's regression test for the same cross-process clobber hazard
// board.json had (specs/archive/260815-acp-harness-feature-parity-auto-review/):
// InMemoryChatStore.persist() unconditionally serializes only its own
// in-memory state on every mutation, with no lock and no re-read of the
// current on-disk state. Two store instances sharing one persistPath --
// standing in for native mode and an ACP-mode process now sharing chat.json
// under FR-502's shared-state root -- each Append a distinct message
// concurrently. Must fail against the unfixed persist() and pass once
// persist() acquires the shared filelock, re-reads persistPath immediately
// before writing, and merges by message ID.
func TestInMemoryChatStore_ConcurrentCrossProcessAppends_BothMessagesSurvive(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "chat.json")

	storeA := orchestrator.NewInMemoryChatStore(path)
	storeB := orchestrator.NewInMemoryChatStore(path)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = storeA.Append(context.Background(), domain.ChatMessage{
			BotName:   "orchestrator",
			Direction: domain.ChatDirectionInbound,
			Content:   "message-a",
		})
	}()
	go func() {
		defer wg.Done()
		_ = storeB.Append(context.Background(), domain.ChatMessage{
			BotName:   "orchestrator",
			Direction: domain.ChatDirectionInbound,
			Content:   "message-b",
		})
	}()
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading persisted chat file: %v", err)
	}
	var state struct {
		Messages []domain.ChatMessage `json:"messages"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshaling persisted chat file: %v", err)
	}

	contents := make(map[string]bool, len(state.Messages))
	for _, m := range state.Messages {
		contents[m.Content] = true
	}

	if len(state.Messages) != 2 || !contents["message-a"] || !contents["message-b"] {
		t.Fatalf("expected persisted chat file to contain both message-a and message-b (2 messages), got %d messages: %+v", len(state.Messages), state.Messages)
	}
}
