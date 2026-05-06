package orchestrator

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ErrBotNotFound is returned when a bot name does not exist in the control plane.
var ErrBotNotFound = errors.New("orchestrator: bot not found")

// InMemoryControlPlane implements domain.ControlPlane with an in-memory map.
type InMemoryControlPlane struct {
	mu   sync.RWMutex
	bots map[string]domain.BotEntry
}

// NewInMemoryControlPlane creates a new InMemoryControlPlane.
func NewInMemoryControlPlane() *InMemoryControlPlane {
	return &InMemoryControlPlane{
		bots: make(map[string]domain.BotEntry),
	}
}

// Register adds or replaces a bot entry, setting Status=active and RegisteredAt.
func (cp *InMemoryControlPlane) Register(_ context.Context, entry domain.BotEntry) error {
	entry.Status = domain.BotStatusActive
	entry.RegisteredAt = time.Now().UTC()

	cp.mu.Lock()
	cp.bots[entry.Name] = entry
	cp.mu.Unlock()
	return nil
}

// Deregister marks a bot as inactive without deleting it.
// Returns ErrBotNotFound if the bot does not exist.
func (cp *InMemoryControlPlane) Deregister(_ context.Context, name string) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	entry, ok := cp.bots[name]
	if !ok {
		return ErrBotNotFound
	}
	entry.Status = domain.BotStatusInactive
	cp.bots[name] = entry
	return nil
}

// UpdateHeartbeat sets LastHeartbeat to now. Returns ErrBotNotFound if the bot
// does not exist.
func (cp *InMemoryControlPlane) UpdateHeartbeat(_ context.Context, name string) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	entry, ok := cp.bots[name]
	if !ok {
		return ErrBotNotFound
	}
	entry.LastHeartbeat = time.Now().UTC()
	cp.bots[name] = entry
	return nil
}

// Get returns the BotEntry for the named bot. Returns ErrBotNotFound if absent.
func (cp *InMemoryControlPlane) Get(_ context.Context, name string) (domain.BotEntry, error) {
	cp.mu.RLock()
	entry, ok := cp.bots[name]
	cp.mu.RUnlock()

	if !ok {
		return domain.BotEntry{}, ErrBotNotFound
	}
	return entry, nil
}

// List returns all bot entries. Always returns a non-nil slice.
func (cp *InMemoryControlPlane) List(_ context.Context) ([]domain.BotEntry, error) {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	result := make([]domain.BotEntry, 0, len(cp.bots))
	for _, e := range cp.bots {
		result = append(result, e)
	}
	return result, nil
}

// IsTypeActive returns true if any bot with the given botType has Status=active.
func (cp *InMemoryControlPlane) IsTypeActive(_ context.Context, botType string) (bool, error) {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	for _, e := range cp.bots {
		if e.BotType == botType && e.Status == domain.BotStatusActive {
			return true, nil
		}
	}
	return false, nil
}
