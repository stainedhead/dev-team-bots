package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// ErrPluginNotFound is returned when a plugin ID is not found.
var ErrPluginNotFound = errors.New("plugin not found")

// ErrPluginAlreadyInstalled is returned when a plugin with that name is already
// in a non-terminal state.
var ErrPluginAlreadyInstalled = errors.New("plugin with that name already installed")

// LocalPluginStore is a filesystem-backed implementation of domain.PluginStore.
//
// Layout:
//
//	install_dir/
//	  <plugin-name>/
//	    plugin.yaml   (manifest — stored as JSON despite the .yaml extension)
//	    status.json   (Plugin struct minus Manifest)
//	    run.sh (or whatever the entrypoint is)
type LocalPluginStore struct {
	mu         sync.RWMutex
	installDir string
	index      map[string]domain.Plugin // keyed by ID
}

// pluginStatus is the on-disk format for status.json (Plugin minus Manifest).
type pluginStatus struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Version     string              `json:"version"`
	Description string              `json:"description"`
	Author      string              `json:"author"`
	Registry    string              `json:"registry,omitempty"`
	Status      domain.PluginStatus `json:"status"`
	InstalledAt time.Time           `json:"installed_at"`
}

// NewLocalPluginStore creates a LocalPluginStore backed by installDir.
func NewLocalPluginStore(installDir string) (*LocalPluginStore, error) {
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return nil, fmt.Errorf("plugin store: create install dir: %w", err)
	}
	s := &LocalPluginStore{
		installDir: installDir,
		index:      make(map[string]domain.Plugin),
	}
	s.loadFromDisk()
	return s, nil
}

func (s *LocalPluginStore) loadFromDisk() {
	entries, err := os.ReadDir(s.installDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		statusPath := filepath.Join(s.installDir, e.Name(), "status.json")
		data, err := os.ReadFile(statusPath)
		if err != nil {
			continue
		}
		var ps pluginStatus
		if err := json.Unmarshal(data, &ps); err != nil {
			continue
		}
		// Re-read manifest.
		manifest := s.readManifest(e.Name())
		p := domain.Plugin{
			ID:          ps.ID,
			Name:        ps.Name,
			Version:     ps.Version,
			Description: ps.Description,
			Author:      ps.Author,
			Registry:    ps.Registry,
			Status:      ps.Status,
			InstalledAt: ps.InstalledAt,
			Manifest:    manifest,
		}
		s.index[ps.ID] = p
	}
}

func (s *LocalPluginStore) readManifest(pluginName string) domain.PluginManifest {
	manifestPath := filepath.Join(s.installDir, pluginName, "plugin.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return domain.PluginManifest{}
	}
	var m domain.PluginManifest
	_ = json.Unmarshal(data, &m)
	return m
}

func (s *LocalPluginStore) saveStatus(p domain.Plugin) error {
	dir := filepath.Join(s.installDir, p.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("plugin store: ensure plugin dir: %w", err)
	}
	ps := pluginStatus{
		ID:          p.ID,
		Name:        p.Name,
		Version:     p.Version,
		Description: p.Description,
		Author:      p.Author,
		Registry:    p.Registry,
		Status:      p.Status,
		InstalledAt: p.InstalledAt,
	}
	data, err := json.Marshal(ps)
	if err != nil {
		return fmt.Errorf("plugin store: marshal status: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "status.json"), data, 0o644)
}

func (s *LocalPluginStore) saveManifest(p domain.Plugin) error {
	dir := filepath.Join(s.installDir, p.Name)
	data, err := json.Marshal(p.Manifest)
	if err != nil {
		return fmt.Errorf("plugin store: marshal manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "plugin.yaml"), data, 0o644)
}

// List returns all installed plugins sorted by InstalledAt descending.
func (s *LocalPluginStore) List(_ context.Context) ([]domain.Plugin, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]domain.Plugin, 0, len(s.index))
	for _, p := range s.index {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].InstalledAt.After(result[j].InstalledAt)
	})
	return result, nil
}

// Get returns a plugin by ID.
func (s *LocalPluginStore) Get(_ context.Context, id string) (domain.Plugin, error) {
	s.mu.RLock()
	p, ok := s.index[id]
	s.mu.RUnlock()
	if !ok {
		return domain.Plugin{}, ErrPluginNotFound
	}
	return p, nil
}

// Install extracts the archive, verifies checksums, and creates the plugin record.
func (s *LocalPluginStore) Install(_ context.Context, manifest domain.PluginManifest, archive []byte, registry string, trusted bool) (domain.Plugin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if a plugin with this name already exists in a non-terminal state.
	for _, existing := range s.index {
		if existing.Name == manifest.Name &&
			existing.Status != domain.PluginStatusRejected {
			return domain.Plugin{}, ErrPluginAlreadyInstalled
		}
	}

	id := uuid.New().String()

	// Extract archive using the plugin name as the directory name.
	// We need to extract to <installDir>/<plugin-name>.
	// The installer uses id as the subdir name, so we use the plugin name as id for extraction.
	destPath, err := Extract(s.installDir, manifest.Name, manifest, archive)
	if err != nil {
		return domain.Plugin{}, fmt.Errorf("plugin store: extract: %w", err)
	}
	_ = destPath

	status := domain.PluginStatusStaged
	if trusted {
		status = domain.PluginStatusActive
	}

	p := domain.Plugin{
		ID:          id,
		Name:        manifest.Name,
		Version:     manifest.Version,
		Description: manifest.Description,
		Author:      manifest.Author,
		Registry:    registry,
		Status:      status,
		InstalledAt: time.Now().UTC(),
		Manifest:    manifest,
	}

	if err := s.saveStatus(p); err != nil {
		return domain.Plugin{}, fmt.Errorf("plugin store: save status: %w", err)
	}
	if err := s.saveManifest(p); err != nil {
		return domain.Plugin{}, fmt.Errorf("plugin store: save manifest: %w", err)
	}

	s.index[id] = p
	return p, nil
}

// Approve transitions a staged plugin to active.
func (s *LocalPluginStore) Approve(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.index[id]
	if !ok {
		return ErrPluginNotFound
	}
	p.Status = domain.PluginStatusActive
	s.index[id] = p
	return s.saveStatus(p)
}

// Reject removes a staged plugin.
func (s *LocalPluginStore) Reject(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.index[id]
	if !ok {
		return ErrPluginNotFound
	}
	delete(s.index, id)
	_ = os.RemoveAll(filepath.Join(s.installDir, p.Name))
	return nil
}

// Disable transitions an active plugin to disabled.
func (s *LocalPluginStore) Disable(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.index[id]
	if !ok {
		return ErrPluginNotFound
	}
	p.Status = domain.PluginStatusDisabled
	s.index[id] = p
	return s.saveStatus(p)
}

// Enable transitions a disabled plugin back to active.
func (s *LocalPluginStore) Enable(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.index[id]
	if !ok {
		return ErrPluginNotFound
	}
	p.Status = domain.PluginStatusActive
	s.index[id] = p
	return s.saveStatus(p)
}

// Update re-extracts the archive with a new manifest.
func (s *LocalPluginStore) Update(_ context.Context, id string, manifest domain.PluginManifest, archive []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.index[id]
	if !ok {
		return ErrPluginNotFound
	}

	// Remove old dir, re-extract.
	_ = os.RemoveAll(filepath.Join(s.installDir, p.Name))

	_, err := Extract(s.installDir, manifest.Name, manifest, archive)
	if err != nil {
		return fmt.Errorf("plugin store: update extract: %w", err)
	}

	p.Version = manifest.Version
	p.Description = manifest.Description
	p.Author = manifest.Author
	p.Manifest = manifest
	p.Name = manifest.Name
	s.index[id] = p

	if err := s.saveStatus(p); err != nil {
		return fmt.Errorf("plugin store: update status: %w", err)
	}
	return s.saveManifest(p)
}

// Reload re-reads the manifest from disk and checks the entrypoint exists.
// If the entrypoint is missing, status is set to disabled and an error is returned.
func (s *LocalPluginStore) Reload(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.index[id]
	if !ok {
		return ErrPluginNotFound
	}

	// Re-read manifest.
	manifest := s.readManifest(p.Name)
	p.Manifest = manifest

	// Check entrypoint.
	entrypointPath := filepath.Join(s.installDir, p.Name, manifest.Entrypoint)
	if _, err := os.Stat(entrypointPath); os.IsNotExist(err) {
		// Set status to disabled.
		p.Status = domain.PluginStatusDisabled
		s.index[id] = p
		_ = s.saveStatus(p)
		return fmt.Errorf("plugin store: reload: entrypoint %q not found", manifest.Entrypoint)
	}

	s.index[id] = p
	return s.saveStatus(p)
}

// Remove deletes a plugin and its directory.
func (s *LocalPluginStore) Remove(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.index[id]
	if !ok {
		return ErrPluginNotFound
	}
	delete(s.index, id)
	_ = os.RemoveAll(filepath.Join(s.installDir, p.Name))
	return nil
}
