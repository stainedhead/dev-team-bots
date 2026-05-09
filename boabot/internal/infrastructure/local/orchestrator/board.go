// Package orchestrator provides in-memory implementations of orchestrator-mode
// domain interfaces for the local single-binary runtime.
package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ErrWorkItemNotFound is returned when a work item ID does not exist in the store.
var ErrWorkItemNotFound = errors.New("orchestrator: work item not found")

// InMemoryBoardStore implements domain.BoardStore with an in-memory map and
// optional file persistence.
type InMemoryBoardStore struct {
	mu               sync.RWMutex
	items            map[string]domain.WorkItem
	persistPath      string
	statusChangeHook func(oldStatus, newStatus domain.WorkItemStatus, item domain.WorkItem)
}

// SetStatusChangeHook registers a callback that is invoked whenever an
// Update call changes a work item's status. The hook is called synchronously
// inside the write lock.
func (s *InMemoryBoardStore) SetStatusChangeHook(fn func(domain.WorkItemStatus, domain.WorkItemStatus, domain.WorkItem)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusChangeHook = fn
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
// SortPosition is set to the number of existing items in the same status + 1
// so the new item lands at the bottom of its column.
func (s *InMemoryBoardStore) Create(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	id, err := newID()
	if err != nil {
		return domain.WorkItem{}, err
	}
	item.ID = id
	item.UpdatedAt = time.Now().UTC()

	s.mu.Lock()
	sameStatus := 0
	for _, existing := range s.items {
		if existing.Status == item.Status {
			sameStatus++
		}
	}
	item.SortPosition = sameStatus + 1
	s.items[id] = item
	s.persist()
	s.mu.Unlock()
	return item, nil
}

// Update replaces an existing WorkItem. Returns ErrWorkItemNotFound if the ID does not exist.
// If the item's Status differs from the stored value, the registered statusChangeHook is called.
func (s *InMemoryBoardStore) Update(_ context.Context, item domain.WorkItem) (domain.WorkItem, error) {
	s.mu.Lock()
	existing, ok := s.items[item.ID]
	if !ok {
		s.mu.Unlock()
		return domain.WorkItem{}, ErrWorkItemNotFound
	}
	oldStatus := existing.Status
	s.items[item.ID] = item
	s.persist()
	hook := s.statusChangeHook
	s.mu.Unlock()

	if hook != nil && oldStatus != item.Status {
		hook(oldStatus, item.Status, item)
	}
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
// Results are sorted by SortPosition ASC (zero-valued items sort after
// explicitly-positioned ones), with CreatedAt ASC as a secondary key.
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
	sort.Slice(result, func(i, j int) bool {
		pi, pj := result[i].SortPosition, result[j].SortPosition
		if pi == 0 {
			pi = math.MaxInt
		}
		if pj == 0 {
			pj = math.MaxInt
		}
		if pi != pj {
			return pi < pj
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

// Delete removes a WorkItem by ID. Returns ErrWorkItemNotFound if absent.
func (s *InMemoryBoardStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return ErrWorkItemNotFound
	}
	delete(s.items, id)
	s.persist()
	return nil
}

// Reorder sets the SortPosition of each item to its 1-based index in ids.
// Items whose ID does not appear in ids are left unchanged.
func (s *InMemoryBoardStore) Reorder(_ context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, id := range ids {
		if item, ok := s.items[id]; ok {
			item.SortPosition = i + 1
			s.items[id] = item
		}
	}
	s.persist()
	return nil
}

// newID generates a random 8-byte hex string for use as an ID.
func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
