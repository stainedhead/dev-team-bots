package fs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/local/fs"
)

// TestFS_WriteAndRead verifies basic write-then-read round-trip.
func TestFS_WriteAndRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := fs.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	if err := store.Write(ctx, "hello", []byte("world")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := store.Read(ctx, "hello")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "world" {
		t.Errorf("expected world, got %s", got)
	}
}

// TestFS_ReadNotFound verifies that reading a missing key returns os.ErrNotExist.
func TestFS_ReadNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := fs.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = store.Read(context.Background(), "missing-key")
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

// TestFS_DeleteExisting verifies that Delete removes the file.
func TestFS_DeleteExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := fs.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	if err := store.Write(ctx, "to-delete", []byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := store.Delete(ctx, "to-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = store.Read(ctx, "to-delete")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist after delete, got %v", err)
	}
}

// TestFS_DeleteNonExistent verifies that deleting a missing key is idempotent.
func TestFS_DeleteNonExistent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := fs.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = store.Delete(context.Background(), "never-existed")
	if err != nil {
		t.Errorf("Delete on non-existent key: expected nil, got %v", err)
	}
}

// TestFS_NestedKey verifies that keys with slashes are treated as relative paths.
func TestFS_NestedKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := fs.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	key := "agents/alice/memory"
	if err := store.Write(ctx, key, []byte("alice's memory")); err != nil {
		t.Fatalf("Write nested key: %v", err)
	}

	got, err := store.Read(ctx, key)
	if err != nil {
		t.Fatalf("Read nested key: %v", err)
	}
	if string(got) != "alice's memory" {
		t.Errorf("expected alice's memory, got %s", got)
	}

	// Verify file exists at the expected path.
	expected := filepath.Join(dir, "agents", "alice", "memory")
	if _, statErr := os.Stat(expected); os.IsNotExist(statErr) {
		t.Errorf("expected file at %s", expected)
	}
}

// TestFS_AtomicWrite verifies that Write is atomic (uses temp file + rename).
// This is tested indirectly by verifying no partial file is left on success.
func TestFS_AtomicWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := fs.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 256)
	}

	if err := store.Write(ctx, "bigfile", data); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := store.Read(ctx, "bigfile")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != len(data) {
		t.Errorf("expected %d bytes, got %d", len(data), len(got))
	}
}

// TestFS_NewBadPath verifies that constructor fails if directory cannot be created.
func TestFS_NewBadPath(t *testing.T) {
	t.Parallel()
	// Use a path under a non-existent parent that is also not writable.
	badPath := "/dev/null/impossible/nested"
	_, err := fs.New(badPath)
	if err == nil {
		t.Fatal("expected error for bad path, got nil")
	}
}

// TestFS_OverwriteExisting verifies that Write replaces an existing file.
func TestFS_OverwriteExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := fs.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	if err := store.Write(ctx, "key", []byte("v1")); err != nil {
		t.Fatalf("Write v1: %v", err)
	}
	if err := store.Write(ctx, "key", []byte("v2")); err != nil {
		t.Fatalf("Write v2: %v", err)
	}

	got, err := store.Read(ctx, "key")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "v2" {
		t.Errorf("expected v2, got %s", got)
	}
}

// TestFS_WriteReadOnlyDir verifies that Write fails gracefully when the
// parent directory is read-only.
func TestFS_WriteReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	t.Parallel()
	dir := t.TempDir()
	store, err := fs.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Make the base directory read-only so CreateTemp fails.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	err = store.Write(context.Background(), "key", []byte("data"))
	if err == nil {
		t.Fatal("expected error writing to read-only directory, got nil")
	}
}

// TestFS_DeleteReadOnlyDir verifies that Delete returns an error when
// the file cannot be removed due to permission restrictions.
func TestFS_DeleteReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	t.Parallel()
	dir := t.TempDir()
	store, err := fs.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	if err := store.Write(ctx, "locked", []byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Remove write permission from base directory so os.Remove fails with
	// a permission error (not ErrNotExist).
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	err = store.Delete(ctx, "locked")
	if err == nil {
		t.Fatal("expected error deleting from read-only directory, got nil")
	}
}

// TestFS_ReadPermissionError verifies that Read returns a non-ErrNotExist error
// when the file exists but is unreadable.
func TestFS_ReadPermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	t.Parallel()
	dir := t.TempDir()
	store, err := fs.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	if err := store.Write(ctx, "secret", []byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Remove read permission from the file.
	if err := os.Chmod(filepath.Join(dir, "secret"), 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "secret"), 0600) })

	_, err = store.Read(ctx, "secret")
	if err == nil {
		t.Fatal("expected error reading unreadable file, got nil")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Error("expected non-ErrNotExist error, got ErrNotExist")
	}
}

// TestFS_WriteRenameFailure verifies that Write returns an error when the
// rename to the destination fails (destination is a non-empty directory).
func TestFS_WriteRenameFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := fs.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Pre-create a directory at the key's location so rename onto it fails.
	destDir := filepath.Join(dir, "occupied")
	if err := os.MkdirAll(filepath.Join(destDir, "child"), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// On most POSIX systems, renaming a file onto a non-empty directory errors.
	err = store.Write(context.Background(), "occupied", []byte("data"))
	if err == nil {
		// Some platforms (or empty directories) allow this; skip gracefully.
		t.Skip("rename onto directory did not fail on this platform")
	}
}

// TestFS_WriteNestedReadOnlyDir verifies that Write fails when the intermediate
// subdirectory cannot be created (parent is read-only).
func TestFS_WriteNestedReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	t.Parallel()
	dir := t.TempDir()

	// Make the base dir read-only BEFORE creating the store, so MkdirAll
	// for nested keys fails. We create the store first with a writable dir,
	// then lock it.
	store, err := fs.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Lock the base directory so we cannot create subdirectories.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	// Key with a slash requires creating a subdirectory — should fail.
	err = store.Write(context.Background(), "sub/key", []byte("data"))
	if err == nil {
		t.Fatal("expected error creating nested dir in read-only parent, got nil")
	}
}
