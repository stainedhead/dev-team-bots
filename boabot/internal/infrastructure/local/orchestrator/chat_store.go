package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// chatStoreState is the JSON-serialisable state persisted to disk.
type chatStoreState struct {
	Threads  []domain.ChatThread  `json:"threads"`
	Messages []domain.ChatMessage `json:"messages"`
}

// InMemoryChatStore implements domain.ChatStore with optional file persistence.
type InMemoryChatStore struct {
	mu          sync.RWMutex
	threads     map[string]domain.ChatThread
	messages    []domain.ChatMessage
	persistPath string
}

// NewInMemoryChatStore creates a new InMemoryChatStore.
// If persistPath is non-empty, the store loads existing data from that file and
// writes to it atomically on every mutation.
func NewInMemoryChatStore(persistPath string) *InMemoryChatStore {
	s := &InMemoryChatStore{
		threads:     make(map[string]domain.ChatThread),
		messages:    make([]domain.ChatMessage, 0),
		persistPath: persistPath,
	}
	if persistPath != "" {
		s.loadFromDisk()
	}
	return s
}

// loadFromDisk reads persisted state from disk, silently ignoring missing files.
func (s *InMemoryChatStore) loadFromDisk() {
	data, err := os.ReadFile(s.persistPath)
	if err != nil {
		return // file not yet created or unreadable
	}
	var state chatStoreState
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	for _, t := range state.Threads {
		s.threads[t.ID] = t
	}
	s.messages = state.Messages
	if s.messages == nil {
		s.messages = make([]domain.ChatMessage, 0)
	}
}

// persist writes current state to disk atomically (write to .tmp then rename).
// The caller must hold the write lock.
func (s *InMemoryChatStore) persist() {
	if s.persistPath == "" {
		return
	}
	threads := make([]domain.ChatThread, 0, len(s.threads))
	for _, t := range s.threads {
		threads = append(threads, t)
	}
	state := chatStoreState{
		Threads:  threads,
		Messages: s.messages,
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	tmp := s.persistPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.persistPath), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, s.persistPath)
}

// ── Thread lifecycle ──────────────────────────────────────────────────────────

// CreateThread creates a new named conversation thread.
func (s *InMemoryChatStore) CreateThread(_ context.Context, title string, participants []string) (domain.ChatThread, error) {
	id, err := newID()
	if err != nil {
		return domain.ChatThread{}, err
	}
	now := time.Now().UTC()
	t := domain.ChatThread{
		ID:           id,
		Title:        title,
		Participants: participants,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.mu.Lock()
	s.threads[id] = t
	s.persist()
	s.mu.Unlock()
	return t, nil
}

// ListThreads returns all threads sorted by UpdatedAt descending.
func (s *InMemoryChatStore) ListThreads(_ context.Context) ([]domain.ChatThread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]domain.ChatThread, 0, len(s.threads))
	for _, t := range s.threads {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

// DeleteThread removes a thread and all its messages.
func (s *InMemoryChatStore) DeleteThread(_ context.Context, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.threads, threadID)

	filtered := s.messages[:0]
	for _, m := range s.messages {
		if m.ThreadID != threadID {
			filtered = append(filtered, m)
		}
	}
	s.messages = filtered
	s.persist()
	return nil
}

// ── Messages ──────────────────────────────────────────────────────────────────

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
	s.messages = append(s.messages, msg)

	// Update the parent thread's UpdatedAt.
	if msg.ThreadID != "" {
		if t, ok := s.threads[msg.ThreadID]; ok {
			t.UpdatedAt = msg.CreatedAt
			s.threads[msg.ThreadID] = t
		}
	}

	s.persist()
	s.mu.Unlock()
	return nil
}

// List returns messages for the given threadID, newest-first.
// Always returns a non-nil slice.
func (s *InMemoryChatStore) List(_ context.Context, threadID string) ([]domain.ChatMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]domain.ChatMessage, 0)
	for _, m := range s.messages {
		if m.ThreadID == threadID {
			result = append(result, m)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

// ListAll returns all messages sorted newest-first (by CreatedAt descending).
// Always returns a non-nil slice.
func (s *InMemoryChatStore) ListAll(_ context.Context) ([]domain.ChatMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]domain.ChatMessage, len(s.messages))
	copy(result, s.messages)

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

// ListByBot returns all messages where BotName == botName, newest-first.
// Always returns a non-nil slice.
func (s *InMemoryChatStore) ListByBot(_ context.Context, botName string) ([]domain.ChatMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]domain.ChatMessage, 0)
	for _, m := range s.messages {
		if m.BotName == botName {
			result = append(result, m)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}
