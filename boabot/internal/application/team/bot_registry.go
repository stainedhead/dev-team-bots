// Package team provides TeamManager and BotRegistry for starting and managing
// all enabled bots in-process without any cloud infrastructure.
package team

import (
	"sync"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// BotRegistry tracks the identity of every bot that has been started by the
// TeamManager.  It is safe for concurrent use.
type BotRegistry struct {
	mu   sync.RWMutex
	bots map[string]domain.BotIdentity
}

// NewBotRegistry constructs an empty BotRegistry.
func NewBotRegistry() *BotRegistry {
	return &BotRegistry{
		bots: make(map[string]domain.BotIdentity),
	}
}

// Register adds or overwrites the identity for identity.Name.
func (r *BotRegistry) Register(identity domain.BotIdentity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bots[identity.Name] = identity
}

// Get returns the BotIdentity for name, and false if it is not registered.
func (r *BotRegistry) Get(name string) (domain.BotIdentity, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.bots[name]
	return id, ok
}

// List returns a snapshot of all registered identities in an unspecified order.
func (r *BotRegistry) List() []domain.BotIdentity {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.BotIdentity, 0, len(r.bots))
	for _, id := range r.bots {
		out = append(out, id)
	}
	return out
}
