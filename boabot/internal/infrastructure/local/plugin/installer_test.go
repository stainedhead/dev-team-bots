package plugin_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/plugin"
)

// buildTarGz builds a tar.gz from the given map of path→content.
func buildTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write content: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestExtract_Success(t *testing.T) {
	archive := buildTarGz(t, map[string]string{
		"run.sh":    "#!/bin/sh\necho hello",
		"README.md": "# Plugin",
	})
	checksum := sha256Hex(archive)

	manifest := domain.PluginManifest{
		Name:       "test-plugin",
		Entrypoint: "run.sh",
		Checksums:  map[string]string{"sha256": checksum},
	}

	installDir := t.TempDir()
	destPath, err := plugin.Extract(installDir, "test-id", manifest, archive)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	expectedPath := filepath.Join(installDir, "test-id")
	if destPath != expectedPath {
		t.Errorf("got path %q, want %q", destPath, expectedPath)
	}

	// Verify extracted files exist.
	if _, err := os.Stat(filepath.Join(destPath, "run.sh")); err != nil {
		t.Errorf("run.sh not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destPath, "README.md")); err != nil {
		t.Errorf("README.md not found: %v", err)
	}

	// Temp dir should be gone.
	tmpDir := filepath.Join(installDir, "test-id-tmp")
	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Error("temp dir should not exist after success")
	}
}

func TestExtract_ZipSlip(t *testing.T) {
	// Archive with a path that escapes the install dir.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{
		Name: "../escape/evil.sh",
		Mode: 0o644,
		Size: 4,
	}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte("evil"))
	_ = tw.Close()
	_ = gw.Close()
	archive := buf.Bytes()
	checksum := sha256Hex(archive)

	manifest := domain.PluginManifest{
		Name:      "zip-slip",
		Checksums: map[string]string{"sha256": checksum},
	}

	installDir := t.TempDir()
	_, err := plugin.Extract(installDir, "zip-id", manifest, archive)
	if err == nil {
		t.Fatal("expected zip-slip error, got nil")
	}
	if !strings.Contains(err.Error(), "zip-slip") && !strings.Contains(err.Error(), "outside") {
		t.Errorf("expected zip-slip error, got: %v", err)
	}

	// No files should be written outside the install dir.
	escapedFile := filepath.Join(filepath.Dir(installDir), "escape", "evil.sh")
	if _, statErr := os.Stat(escapedFile); !os.IsNotExist(statErr) {
		t.Error("zip-slip file was written outside install dir")
	}

	// Temp dir should be cleaned up.
	tmpDir := filepath.Join(installDir, "zip-id-tmp")
	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Error("temp dir should be removed after zip-slip error")
	}
}

func TestExtract_SizeLimit(t *testing.T) {
	// Build an archive that exceeds 50 MB when extracted.
	bigContent := strings.Repeat("x", 51*1024*1024) // 51 MB

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{
		Name: "big.bin",
		Mode: 0o644,
		Size: int64(len(bigContent)),
	}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(bigContent))
	_ = tw.Close()
	_ = gw.Close()
	archive := buf.Bytes()
	checksum := sha256Hex(archive)

	manifest := domain.PluginManifest{
		Name:      "big-plugin",
		Checksums: map[string]string{"sha256": checksum},
	}

	installDir := t.TempDir()
	_, err := plugin.Extract(installDir, "big-id", manifest, archive)
	if err == nil {
		t.Fatal("expected size limit error, got nil")
	}
	if !strings.Contains(err.Error(), "size") && !strings.Contains(err.Error(), "limit") && !strings.Contains(err.Error(), "50") {
		t.Errorf("expected size limit error, got: %v", err)
	}

	// Temp dir should be cleaned up.
	tmpDir := filepath.Join(installDir, "big-id-tmp")
	if _, statErr := os.Stat(tmpDir); !os.IsNotExist(statErr) {
		t.Error("temp dir should be removed after size limit error")
	}
}

func TestExtract_WrongChecksum(t *testing.T) {
	archive := buildTarGz(t, map[string]string{"run.sh": "#!/bin/sh"})

	manifest := domain.PluginManifest{
		Name:      "checksum-fail",
		Checksums: map[string]string{"sha256": fmt.Sprintf("%064d", 0)},
	}

	installDir := t.TempDir()
	_, err := plugin.Extract(installDir, "chk-id", manifest, archive)
	if err == nil {
		t.Fatal("expected checksum error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum") && !strings.Contains(err.Error(), "sha256") {
		t.Errorf("expected checksum error, got: %v", err)
	}

	// Temp dir should be cleaned up.
	tmpDir := filepath.Join(installDir, "chk-id-tmp")
	if _, statErr := os.Stat(tmpDir); !os.IsNotExist(statErr) {
		t.Error("temp dir should be removed after checksum failure")
	}
}
