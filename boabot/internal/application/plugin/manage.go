package plugin

import (
	"context"
	"log/slog"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ManageUseCase handles plugin lifecycle management operations.
type ManageUseCase struct {
	store domain.PluginStore
}

// NewManageUseCase creates a ManageUseCase.
func NewManageUseCase(store domain.PluginStore) *ManageUseCase {
	return &ManageUseCase{store: store}
}

// List returns all installed plugins.
func (uc *ManageUseCase) List(ctx context.Context) ([]domain.Plugin, error) {
	return uc.store.List(ctx)
}

// Get returns a plugin by ID.
func (uc *ManageUseCase) Get(ctx context.Context, id string) (domain.Plugin, error) {
	return uc.store.Get(ctx, id)
}

// Approve transitions a staged plugin to active.
func (uc *ManageUseCase) Approve(ctx context.Context, id, actor string) error {
	slog.Info("plugin.approve", "plugin_id", id, "actor", actor, "timestamp", time.Now().UTC())
	return uc.store.Approve(ctx, id)
}

// Reject removes a staged plugin.
func (uc *ManageUseCase) Reject(ctx context.Context, id, actor string) error {
	slog.Info("plugin.reject", "plugin_id", id, "actor", actor, "timestamp", time.Now().UTC())
	return uc.store.Reject(ctx, id)
}

// Enable transitions a disabled plugin to active.
func (uc *ManageUseCase) Enable(ctx context.Context, id, actor string) error {
	slog.Info("plugin.enable", "plugin_id", id, "actor", actor, "timestamp", time.Now().UTC())
	return uc.store.Enable(ctx, id)
}

// Disable transitions an active plugin to disabled.
func (uc *ManageUseCase) Disable(ctx context.Context, id, actor string) error {
	slog.Info("plugin.disable", "plugin_id", id, "actor", actor, "timestamp", time.Now().UTC())
	return uc.store.Disable(ctx, id)
}

// Update updates a plugin with a new manifest and archive.
func (uc *ManageUseCase) Update(ctx context.Context, id string, manifest domain.PluginManifest, archive []byte, actor string) error {
	slog.Info("plugin.update", "plugin_id", id, "actor", actor, "timestamp", time.Now().UTC())
	return uc.store.Update(ctx, id, manifest, archive)
}

// Reload re-reads the plugin manifest from disk.
func (uc *ManageUseCase) Reload(ctx context.Context, id, actor string) error {
	slog.Info("plugin.reload", "plugin_id", id, "actor", actor, "timestamp", time.Now().UTC())
	return uc.store.Reload(ctx, id)
}

// Remove deletes a plugin.
func (uc *ManageUseCase) Remove(ctx context.Context, id, actor string) error {
	slog.Info("plugin.remove", "plugin_id", id, "actor", actor, "timestamp", time.Now().UTC())
	return uc.store.Remove(ctx, id)
}
