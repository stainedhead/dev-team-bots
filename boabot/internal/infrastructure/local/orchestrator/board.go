// Package orchestrator provides in-memory implementations of orchestrator-mode
// domain interfaces for the local single-binary runtime.
package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ErrWorkItemNotFound is returned when a work item ID does not exist in the store.
var ErrWorkItemNotFound = errors.New("orchestrator: work item not found")

// InMemoryBoardStore implements domain.BoardStore with an in-memory map.
type InMemoryBoardStore struct {
	mu    sync.RWMutex
	items map[string]domain.WorkItem
}

// NewInMemoryBoardStore creates a new InMemoryBoardStore.
func NewInMemoryBoardStore() *InMemoryBoardStore {
	return &InMemoryBoardStore{
		items: make(map[string]domain.WorkItem),
	}
}

// Create stores a new WorkItem with a generated ID and sets UpdatedAt.
func (s *InMemoryBoardStore) Create(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	id, err := newID()
	if err != nil {
		return domain.WorkItem{}, err
	}
	item.ID = id
	item.UpdatedAt = time.Now().UTC()

	s.mu.Lock()
	s.items[id] = item
	s.mu.Unlock()
	return item, nil
}

// Update replaces an existing WorkItem. Returns ErrWorkItemNotFound if the ID does not exist.
func (s *InMemoryBoardStore) Update(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.items[item.ID]; !ok {
		return domain.WorkItem{}, ErrWorkItemNotFound
	}
	s.items[item.ID] = item
	return item, nil
}

// Get returns the WorkItem with the given ID. Returns ErrWorkItemNotFound if absent.
func (s *InMemoryBoardStore) Get(_ context.Context, id string) (domain.WorkItem, error) {
	s.mu.RLock()
	item, ok := s.items[id]
	s.mu.RUnlock()

	if !ok {
		return domain.WorkItem{}, ErrWorkItemNotFound
	}
	return item, nil
}

// List returns all work items matching the filter. Always returns a non-nil slice.
func (s *InMemoryBoardStore) List(_ context.Context, filter domain.WorkItemFilter) ([]domain.WorkItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]domain.WorkItem, 0, len(s.items))
	for _, item := range s.items {
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if filter.AssignedTo != "" && item.AssignedTo != filter.AssignedTo {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

// newID generates a random 8-byte hex string for use as an item ID.
func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
