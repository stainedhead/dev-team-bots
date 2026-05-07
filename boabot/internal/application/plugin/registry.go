package plugin

import (
	"context"
	"fmt"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// RegistryUseCase handles plugin registry management.
type RegistryUseCase struct {
	mgr domain.RegistryManager
}

// NewRegistryUseCase creates a RegistryUseCase.
func NewRegistryUseCase(mgr domain.RegistryManager) *RegistryUseCase {
	return &RegistryUseCase{mgr: mgr}
}

// List returns all configured registries.
func (uc *RegistryUseCase) List(ctx context.Context) ([]domain.PluginRegistry, error) {
	return uc.mgr.List(ctx)
}

// Add adds a new registry.
func (uc *RegistryUseCase) Add(ctx context.Context, reg domain.PluginRegistry) error {
	return uc.mgr.Add(ctx, reg)
}

// Remove removes a registry by name.
func (uc *RegistryUseCase) Remove(ctx context.Context, name string) error {
	return uc.mgr.Remove(ctx, name)
}

// FetchIndex looks up the registry by name then fetches its index.
func (uc *RegistryUseCase) FetchIndex(ctx context.Context, name string, force bool) (domain.RegistryIndex, error) {
	regs, err := uc.mgr.List(ctx)
	if err != nil {
		return domain.RegistryIndex{}, fmt.Errorf("registry use case: list registries: %w", err)
	}
	for _, r := range regs {
		if r.Name == name {
			return uc.mgr.FetchIndex(ctx, r.URL, force)
		}
	}
	return domain.RegistryIndex{}, fmt.Errorf("registry use case: registry %q not found", name)
}
