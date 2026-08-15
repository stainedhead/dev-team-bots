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
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/filelock"
)

// persistLockTimeout bounds how long persist() waits to acquire the
// cross-process file lock (persistPath+".lock") before giving up. Chosen
// generously relative to the sub-millisecond cost of a JSON marshal and a
// small file write, so it is only ever reached if a lock holder has
// genuinely wedged -- not a value real concurrent writers are expected to
// approach.
const persistLockTimeout = 5 * time.Second

// persistAfterLockHook, when non-nil, is invoked by persist() immediately
// after it acquires the cross-process file lock, before re-reading
// persistPath from disk or writing -- a test seam only (see
// board_race_test.go), letting tests deterministically force a second,
// concurrent persist() call from a different InMemoryBoardStore instance
// onto filelock.AcquireWait's retry/backoff wait path rather than relying
// on scheduler luck. Nil (the production default) means no delay; must
// never be set outside a test.
var persistAfterLockHook func()

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
	for id, it := range readDiskItems(s.persistPath) {
		s.items[id] = it
	}
}

// readDiskItems reads and decodes path's current on-disk board state,
// keyed by item ID. A missing file or malformed content is treated as "no
// items" (mirrors the previous loadFromDisk's silent-skip behavior), not
// an error -- callers persisting for the first time, or racing a
// concurrent writer's own in-progress first write, must not fail.
func readDiskItems(path string) map[string]domain.WorkItem {
	items := make(map[string]domain.WorkItem)
	data, err := os.ReadFile(path)
	if err != nil {
		return items
	}
	var decoded []domain.WorkItem
	if err := json.Unmarshal(data, &decoded); err != nil {
		return items
	}
	for _, it := range decoded {
		items[it.ID] = it
	}
	return items
}

// writeItemsAtomically marshals items and atomically publishes them under
// path via the same same-directory-temp-file-then-rename sequence the
// original persist() used. Errors are swallowed (best-effort persistence,
// matching the pre-fix behavior) rather than propagated, since BoardStore's
// domain interface has no error return for persistence failures.
func writeItemsAtomically(path string, items []domain.WorkItem) {
	data, err := json.Marshal(items)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// persist writes this process's just-made mutation to persistPath so it
// survives alongside any other process's concurrently-persisted state
// (FR-1/T-FR1): it acquires a cross-process file lock at
// persistPath+".lock" (waiting its turn rather than failing fast, since
// many legitimate concurrent writers -- e.g. buzz-acp's documented
// multi-process agent pool, ADR-B026 -- are expected), re-reads the
// current on-disk state immediately before writing, and merges it with
// this call's own touched item(s) by ID: upsert sets/overwrites those IDs,
// deleteIDs removes them, and every other on-disk item (written by another
// process) passes through untouched. This stops one process's write from
// silently clobbering items it never touched -- it does not solve full
// concurrent-edit-of-the-same-item semantics (see Reorder's doc comment
// for the one known, accepted gap in that regard).
func (s *InMemoryBoardStore) persist(upsert []domain.WorkItem, deleteIDs []string) {
	if s.persistPath == "" {
		return
	}

	lockPath := s.persistPath + ".lock"
	lock, err := filelock.AcquireWait(lockPath, persistLockTimeout)
	if err != nil {
		// Best-effort persistence: if the cross-process lock cannot be
		// acquired within persistLockTimeout (e.g. a wedged holder), skip
		// this write rather than falling back to an unsafe full overwrite
		// that would reintroduce the very clobbering hazard this fix
		// exists to close. Unlike the pre-fix full-overwrite persist(),
		// this write is NOT automatically retried by a later call: since
		// each persist() now only writes its own targeted upsert/delete
		// (not the whole in-memory state), an item dropped here reaches
		// disk only if it is mutated again (Create/Update/Delete/Reorder)
		// and that later call's persist() succeeds. See
		// implementation-notes.md's Edge Cases section.
		return
	}
	defer func() { _ = lock.Release() }()

	if persistAfterLockHook != nil {
		persistAfterLockHook()
	}

	merged := readDiskItems(s.persistPath)
	for _, it := range upsert {
		merged[it.ID] = it
	}
	for _, id := range deleteIDs {
		delete(merged, id)
	}

	items := make([]domain.WorkItem, 0, len(merged))
	for _, it := range merged {
		items = append(items, it)
	}
	writeItemsAtomically(s.persistPath, items)
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
	s.persist([]domain.WorkItem{item}, nil)
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
	s.persist([]domain.WorkItem{item}, nil)
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
	s.persist(nil, []string{id})
	return nil
}

// Reorder sets the SortPosition of each item to its 1-based index in ids.
// Items whose ID does not appear in ids are left unchanged.
//
// Known, accepted limitation (FR-1/T-FR1's scope): this stops one
// process's Reorder from clobbering items *other* processes touched, but
// it does not solve the true concurrent-conflict case of two processes
// reordering the *same* column at the same time -- a naive per-item merge
// by ID can still produce colliding or gapped SortPosition values in that
// scenario. Solving full concurrent-edit-of-the-same-item semantics is out
// of scope for this fix.
func (s *InMemoryBoardStore) Reorder(_ context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	touched := make([]domain.WorkItem, 0, len(ids))
	for i, id := range ids {
		if item, ok := s.items[id]; ok {
			item.SortPosition = i + 1
			s.items[id] = item
			touched = append(touched, item)
		}
	}
	s.persist(touched, nil)
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
