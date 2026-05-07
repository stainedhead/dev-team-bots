// Package plugin provides filesystem-backed plugin installation and storage.
package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

// MaxExtractSize is the maximum allowed total extraction size in bytes (50 MB).
const MaxExtractSize = 50 * 1024 * 1024

// Extract atomically extracts archive to a subdirectory of installDir named by id.
// It enforces zip-slip protection, 50 MB total size cap, and verifies sha256sum
// against manifest.Checksums["sha256"].
// On success, the final directory path is returned.
// On any failure, all written files are cleaned up.
func Extract(installDir, id string, manifest domain.PluginManifest, archive []byte) (string, error) {
	// Verify checksum first before touching the filesystem.
	expectedHex, ok := manifest.Checksums["sha256"]
	if !ok {
		return "", fmt.Errorf("plugin installer: no sha256 checksum in manifest")
	}
	sum := sha256.Sum256(archive)
	actualHex := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actualHex, expectedHex) {
		return "", fmt.Errorf("plugin installer: sha256 checksum mismatch: got %s, want %s", actualHex, expectedHex)
	}

	tmpDir := filepath.Join(installDir, id+"-tmp")
	finalDir := filepath.Join(installDir, id)

	// Clean up tmp dir on any error.
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", fmt.Errorf("plugin installer: create tmp dir: %w", err)
	}

	if err := extractArchive(archive, tmpDir); err != nil {
		cleanup()
		return "", err
	}

	if err := os.Rename(tmpDir, finalDir); err != nil {
		cleanup()
		return "", fmt.Errorf("plugin installer: rename tmp to final: %w", err)
	}

	return finalDir, nil
}

// extractArchive extracts a tar.gz archive into destDir, enforcing zip-slip
// protection and a 50 MB total size limit.
func extractArchive(archive []byte, destDir string) error {
	gzr, err := gzip.NewReader(newByteReader(archive))
	if err != nil {
		return fmt.Errorf("plugin installer: open gzip: %w", err)
	}
	defer func() { _ = gzr.Close() }()

	tr := tar.NewReader(gzr)
	var totalBytes int64

	cleanDestDir := filepath.Clean(destDir)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("plugin installer: read tar: %w", err)
		}

		// Zip-slip protection: join and clean, then check prefix.
		target := filepath.Join(destDir, hdr.Name)
		target = filepath.Clean(target)
		if target != cleanDestDir && !strings.HasPrefix(target, cleanDestDir+string(filepath.Separator)) {
			return fmt.Errorf("plugin installer: zip-slip detected: path %q is outside extraction dir", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			// Symlinks and hardlinks are explicitly prohibited: they are a well-known
			// zip-slip bypass vector and the spec forbids them.
			return fmt.Errorf("plugin installer: archive contains symlink/hardlink %q — not permitted", hdr.Name)
		case tar.TypeDir:
			if mkErr := os.MkdirAll(target, 0o755); mkErr != nil {
				return fmt.Errorf("plugin installer: create dir %q: %w", target, mkErr)
			}
		case tar.TypeReg:
			// Enforce size limit.
			if totalBytes+hdr.Size > MaxExtractSize {
				return fmt.Errorf("plugin installer: size limit exceeded: total extracted size would exceed %d bytes (50 MB limit)", MaxExtractSize)
			}

			if mkErr := os.MkdirAll(filepath.Dir(target), 0o755); mkErr != nil {
				return fmt.Errorf("plugin installer: create parent dir: %w", mkErr)
			}
			f, createErr := os.Create(target)
			if createErr != nil {
				return fmt.Errorf("plugin installer: create file %q: %w", target, createErr)
			}
			n, copyErr := io.Copy(f, tr)
			_ = f.Close()
			if copyErr != nil {
				return fmt.Errorf("plugin installer: write file %q: %w", target, copyErr)
			}
			totalBytes += n
		}
	}
	return nil
}

func newByteReader(b []byte) *bytes.Reader {
	return bytes.NewReader(b)
}
