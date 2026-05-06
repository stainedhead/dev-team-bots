package orchestrator

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ErrDirectTaskNotFound is returned when a task ID does not exist in the store.
var ErrDirectTaskNotFound = errors.New("orchestrator: direct task not found")

// InMemoryDirectTaskStore implements domain.DirectTaskStore with an in-memory map.
type InMemoryDirectTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]domain.DirectTask
}

// NewInMemoryDirectTaskStore creates a new InMemoryDirectTaskStore.
func NewInMemoryDirectTaskStore() *InMemoryDirectTaskStore {
	return &InMemoryDirectTaskStore{
		tasks: make(map[string]domain.DirectTask),
	}
}

// Create stores a new DirectTask with a generated ID and sets CreatedAt/UpdatedAt.
func (s *InMemoryDirectTaskStore) Create(_ context.Context, task domain.DirectTask) (domain.DirectTask, error) {
	id, err := newID()
	if err != nil {
		return domain.DirectTask{}, err
	}
	now := time.Now().UTC()
	task.ID = id
	task.CreatedAt = now
	task.UpdatedAt = now

	s.mu.Lock()
	s.tasks[id] = task
	s.mu.Unlock()
	return task, nil
}

// Update replaces an existing DirectTask. Returns ErrDirectTaskNotFound if the ID does not exist.
func (s *InMemoryDirectTaskStore) Update(_ context.Context, task domain.DirectTask) (domain.DirectTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[task.ID]; !ok {
		return domain.DirectTask{}, ErrDirectTaskNotFound
	}
	task.UpdatedAt = time.Now().UTC()
	s.tasks[task.ID] = task
	return task, nil
}

// Get returns the DirectTask with the given ID. Returns ErrDirectTaskNotFound if absent.
func (s *InMemoryDirectTaskStore) Get(_ context.Context, id string) (domain.DirectTask, error) {
	s.mu.RLock()
	task, ok := s.tasks[id]
	s.mu.RUnlock()

	if !ok {
		return domain.DirectTask{}, ErrDirectTaskNotFound
	}
	return task, nil
}

// List returns all tasks for the given botName. If botName is empty, all tasks are returned.
// Always returns a non-nil slice.
func (s *InMemoryDirectTaskStore) List(_ context.Context, botName string) ([]domain.DirectTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]domain.DirectTask, 0)
	for _, task := range s.tasks {
		if botName != "" && task.BotName != botName {
			continue
		}
		result = append(result, task)
	}
	return result, nil
}

// ListAll returns all tasks sorted newest first (by CreatedAt descending).
// Always returns a non-nil slice.
func (s *InMemoryDirectTaskStore) ListAll(_ context.Context) ([]domain.DirectTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]domain.DirectTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		result = append(result, task)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}
