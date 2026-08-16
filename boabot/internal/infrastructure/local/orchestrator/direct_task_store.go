package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/filelock"
)

// ErrDirectTaskNotFound is returned when a task ID does not exist in the store.
var ErrDirectTaskNotFound = errors.New("orchestrator: direct task not found")

// InMemoryDirectTaskStore implements domain.DirectTaskStore with an in-memory
// map and optional file persistence.
type InMemoryDirectTaskStore struct {
	mu          sync.RWMutex
	tasks       map[string]domain.DirectTask
	persistPath string
}

// NewInMemoryDirectTaskStore creates a new InMemoryDirectTaskStore.
// If persistPath is non-empty, existing data is loaded from that file and every
// mutation is written back atomically.
func NewInMemoryDirectTaskStore(persistPath string) *InMemoryDirectTaskStore {
	s := &InMemoryDirectTaskStore{
		tasks:       make(map[string]domain.DirectTask),
		persistPath: persistPath,
	}
	if persistPath != "" {
		s.loadFromDisk()
	}
	return s
}

func (s *InMemoryDirectTaskStore) loadFromDisk() {
	data, err := os.ReadFile(s.persistPath)
	if err != nil {
		return
	}
	var tasks []domain.DirectTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		return
	}
	for _, t := range tasks {
		s.tasks[t.ID] = t
	}
}

// readDiskTasks reads and decodes path's current on-disk task state, keyed
// by task ID. A missing file or malformed content is treated as "no tasks",
// not an error -- callers persisting for the first time, or racing a
// concurrent writer's own in-progress first write, must not fail.
func readDiskTasks(path string) map[string]domain.DirectTask {
	tasks := make(map[string]domain.DirectTask)
	data, err := os.ReadFile(path)
	if err != nil {
		return tasks
	}
	var decoded []domain.DirectTask
	if err := json.Unmarshal(data, &decoded); err != nil {
		return tasks
	}
	for _, t := range decoded {
		tasks[t.ID] = t
	}
	return tasks
}

// writeTasksAtomically marshals tasks and atomically publishes them under
// path via a same-directory-temp-file-then-rename sequence.
func writeTasksAtomically(path string, tasks []domain.DirectTask) {
	data, err := json.Marshal(tasks)
	if err != nil {
		slog.Error("direct-task-store: marshal failed during persist", "err", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		slog.Error("direct-task-store: mkdir failed during persist", "path", filepath.Dir(path), "err", err)
		return
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Error("direct-task-store: write failed during persist", "path", tmp, "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		slog.Error("direct-task-store: rename failed during persist", "from", tmp, "to", path, "err", err)
	}
}

// persist writes this process's just-made mutation to persistPath so it
// survives alongside any other process's concurrently-persisted state,
// mirroring InMemoryBoardStore.persist()'s fix (board.go): it acquires a
// cross-process file lock at persistPath+".lock" (waiting its turn rather
// than failing fast), re-reads the current on-disk state immediately before
// writing, and merges it with this call's own touched task(s) by ID --
// upsert sets/overwrites those IDs, every other on-disk task (written by
// another process, e.g. native mode and an ACP-mode process sharing
// tasks.json under FR-502's shared-state root) passes through untouched.
func (s *InMemoryDirectTaskStore) persist(upsert []domain.DirectTask, deleteIDs []string) {
	if s.persistPath == "" {
		return
	}

	lockPath := s.persistPath + ".lock"
	lock, err := filelock.AcquireWait(lockPath, persistLockTimeout)
	if err != nil {
		slog.Error("direct-task-store: lock acquisition failed during persist", "path", lockPath, "err", err)
		return
	}
	defer func() { _ = lock.Release() }()

	merged := readDiskTasks(s.persistPath)
	for _, t := range upsert {
		merged[t.ID] = t
	}
	for _, id := range deleteIDs {
		delete(merged, id)
	}

	tasks := make([]domain.DirectTask, 0, len(merged))
	for _, t := range merged {
		tasks = append(tasks, t)
	}
	writeTasksAtomically(s.persistPath, tasks)
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
	s.persist([]domain.DirectTask{task}, nil)
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
	s.persist([]domain.DirectTask{task}, nil)
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

// List returns all tasks for the given botName, newest-first.
// If botName is empty, all tasks are returned.
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
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

// ListAll returns all tasks sorted newest-first. Always returns a non-nil slice.
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

// Delete removes the task with the given ID. Returns ErrDirectTaskNotFound if absent.
func (s *InMemoryDirectTaskStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[id]; !ok {
		return ErrDirectTaskNotFound
	}
	delete(s.tasks, id)
	s.persist(nil, []string{id})
	return nil
}

// ListBySource returns all tasks with the given source, sorted newest-first.
func (s *InMemoryDirectTaskStore) ListBySource(_ context.Context, source domain.DirectTaskSource) ([]domain.DirectTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]domain.DirectTask, 0)
	for _, task := range s.tasks {
		if task.Source == source {
			result = append(result, task)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

// ListDue returns all pending tasks whose NextRunAt is non-nil and <= now.
func (s *InMemoryDirectTaskStore) ListDue(_ context.Context, now time.Time) ([]domain.DirectTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]domain.DirectTask, 0)
	for _, task := range s.tasks {
		if task.Status != domain.DirectTaskStatusPending {
			continue
		}
		if task.NextRunAt == nil || task.NextRunAt.After(now) {
			continue
		}
		result = append(result, task)
	}
	return result, nil
}

// ClaimDue atomically transitions the given task from pending to dispatching.
// Returns true if the claim succeeded (task was pending). Returns false, nil
// if the task exists but is not in pending status.
func (s *InMemoryDirectTaskStore) ClaimDue(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		return false, ErrDirectTaskNotFound
	}
	if task.Status != domain.DirectTaskStatusPending {
		return false, nil
	}
	task.Status = domain.DirectTaskStatusDispatching
	task.UpdatedAt = time.Now().UTC()
	s.tasks[id] = task
	s.persist([]domain.DirectTask{task}, nil)
	return true, nil
}
