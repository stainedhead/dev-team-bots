// Package orchestrator provides in-memory implementations of orchestrator-mode
// domain interfaces for the local single-binary runtime.
package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ErrWorkItemNotFound is returned when a work item ID does not exist in the store.
var ErrWorkItemNotFound = errors.New("orchestrator: work item not found")

// InMemoryBoardStore implements domain.BoardStore with an in-memory map and
// optional file persistence.
type InMemoryBoardStore struct {
	mu          sync.RWMutex
	items       map[string]domain.WorkItem
	persistPath string
}

// NewInMemoryBoardStore creates a new InMemoryBoardStore.
// If persistPath is non-empty, existing data is loaded from that file and every
// mutation is written back atomically.
func NewInMemoryBoardStore(persistPath string) *InMemoryBoardStore {
	s := &InMemoryBoardStore{
		items:       make(map[string]domain.WorkItem),
		persistPath: persistPath,
	}
	if persistPath != "" {
		s.loadFromDisk()
	}
	return s
}

func (s *InMemoryBoardStore) loadFromDisk() {
	data, err := os.ReadFile(s.persistPath)
	if err != nil {
		return
	}
	var items []domain.WorkItem
	if err := json.Unmarshal(data, &items); err != nil {
		return
	}
	for _, it := range items {
		s.items[it.ID] = it
	}
}

func (s *InMemoryBoardStore) persist() {
	if s.persistPath == "" {
		return
	}
	items := make([]domain.WorkItem, 0, len(s.items))
	for _, it := range s.items {
		items = append(items, it)
	}
	data, err := json.Marshal(items)
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
	s.persist()
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
	s.persist()
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
		if filter.ActiveTaskID != "" && item.ActiveTaskID != filter.ActiveTaskID {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

// newID generates a random 8-byte hex string for use as an ID.
func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
