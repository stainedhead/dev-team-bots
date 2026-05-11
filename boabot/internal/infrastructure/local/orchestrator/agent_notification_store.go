package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ErrAgentNotificationNotFound is returned when the requested notification does not exist.
var ErrAgentNotificationNotFound = errors.New("orchestrator: agent notification not found")

// InMemoryAgentNotificationStore implements domain.AgentNotificationStore with an
// in-memory map and optional file persistence.
type InMemoryAgentNotificationStore struct {
	mu            sync.RWMutex
	notifications map[string]domain.AgentNotification
	persistPath   string
}

// NewInMemoryAgentNotificationStore creates a new InMemoryAgentNotificationStore.
// If persistPath is non-empty, existing data is loaded from that file and every
// mutation is written back atomically.
func NewInMemoryAgentNotificationStore(persistPath string) *InMemoryAgentNotificationStore {
	s := &InMemoryAgentNotificationStore{
		notifications: make(map[string]domain.AgentNotification),
		persistPath:   persistPath,
	}
	if persistPath != "" {
		s.loadFromDisk()
	}
	return s
}

func (s *InMemoryAgentNotificationStore) loadFromDisk() {
	data, err := os.ReadFile(s.persistPath)
	if err != nil {
		return
	}
	var notifications []domain.AgentNotification
	if err := json.Unmarshal(data, &notifications); err != nil {
		return
	}
	for _, n := range notifications {
		s.notifications[n.ID] = n
	}
}

func (s *InMemoryAgentNotificationStore) persist() {
	if s.persistPath == "" {
		return
	}
	notifications := make([]domain.AgentNotification, 0, len(s.notifications))
	for _, n := range s.notifications {
		notifications = append(notifications, n)
	}
	data, err := json.Marshal(notifications)
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

// Save stores or replaces a notification. If the notification has no ID, a UUID
// is generated. If CreatedAt is zero, it is set to the current UTC time.
func (s *InMemoryAgentNotificationStore) Save(_ context.Context, n domain.AgentNotification) error {
	if n.ID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		n.ID = id
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}

	s.mu.Lock()
	s.notifications[n.ID] = n
	s.persist()
	s.mu.Unlock()
	return nil
}

// Get returns a copy of the notification with the given ID.
// Returns ErrAgentNotificationNotFound if the ID does not exist.
func (s *InMemoryAgentNotificationStore) Get(_ context.Context, id string) (domain.AgentNotification, error) {
	s.mu.RLock()
	n, ok := s.notifications[id]
	s.mu.RUnlock()

	if !ok {
		return domain.AgentNotification{}, ErrAgentNotificationNotFound
	}
	return n, nil
}

// List returns all notifications matching the filter, sorted newest-first.
// Empty filter fields are treated as "no constraint". BotName is an exact match,
// Status is an exact match, and Search is a case-sensitive substring match on Message.
func (s *InMemoryAgentNotificationStore) List(_ context.Context, filter domain.AgentNotificationFilter) ([]domain.AgentNotification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]domain.AgentNotification, 0)
	for _, n := range s.notifications {
		if filter.BotName != "" && n.BotName != filter.BotName {
			continue
		}
		if filter.Status != "" && n.Status != filter.Status {
			continue
		}
		if filter.Search != "" && !strings.Contains(n.Message, filter.Search) {
			continue
		}
		result = append(result, n)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

// UnreadCount returns the number of notifications with status "unread".
func (s *InMemoryAgentNotificationStore) UnreadCount(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, n := range s.notifications {
		if n.Status == domain.AgentNotificationStatusUnread {
			count++
		}
	}
	return count, nil
}

// AppendDiscuss appends a DiscussEntry to the notification's thread and persists.
// Returns ErrAgentNotificationNotFound if the ID does not exist.
// Note: the 100-entry cap is enforced by NotificationService, not this store.
func (s *InMemoryAgentNotificationStore) AppendDiscuss(_ context.Context, id string, entry domain.DiscussEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	n, ok := s.notifications[id]
	if !ok {
		return ErrAgentNotificationNotFound
	}
	n.DiscussThread = append(n.DiscussThread, entry)
	s.notifications[id] = n
	s.persist()
	return nil
}

// MarkActioned sets the notification's status to "actioned" and records ActionedAt.
// Returns ErrAgentNotificationNotFound if the ID does not exist.
func (s *InMemoryAgentNotificationStore) MarkActioned(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	n, ok := s.notifications[id]
	if !ok {
		return ErrAgentNotificationNotFound
	}
	n.Status = domain.AgentNotificationStatusActioned
	now := time.Now().UTC()
	n.ActionedAt = &now
	s.notifications[id] = n
	s.persist()
	return nil
}

// Delete removes all notifications whose IDs appear in ids and persists.
// IDs that do not exist are silently ignored.
func (s *InMemoryAgentNotificationStore) Delete(_ context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range ids {
		delete(s.notifications, id)
	}
	s.persist()
	return nil
}
