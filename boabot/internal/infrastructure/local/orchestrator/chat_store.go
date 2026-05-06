package orchestrator

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// InMemoryChatStore implements domain.ChatStore with an in-memory slice ordered by CreatedAt.
type InMemoryChatStore struct {
	mu   sync.RWMutex
	msgs []domain.ChatMessage
}

// NewInMemoryChatStore creates a new InMemoryChatStore.
func NewInMemoryChatStore() *InMemoryChatStore {
	return &InMemoryChatStore{
		msgs: make([]domain.ChatMessage, 0),
	}
}

// Append adds a ChatMessage to the store.
// If ID is empty, a new random ID is generated.
// If CreatedAt is zero, it is set to the current UTC time.
func (s *InMemoryChatStore) Append(_ context.Context, msg domain.ChatMessage) error {
	if msg.ID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		msg.ID = id
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}

	s.mu.Lock()
	s.msgs = append(s.msgs, msg)
	s.mu.Unlock()
	return nil
}

// List returns all messages for the given botName, ordered oldest-first.
// If botName is empty, all messages are returned.
// Always returns a non-nil slice.
func (s *InMemoryChatStore) List(_ context.Context, botName string) ([]domain.ChatMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]domain.ChatMessage, 0)
	for _, m := range s.msgs {
		if botName != "" && m.BotName != botName {
			continue
		}
		result = append(result, m)
	}
	return result, nil
}

// ListAll returns all messages sorted newest-first (by CreatedAt descending).
// Always returns a non-nil slice.
func (s *InMemoryChatStore) ListAll(_ context.Context) ([]domain.ChatMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]domain.ChatMessage, len(s.msgs))
	copy(result, s.msgs)

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}
