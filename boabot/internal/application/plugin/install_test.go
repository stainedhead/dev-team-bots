package plugin_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/application/mocks"
	"github.com/stainedhead/dev-team-bots/boabot/internal/application/plugin"
	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func makeIndex(trusted bool) domain.RegistryIndex {
	return domain.RegistryIndex{
		Registry: "official",
		Plugins: []domain.RegistryEntry{
			{
				Name:          "my-tool",
				LatestVersion: "1.0.0",
				ManifestURL:   "https://example.com/my-tool/1.0.0/manifest.yaml",
				DownloadURL:   "https://example.com/my-tool/1.0.0/my-tool.tar.gz",
			},
		},
	}
}

func TestInstallUseCase_TrustedRegistry(t *testing.T) {
	reg := domain.PluginRegistry{Name: "official", URL: "https://example.com", Trusted: true}
	manifest := domain.PluginManifest{Name: "my-tool", Version: "1.0.0"}
	archive := []byte("fake archive")
	expectedPlugin := domain.Plugin{
		ID:     "abc123",
		Name:   "my-tool",
		Status: domain.PluginStatusActive,
	}

	registryMgr := &mocks.RegistryManager{
		ListFn: func(_ context.Context) ([]domain.PluginRegistry, error) {
			return []domain.PluginRegistry{reg}, nil
		},
		FetchIndexFn: func(_ context.Context, registryURL string, _ bool) (domain.RegistryIndex, error) {
			return makeIndex(true), nil
		},
		FetchManifestFn: func(_ context.Context, _ string) (domain.PluginManifest, error) {
			return manifest, nil
		},
		FetchArchiveFn: func(_ context.Context, _ string) ([]byte, error) {
			return archive, nil
		},
	}

	var installCalled bool
	var installTrusted bool
	pluginStore := &mocks.PluginStore{
		InstallFn: func(_ context.Context, m domain.PluginManifest, _ []byte, _ string, trusted bool) (domain.Plugin, error) {
			installCalled = true
			installTrusted = trusted
			return expectedPlugin, nil
		},
	}

	uc := plugin.NewInstallUseCase(pluginStore, registryMgr)
	p, err := uc.Install(context.Background(), "official", "my-tool", "", "admin")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !installCalled {
		t.Error("expected store.Install to be called")
	}
	if !installTrusted {
		t.Error("expected trusted=true for trusted registry")
	}
	if p.ID != expectedPlugin.ID {
		t.Errorf("got plugin ID %q, want %q", p.ID, expectedPlugin.ID)
	}
}

func TestInstallUseCase_UntrustedRegistry(t *testing.T) {
	reg := domain.PluginRegistry{Name: "community", URL: "https://community.example.com", Trusted: false}
	manifest := domain.PluginManifest{Name: "comm-tool", Version: "2.0.0"}
	archive := []byte("fake archive")
	expectedPlugin := domain.Plugin{
		ID:     "def456",
		Name:   "comm-tool",
		Status: domain.PluginStatusStaged,
	}

	registryMgr := &mocks.RegistryManager{
		ListFn: func(_ context.Context) ([]domain.PluginRegistry, error) {
			return []domain.PluginRegistry{reg}, nil
		},
		FetchIndexFn: func(_ context.Context, _ string, _ bool) (domain.RegistryIndex, error) {
			return domain.RegistryIndex{
				Registry: "community",
				Plugins: []domain.RegistryEntry{
					{
						Name:          "comm-tool",
						LatestVersion: "2.0.0",
						ManifestURL:   "https://community.example.com/manifest.yaml",
						DownloadURL:   "https://community.example.com/comm-tool.tar.gz",
					},
				},
			}, nil
		},
		FetchManifestFn: func(_ context.Context, _ string) (domain.PluginManifest, error) {
			return manifest, nil
		},
		FetchArchiveFn: func(_ context.Context, _ string) ([]byte, error) {
			return archive, nil
		},
	}

	var installTrusted bool
	pluginStore := &mocks.PluginStore{
		InstallFn: func(_ context.Context, _ domain.PluginManifest, _ []byte, _ string, trusted bool) (domain.Plugin, error) {
			installTrusted = trusted
			return expectedPlugin, nil
		},
	}

	uc := plugin.NewInstallUseCase(pluginStore, registryMgr)
	_, err := uc.Install(context.Background(), "community", "comm-tool", "", "user1")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if installTrusted {
		t.Error("expected trusted=false for untrusted registry")
	}
}

func TestInstallUseCase_RegistryNotFound(t *testing.T) {
	registryMgr := &mocks.RegistryManager{
		ListFn: func(_ context.Context) ([]domain.PluginRegistry, error) {
			return []domain.PluginRegistry{}, nil
		},
	}
	pluginStore := &mocks.PluginStore{}

	uc := plugin.NewInstallUseCase(pluginStore, registryMgr)
	_, err := uc.Install(context.Background(), "missing-registry", "tool", "", "admin")
	if err == nil {
		t.Fatal("expected error for unknown registry, got nil")
	}
}

func TestInstallUseCase_PluginNotInRegistry(t *testing.T) {
	reg := domain.PluginRegistry{Name: "official", URL: "https://example.com", Trusted: true}
	registryMgr := &mocks.RegistryManager{
		ListFn: func(_ context.Context) ([]domain.PluginRegistry, error) {
			return []domain.PluginRegistry{reg}, nil
		},
		FetchIndexFn: func(_ context.Context, _ string, _ bool) (domain.RegistryIndex, error) {
			return domain.RegistryIndex{
				Registry: "official",
				Plugins:  []domain.RegistryEntry{}, // empty
			}, nil
		},
	}
	pluginStore := &mocks.PluginStore{}

	uc := plugin.NewInstallUseCase(pluginStore, registryMgr)
	_, err := uc.Install(context.Background(), "official", "nonexistent", "", "admin")
	if err == nil {
		t.Fatal("expected error for plugin not in registry, got nil")
	}
}

func TestInstallUseCase_VersionPinned_UsesVersionURL(t *testing.T) {
	reg := domain.PluginRegistry{Name: "official", URL: "https://example.com", Trusted: true}
	manifest := domain.PluginManifest{Name: "my-tool", Version: "1.0.0"}
	archive := []byte("fake archive")

	var capturedManifestURL, capturedArchiveURL string

	registryMgr := &mocks.RegistryManager{
		ListFn: func(_ context.Context) ([]domain.PluginRegistry, error) {
			return []domain.PluginRegistry{reg}, nil
		},
		FetchIndexFn: func(_ context.Context, _ string, _ bool) (domain.RegistryIndex, error) {
			return domain.RegistryIndex{
				Registry: "official",
				Plugins: []domain.RegistryEntry{
					{
						Name:          "my-tool",
						LatestVersion: "1.2.0",
						Versions:      []string{"1.0.0", "1.2.0"},
						ManifestURL:   "https://example.com/my-tool/1.2.0/plugin.yaml",
						DownloadURL:   "https://example.com/my-tool/1.2.0/my-tool-1.2.0.tar.gz",
					},
				},
			}, nil
		},
		FetchManifestFn: func(_ context.Context, url string) (domain.PluginManifest, error) {
			capturedManifestURL = url
			return manifest, nil
		},
		FetchArchiveFn: func(_ context.Context, url string) ([]byte, error) {
			capturedArchiveURL = url
			return archive, nil
		},
	}

	pluginStore := &mocks.PluginStore{
		InstallFn: func(_ context.Context, _ domain.PluginManifest, _ []byte, _ string, _ bool) (domain.Plugin, error) {
			return domain.Plugin{ID: "abc", Status: domain.PluginStatusActive}, nil
		},
	}

	uc := plugin.NewInstallUseCase(pluginStore, registryMgr)
	_, err := uc.Install(context.Background(), "official", "my-tool", "1.0.0", "admin")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if !strings.Contains(capturedManifestURL, "1.0.0") {
		t.Errorf("manifest URL should contain requested version 1.0.0, got: %s", capturedManifestURL)
	}
	if strings.Contains(capturedManifestURL, "1.2.0") {
		t.Errorf("manifest URL must not contain latest version 1.2.0, got: %s", capturedManifestURL)
	}
	if !strings.Contains(capturedArchiveURL, "1.0.0") {
		t.Errorf("archive URL should contain requested version 1.0.0, got: %s", capturedArchiveURL)
	}
	if strings.Contains(capturedArchiveURL, "1.2.0") {
		t.Errorf("archive URL must not contain latest version 1.2.0, got: %s", capturedArchiveURL)
	}
}

func TestInstallUseCase_VersionPinned_NotAvailable(t *testing.T) {
	reg := domain.PluginRegistry{Name: "official", URL: "https://example.com", Trusted: true}

	registryMgr := &mocks.RegistryManager{
		ListFn: func(_ context.Context) ([]domain.PluginRegistry, error) {
			return []domain.PluginRegistry{reg}, nil
		},
		FetchIndexFn: func(_ context.Context, _ string, _ bool) (domain.RegistryIndex, error) {
			return domain.RegistryIndex{
				Registry: "official",
				Plugins: []domain.RegistryEntry{
					{
						Name:          "my-tool",
						LatestVersion: "1.2.0",
						Versions:      []string{"1.2.0"},
						ManifestURL:   "https://example.com/my-tool/1.2.0/plugin.yaml",
						DownloadURL:   "https://example.com/my-tool/1.2.0/my-tool-1.2.0.tar.gz",
					},
				},
			}, nil
		},
	}
	pluginStore := &mocks.PluginStore{}

	uc := plugin.NewInstallUseCase(pluginStore, registryMgr)
	_, err := uc.Install(context.Background(), "official", "my-tool", "0.9.0", "admin")
	if err == nil {
		t.Fatal("expected error for unavailable version, got nil")
	}
	if !strings.Contains(err.Error(), "0.9.0") || !strings.Contains(err.Error(), "not available") {
		t.Errorf("expected version-not-available error, got: %v", err)
	}
}

func TestInstallUseCase_StoreError_Propagated(t *testing.T) {
	reg := domain.PluginRegistry{Name: "official", URL: "https://example.com", Trusted: true}
	storeErr := errors.New("checksum mismatch")

	registryMgr := &mocks.RegistryManager{
		ListFn: func(_ context.Context) ([]domain.PluginRegistry, error) {
			return []domain.PluginRegistry{reg}, nil
		},
		FetchIndexFn: func(_ context.Context, _ string, _ bool) (domain.RegistryIndex, error) {
			return makeIndex(true), nil
		},
		FetchManifestFn: func(_ context.Context, _ string) (domain.PluginManifest, error) {
			return domain.PluginManifest{Name: "my-tool", Version: "1.0.0"}, nil
		},
		FetchArchiveFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("data"), nil
		},
	}
	pluginStore := &mocks.PluginStore{
		InstallFn: func(_ context.Context, _ domain.PluginManifest, _ []byte, _ string, _ bool) (domain.Plugin, error) {
			return domain.Plugin{}, storeErr
		},
	}

	uc := plugin.NewInstallUseCase(pluginStore, registryMgr)
	_, err := uc.Install(context.Background(), "official", "my-tool", "", "admin")
	if err == nil {
		t.Fatal("expected error from store, got nil")
	}
	if !errors.Is(err, storeErr) {
		t.Errorf("expected storeErr, got: %v", err)
	}
}
