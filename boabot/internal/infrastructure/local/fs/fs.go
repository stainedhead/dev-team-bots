// Package fs provides a local filesystem implementation of domain.MemoryStore.
// It is intended for single-binary operation without any cloud infrastructure.
//
// Files are stored under a configurable base directory. Keys may contain
// forward slashes, which are treated as path separators relative to the base
// directory. Writes are atomic: the payload is written to a temporary file in
// the same directory and then renamed into place.
package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// FS implements domain.MemoryStore using the local filesystem.
type FS struct {
	basePath string
}

// New constructs an FS that stores files under basePath.
// The directory (and any missing parents) is created with mode 0700 on
// construction. An error is returned immediately if the directory cannot be
// created.
func New(basePath string) (*FS, error) {
	if err := os.MkdirAll(basePath, 0700); err != nil {
		return nil, fmt.Errorf("local/fs: create base directory %q: %w", basePath, err)
	}
	return &FS{basePath: basePath}, nil
}

// Write stores value at basePath/<key>, creating any intermediate directories.
// The write is atomic: a temporary file is created in the same directory as
// the destination, data is written, and then the file is renamed into place.
func (f *FS) Write(_ context.Context, key string, value []byte) error {
	dest := filepath.Join(f.basePath, filepath.FromSlash(key))
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("local/fs: create parent directory for %q: %w", key, err)
	}

	// Use a pattern that gives us the temp name before writing.
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("local/fs: create temp file for %q: %w", key, err)
	}
	tmpName := tmp.Name()
	_ = tmp.Close() // close before WriteFile re-opens

	// WriteFile truncates and writes atomically; errors are wrapped below.
	if err := os.WriteFile(tmpName, value, 0600); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("local/fs: write temp file for %q: %w", key, err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("local/fs: rename temp to %q: %w", key, err)
	}
	return nil
}

// Read returns the contents of basePath/<key>.
// Returns a wrapped os.ErrNotExist if the file does not exist.
func (f *FS) Read(_ context.Context, key string) ([]byte, error) {
	dest := filepath.Join(f.basePath, filepath.FromSlash(key))
	data, err := os.ReadFile(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("local/fs: key %q not found: %w", key, os.ErrNotExist)
		}
		return nil, fmt.Errorf("local/fs: read %q: %w", key, err)
	}
	return data, nil
}

// Delete removes basePath/<key>. Returns nil if the file does not exist
// (idempotent).
func (f *FS) Delete(_ context.Context, key string) error {
	dest := filepath.Join(f.basePath, filepath.FromSlash(key))
	err := os.Remove(dest)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("local/fs: delete %q: %w", key, err)
	}
	return nil
}
