package domain

import (
	"context"
	"errors"
	"time"
)

// ErrPluginNotFound is returned by PluginStore when a plugin ID does not exist.
var ErrPluginNotFound = errors.New("plugin not found")

// Plugin represents an installed plugin in the system.
type Plugin struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Description string         `json:"description"`
	Author      string         `json:"author"`
	Registry    string         `json:"registry,omitempty"`
	Status      PluginStatus   `json:"status"`
	InstalledAt time.Time      `json:"installed_at"`
	Manifest    PluginManifest `json:"manifest"`
}

// PluginStatus represents the lifecycle state of a plugin.
type PluginStatus string

const (
	PluginStatusDownloading     PluginStatus = "downloading"
	PluginStatusStaged          PluginStatus = "staged"
	PluginStatusActive          PluginStatus = "active"
	PluginStatusDisabled        PluginStatus = "disabled"
	PluginStatusUpdateAvailable PluginStatus = "update_available"
	PluginStatusRejected        PluginStatus = "rejected"
	PluginStatusChecksumFail    PluginStatus = "checksum_fail"
)

// PluginManifest is the parsed contents of a plugin's manifest file.
type PluginManifest struct {
	Name        string            `yaml:"name"        json:"name"`
	Version     string            `yaml:"version"     json:"version"`
	Description string            `yaml:"description" json:"description"`
	Author      string            `yaml:"author"      json:"author"`
	Homepage    string            `yaml:"homepage"    json:"homepage,omitempty"`
	License     string            `yaml:"license"     json:"license,omitempty"`
	Tags        []string          `yaml:"tags"        json:"tags,omitempty"`
	MinRuntime  string            `yaml:"min_runtime" json:"min_runtime,omitempty"`
	Provides    PluginProvides    `yaml:"provides"    json:"provides"`
	Permissions PluginPermissions `yaml:"permissions" json:"permissions"`
	Entrypoint  string            `yaml:"entrypoint"  json:"entrypoint"`
	Checksums   map[string]string `yaml:"checksums"   json:"checksums,omitempty"`
}

// PluginProvides lists what a plugin exposes.
type PluginProvides struct {
	Tools []MCPTool `yaml:"tools" json:"tools,omitempty"`
}

// PluginPermissions declares what a plugin is allowed to access.
type PluginPermissions struct {
	Network    []string `yaml:"network"    json:"network,omitempty"`
	EnvVars    []string `yaml:"env_vars"   json:"env_vars,omitempty"`
	Filesystem bool     `yaml:"filesystem" json:"filesystem"`
}

// PluginRegistry is a configured plugin registry source.
type PluginRegistry struct {
	Name    string `yaml:"name"    json:"name"`
	URL     string `yaml:"url"     json:"url"`
	Trusted bool   `yaml:"trusted" json:"trusted"`
}

// RegistryIndex is the top-level index returned by a registry server.
type RegistryIndex struct {
	Registry    string          `json:"registry"`
	GeneratedAt time.Time       `json:"generated_at"`
	Plugins     []RegistryEntry `json:"plugins"`
}

// RegistryEntry is one plugin entry in a registry index.
type RegistryEntry struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Author        string   `json:"author"`
	LatestVersion string   `json:"latest_version"`
	Tags          []string `json:"tags,omitempty"`
	Versions      []string `json:"versions"`
	ManifestURL   string   `json:"manifest_url"`
	DownloadURL   string   `json:"download_url"`
}

// InstallPluginRequest is the payload for POST /api/v1/plugins.
type InstallPluginRequest struct {
	Registry string `json:"registry"`
	Name     string `json:"name"`
	Version  string `json:"version,omitempty"`
}

// AddRegistryRequest is the payload for POST /api/v1/registries.
type AddRegistryRequest struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Trusted bool   `json:"trusted"`
}

// PluginStore manages the lifecycle of installed plugins.
type PluginStore interface {
	List(ctx context.Context) ([]Plugin, error)
	Get(ctx context.Context, id string) (Plugin, error)
	// Install atomically extracts the archive to install_dir, verifies checksums,
	// zip-slip, and size limits, then creates the plugin record.
	Install(ctx context.Context, manifest PluginManifest, archive []byte, registry string, trusted bool) (Plugin, error)
	Approve(ctx context.Context, id string) error
	Reject(ctx context.Context, id string) error
	Disable(ctx context.Context, id string) error
	Enable(ctx context.Context, id string) error
	Update(ctx context.Context, id string, manifest PluginManifest, archive []byte) error
	Reload(ctx context.Context, id string) error
	Remove(ctx context.Context, id string) error
}

// RegistryManager manages configured registries and fetches their indexes.
type RegistryManager interface {
	List(ctx context.Context) ([]PluginRegistry, error)
	Add(ctx context.Context, reg PluginRegistry) error
	Remove(ctx context.Context, name string) error
	// FetchIndex fetches the registry index, bypassing cache if force=true.
	FetchIndex(ctx context.Context, registryURL string, force bool) (RegistryIndex, error)
	// FetchManifest downloads and parses a plugin manifest from manifestURL.
	FetchManifest(ctx context.Context, manifestURL string) (PluginManifest, error)
	// FetchArchive downloads a plugin archive from downloadURL.
	FetchArchive(ctx context.Context, downloadURL string) ([]byte, error)
	// FetchClaudePlugin downloads a Claude Code plugin from its .claude-plugin/plugin.json
	// manifest URL, packs the skill files into a tar.gz archive, and returns a synthetic
	// PluginManifest with Checksums["sha256"] set from the packed archive.
	FetchClaudePlugin(ctx context.Context, manifestURL string) (PluginManifest, []byte, error)
}
