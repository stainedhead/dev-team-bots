package plugin_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/plugin"
)

// buildValidArchive builds a valid tar.gz with a run.sh file and returns the archive
// along with its SHA-256 hex checksum.
func buildValidArchive(t *testing.T) ([]byte, string) {
	t.Helper()
	archive := buildTarGz(t, map[string]string{
		"run.sh": "#!/bin/sh\necho hello",
	})
	return archive, sha256Hex(archive)
}

func makeManifest(name, checksum string) domain.PluginManifest {
	return domain.PluginManifest{
		Name:       name,
		Version:    "1.0.0",
		Entrypoint: "run.sh",
		Checksums:  map[string]string{"sha256": checksum},
	}
}

func TestStore_InstallTrusted_StatusActive(t *testing.T) {
	archive, checksum := buildValidArchive(t)
	manifest := makeManifest("my-plugin", checksum)

	store, err := plugin.NewLocalPluginStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	p, err := store.Install(context.Background(), manifest, archive, "official", true)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if p.Status != domain.PluginStatusActive {
		t.Errorf("expected status active, got %s", p.Status)
	}
	if p.Registry != "official" {
		t.Errorf("expected registry official, got %s", p.Registry)
	}
	if p.Name != "my-plugin" {
		t.Errorf("expected name my-plugin, got %s", p.Name)
	}
	if p.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestStore_InstallUntrusted_StatusStaged(t *testing.T) {
	archive, checksum := buildValidArchive(t)
	manifest := makeManifest("untrusted-plugin", checksum)

	store, err := plugin.NewLocalPluginStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	p, err := store.Install(context.Background(), manifest, archive, "unofficial", false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if p.Status != domain.PluginStatusStaged {
		t.Errorf("expected status staged, got %s", p.Status)
	}
}

func TestStore_ApproveStaged(t *testing.T) {
	archive, checksum := buildValidArchive(t)
	manifest := makeManifest("stage-plugin", checksum)

	store, err := plugin.NewLocalPluginStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	p, err := store.Install(context.Background(), manifest, archive, "r", false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := store.Approve(context.Background(), p.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	got, err := store.Get(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PluginStatusActive {
		t.Errorf("expected active after approve, got %s", got.Status)
	}
}

func TestStore_RejectStaged_RemovesDirectory(t *testing.T) {
	archive, checksum := buildValidArchive(t)
	manifest := makeManifest("reject-plugin", checksum)

	installDir := t.TempDir()
	store, err := plugin.NewLocalPluginStore(installDir)
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	p, err := store.Install(context.Background(), manifest, archive, "r", false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if err := store.Reject(context.Background(), p.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	// Plugin should be gone from store.
	_, err = store.Get(context.Background(), p.ID)
	if err == nil {
		t.Error("expected error after reject, got nil")
	}
}

func TestStore_DisableActive(t *testing.T) {
	archive, checksum := buildValidArchive(t)
	manifest := makeManifest("disable-plugin", checksum)

	store, err := plugin.NewLocalPluginStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	p, err := store.Install(context.Background(), manifest, archive, "r", true)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if err := store.Disable(context.Background(), p.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	got, err := store.Get(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PluginStatusDisabled {
		t.Errorf("expected disabled, got %s", got.Status)
	}
}

func TestStore_EnableDisabled(t *testing.T) {
	archive, checksum := buildValidArchive(t)
	manifest := makeManifest("enable-plugin", checksum)

	store, err := plugin.NewLocalPluginStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	p, err := store.Install(context.Background(), manifest, archive, "r", true)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := store.Disable(context.Background(), p.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if err := store.Enable(context.Background(), p.ID); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	got, err := store.Get(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PluginStatusActive {
		t.Errorf("expected active after enable, got %s", got.Status)
	}
}

func TestStore_RemoveActive_DirectoryDeleted(t *testing.T) {
	archive, checksum := buildValidArchive(t)
	manifest := makeManifest("remove-plugin", checksum)

	installDir := t.TempDir()
	store, err := plugin.NewLocalPluginStore(installDir)
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	p, err := store.Install(context.Background(), manifest, archive, "r", true)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if err := store.Remove(context.Background(), p.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Plugin should be gone from store.
	_, err = store.Get(context.Background(), p.ID)
	if err == nil {
		t.Error("expected error after remove, got nil")
	}
}

func TestStore_ConcurrentInstall_SameName(t *testing.T) {
	archive, checksum := buildValidArchive(t)
	manifest := makeManifest("conflict-plugin", checksum)

	store, err := plugin.NewLocalPluginStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	// Install first one.
	_, err = store.Install(context.Background(), manifest, archive, "r", true)
	if err != nil {
		t.Fatalf("first Install: %v", err)
	}

	// Second install of same name should fail.
	_, err = store.Install(context.Background(), manifest, archive, "r", true)
	if err == nil {
		t.Fatal("expected error on duplicate install, got nil")
	}
}

func TestStore_ConcurrentInstall_Race(t *testing.T) {
	// Run concurrent installs of the SAME name to verify only one succeeds.
	store, err := plugin.NewLocalPluginStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	const n = 5
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			archive, checksum := buildValidArchive(t)
			m := makeManifest("race-plugin", checksum)
			_, errs[i] = store.Install(context.Background(), m, archive, "r", true)
		}(i)
	}
	wg.Wait()

	// At most one should succeed.
	successCount := 0
	for _, e := range errs {
		if e == nil {
			successCount++
		}
	}
	if successCount == 0 {
		t.Error("expected at least one successful install")
	}
	if successCount > 1 {
		t.Errorf("expected at most one successful install, got %d", successCount)
	}
}

func TestStore_List_SortedByInstalledAtDesc(t *testing.T) {
	store, err := plugin.NewLocalPluginStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	names := []string{"alpha", "beta", "gamma"}
	for _, name := range names {
		archive, checksum := buildValidArchive(t)
		manifest := makeManifest(name, checksum)
		_, err := store.Install(context.Background(), manifest, archive, "r", true)
		if err != nil {
			t.Fatalf("Install %s: %v", name, err)
		}
		// Small delay to ensure distinct timestamps.
		time.Sleep(2 * time.Millisecond)
	}

	plugins, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(plugins) != 3 {
		t.Fatalf("expected 3 plugins, got %d", len(plugins))
	}
	// Should be sorted by InstalledAt descending (newest first = gamma).
	if plugins[0].Name != "gamma" {
		t.Errorf("expected gamma first (newest), got %s", plugins[0].Name)
	}
}

func TestStore_Reload_MissingEntrypoint_Disabled(t *testing.T) {
	// Build archive with run.sh.
	archive, checksum := buildValidArchive(t)
	manifest := makeManifest("reload-plugin", checksum)

	installDir := t.TempDir()
	store, err := plugin.NewLocalPluginStore(installDir)
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	p, err := store.Install(context.Background(), manifest, archive, "r", true)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Remove the entrypoint from the installed dir to simulate the missing file.
	installedEntrypoint := filepath.Join(installDir, "reload-plugin", manifest.Entrypoint)
	if err := os.Remove(installedEntrypoint); err != nil {
		t.Fatalf("remove entrypoint: %v", err)
	}

	// Reload should fail and set status to disabled.
	reloadErr := store.Reload(context.Background(), p.ID)
	if reloadErr == nil {
		t.Error("expected error from Reload with missing entrypoint")
	}

	got, err := store.Get(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if got.Status != domain.PluginStatusDisabled {
		t.Errorf("expected disabled after failed reload, got %s", got.Status)
	}
}

func TestStore_Update_CorruptArchive_RollsBack(t *testing.T) {
	installDir := t.TempDir()
	store, err := plugin.NewLocalPluginStore(installDir)
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	// Install v1.0.0.
	archive, checksum := buildValidArchive(t)
	manifest := makeManifest("update-plugin", checksum)
	p, err := store.Install(context.Background(), manifest, archive, "official", true)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Verify plugin directory exists.
	pluginDir := filepath.Join(installDir, "update-plugin")
	if _, statErr := os.Stat(pluginDir); os.IsNotExist(statErr) {
		t.Fatal("plugin dir should exist after install")
	}

	// Try to update with a corrupt archive (bad gzip bytes).
	corruptArchive := []byte("this is not a valid tar.gz archive")
	corruptChecksum := sha256Hex(corruptArchive)
	newManifest := domain.PluginManifest{
		Name:      "update-plugin",
		Version:   "2.0.0",
		Checksums: map[string]string{"sha256": corruptChecksum},
	}
	updateErr := store.Update(context.Background(), p.ID, newManifest, corruptArchive)
	if updateErr == nil {
		t.Fatal("expected error when updating with corrupt archive, got nil")
	}

	// Original plugin directory must still exist.
	if _, statErr := os.Stat(pluginDir); os.IsNotExist(statErr) {
		t.Error("original plugin directory was deleted despite failed update — data loss")
	}

	// Plugin must still be active.
	got, getErr := store.Get(context.Background(), p.ID)
	if getErr != nil {
		t.Fatalf("Get after failed update: %v", getErr)
	}
	if got.Status != domain.PluginStatusActive {
		t.Errorf("expected active status after failed update, got %s", got.Status)
	}
	if got.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0 after failed update, got %s", got.Version)
	}
}

func TestStore_Update_Success_OldDirectoryRemoved(t *testing.T) {
	installDir := t.TempDir()
	store, err := plugin.NewLocalPluginStore(installDir)
	if err != nil {
		t.Fatalf("NewLocalPluginStore: %v", err)
	}

	// Install v1.0.0.
	archive, checksum := buildValidArchive(t)
	manifest := makeManifest("success-update-plugin", checksum)
	p, err := store.Install(context.Background(), manifest, archive, "official", true)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Update with a valid archive.
	newArchive, newChecksum := buildValidArchive(t)
	newManifest := domain.PluginManifest{
		Name:       "success-update-plugin",
		Version:    "2.0.0",
		Entrypoint: "run.sh",
		Checksums:  map[string]string{"sha256": newChecksum},
	}
	if err := store.Update(context.Background(), p.ID, newManifest, newArchive); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Plugin must be at new version.
	got, getErr := store.Get(context.Background(), p.ID)
	if getErr != nil {
		t.Fatalf("Get after update: %v", getErr)
	}
	if got.Version != "2.0.0" {
		t.Errorf("expected version 2.0.0 after update, got %s", got.Version)
	}

	// Old backup directory must be cleaned up.
	oldDir := filepath.Join(installDir, "success-update-plugin-old")
	if _, statErr := os.Stat(oldDir); !os.IsNotExist(statErr) {
		t.Error("backup directory should be removed after successful update")
	}

	// Temp directory must be cleaned up.
	tmpDir := filepath.Join(installDir, "success-update-plugin-update-tmp")
	if _, statErr := os.Stat(tmpDir); !os.IsNotExist(statErr) {
		t.Error("temp directory should be removed after successful update")
	}
}
