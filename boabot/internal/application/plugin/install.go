// Package plugin contains application use cases for the plugin system.
package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// InstallUseCase orchestrates plugin installation from a registry.
type InstallUseCase struct {
	store    domain.PluginStore
	registry domain.RegistryManager
}

// NewInstallUseCase creates an InstallUseCase.
func NewInstallUseCase(store domain.PluginStore, registry domain.RegistryManager) *InstallUseCase {
	return &InstallUseCase{store: store, registry: registry}
}

// Install fetches manifest and archive from the named registry, verifies checksums via
// the store, and emits an audit log line.
func (uc *InstallUseCase) Install(ctx context.Context, registryName, name, version, actor string) (domain.Plugin, error) {
	// Look up the registry.
	regs, err := uc.registry.List(ctx)
	if err != nil {
		return domain.Plugin{}, fmt.Errorf("plugin install: list registries: %w", err)
	}
	var reg domain.PluginRegistry
	found := false
	for _, r := range regs {
		if r.Name == registryName {
			reg = r
			found = true
			break
		}
	}
	if !found {
		return domain.Plugin{}, fmt.Errorf("plugin install: registry %q not found", registryName)
	}

	// Fetch the registry index to find the plugin entry.
	idx, err := uc.registry.FetchIndex(ctx, reg.URL, false)
	if err != nil {
		return domain.Plugin{}, fmt.Errorf("plugin install: fetch index: %w", err)
	}

	var entry domain.RegistryEntry
	entryFound := false
	for _, e := range idx.Plugins {
		if e.Name == name {
			entry = e
			entryFound = true
			break
		}
	}
	if !entryFound {
		return domain.Plugin{}, fmt.Errorf("plugin install: plugin %q not found in registry %q", name, registryName)
	}

	// Resolve manifest and archive URLs.
	// When version is empty or matches the index's latest version, use the
	// pre-computed URLs from the index entry (which are already resolved to raw
	// content URLs for GitHub registries). For other versions, construct URLs
	// relative to the registry.
	var manifestURL, archiveURL string
	if version == "" || version == entry.LatestVersion {
		version = entry.LatestVersion
		manifestURL = entry.ManifestURL
		archiveURL = entry.DownloadURL
	} else {
		versionFound := false
		for _, v := range entry.Versions {
			if v == version {
				versionFound = true
				break
			}
		}
		if !versionFound {
			return domain.Plugin{}, fmt.Errorf("plugin install: version %s not available in registry", version)
		}
		manifestURL = reg.URL + "/" + entry.Name + "/" + version + "/plugin.yaml"
		archiveURL = reg.URL + "/" + entry.Name + "/" + version + "/" + entry.Name + "-" + version + ".tar.gz"
	}

	if archiveURL == "" {
		return domain.Plugin{}, fmt.Errorf("plugin install: no download URL available for %q — this registry entry may not support archive-based installation", name)
	}

	// Fetch manifest.
	manifest, err := uc.registry.FetchManifest(ctx, manifestURL)
	if err != nil {
		return domain.Plugin{}, fmt.Errorf("plugin install: fetch manifest: %w", err)
	}

	// Fetch archive.
	archive, err := uc.registry.FetchArchive(ctx, archiveURL)
	if err != nil {
		return domain.Plugin{}, fmt.Errorf("plugin install: fetch archive: %w", err)
	}

	// Install via store (handles checksum verification, zip-slip, size limit).
	p, err := uc.store.Install(ctx, manifest, archive, registryName, reg.Trusted)
	if err != nil {
		slog.Info("plugin.install",
			"plugin_name", name,
			"version", version,
			"registry", registryName,
			"actor", actor,
			"status", "failed",
			"timestamp", time.Now().UTC(),
		)
		return domain.Plugin{}, err
	}

	slog.Info("plugin.install",
		"plugin_name", name,
		"version", p.Version,
		"registry", registryName,
		"actor", actor,
		"status", string(p.Status),
		"timestamp", time.Now().UTC(),
	)

	return p, nil
}
