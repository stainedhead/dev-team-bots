package mocks

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ErrAgentNotificationNotFound is returned when the requested notification does not exist.
var ErrAgentNotificationNotFound = errors.New("mocks: agent notification not found")

// InMemoryAgentNotificationStore is a hand-written test double for
// domain.AgentNotificationStore backed by an in-memory map.
// It implements the full interface and applies filter logic so tests can
// exercise query behaviour without a real database.
type InMemoryAgentNotificationStore struct {
	mu            sync.RWMutex
	notifications map[string]domain.AgentNotification

	// SaveCalls records every Save invocation for assertion in tests.
	SaveCalls []domain.AgentNotification
}

// NewInMemoryAgentNotificationStore returns an initialised store.
func NewInMemoryAgentNotificationStore() *InMemoryAgentNotificationStore {
	return &InMemoryAgentNotificationStore{
		notifications: make(map[string]domain.AgentNotification),
	}
}

// Save stores or replaces a notification by ID.
func (s *InMemoryAgentNotificationStore) Save(_ context.Context, n domain.AgentNotification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifications[n.ID] = n
	s.SaveCalls = append(s.SaveCalls, n)
	return nil
}

// Get returns the notification with the given ID or ErrAgentNotificationNotFound.
func (s *InMemoryAgentNotificationStore) Get(_ context.Context, id string) (domain.AgentNotification, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.notifications[id]
	if !ok {
		return domain.AgentNotification{}, ErrAgentNotificationNotFound
	}
	return n, nil
}

// List returns all notifications matching the filter.
// All non-empty filter fields are ANDed together. Status="" and BotName="" mean
// "match any". Search is a case-sensitive substring match on Message.
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

// AppendDiscuss adds entry to the discuss thread of the notification identified by id.
func (s *InMemoryAgentNotificationStore) AppendDiscuss(_ context.Context, id string, entry domain.DiscussEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notifications[id]
	if !ok {
		return ErrAgentNotificationNotFound
	}
	n.DiscussThread = append(n.DiscussThread, entry)
	s.notifications[id] = n
	return nil
}

// MarkActioned sets the notification status to "actioned" and records ActionedAt.
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
	return nil
}

// Delete removes all notifications whose IDs appear in ids.
// IDs that do not exist are silently ignored.
func (s *InMemoryAgentNotificationStore) Delete(_ context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		delete(s.notifications, id)
	}
	return nil
}
