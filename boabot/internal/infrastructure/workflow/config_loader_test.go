// Package workflow_test exercises the ConfigLoader infrastructure component.
package workflow_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	infrawf "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/workflow"
)

// minimalYAML is a valid workflow YAML with a two-step workflow.
const minimalYAML = `
workflows:
  - name: default
    steps:
      - name: backlog
        required_role: orchestrator
        next_step: done
        notify_on_entry: false
      - name: done
        required_role: ""
        next_step: ""
        notify_on_entry: true
`

// extendedYAML is a different valid YAML used to verify router replacement on Reload.
const extendedYAML = `
workflows:
  - name: feature
    steps:
      - name: start
        required_role: implementer
        next_step: ""
        notify_on_entry: false
`

// writeTempYAML writes content to a temporary file and returns its path.
// The caller is responsible for removing the file.
func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "workflow-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return f.Name()
}

// TestNewConfigLoader_ValidFile verifies that a valid YAML file produces a
// non-nil ConfigLoader with a non-nil Router.
func TestNewConfigLoader_ValidFile(t *testing.T) {
	path := writeTempYAML(t, minimalYAML)
	defer os.Remove(path)

	cl, err := infrawf.NewConfigLoader(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cl == nil {
		t.Fatal("expected non-nil ConfigLoader")
	}
	if cl.Router() == nil {
		t.Fatal("expected non-nil Router")
	}
}

// TestNewConfigLoader_InvalidYAML verifies that malformed YAML returns an error.
func TestNewConfigLoader_InvalidYAML(t *testing.T) {
	path := writeTempYAML(t, "workflows: [\x00unclosed")
	defer os.Remove(path)

	cl, err := infrawf.NewConfigLoader(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
	if cl != nil {
		t.Fatal("expected nil ConfigLoader on error")
	}
}

// TestNewConfigLoader_FileNotFound verifies that a missing file returns an error.
func TestNewConfigLoader_FileNotFound(t *testing.T) {
	cl, err := infrawf.NewConfigLoader("/no/such/file/workflow.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if cl != nil {
		t.Fatal("expected nil ConfigLoader on error")
	}
}

// TestReload_ReplacesRouter verifies that Reload atomically swaps the router so
// that Router() returns a router built from the new file contents.
func TestReload_ReplacesRouter(t *testing.T) {
	path := writeTempYAML(t, minimalYAML)
	defer os.Remove(path)

	cl, err := infrawf.NewConfigLoader(path)
	if err != nil {
		t.Fatalf("initial load: %v", err)
	}

	firstRouter := cl.Router()

	// Overwrite the file with different content.
	if err := os.WriteFile(path, []byte(extendedYAML), 0o600); err != nil {
		t.Fatalf("overwrite file: %v", err)
	}

	if err := cl.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	secondRouter := cl.Router()
	if secondRouter == firstRouter {
		t.Fatal("expected Router() to return a new router after Reload")
	}
}

// TestReload_InvalidFile_KeepsOldRouter verifies that a failed Reload leaves the
// existing router in place (atomic swap only happens on success).
func TestReload_InvalidFile_KeepsOldRouter(t *testing.T) {
	path := writeTempYAML(t, minimalYAML)
	defer os.Remove(path)

	cl, err := infrawf.NewConfigLoader(path)
	if err != nil {
		t.Fatalf("initial load: %v", err)
	}

	firstRouter := cl.Router()

	// Overwrite with invalid YAML (null byte causes parse error).
	if err := os.WriteFile(path, []byte("workflows: [\x00bad"), 0o600); err != nil {
		t.Fatalf("overwrite file: %v", err)
	}

	if err := cl.Reload(); err == nil {
		t.Fatal("expected error on bad reload, got nil")
	}

	// Router must still be the original one.
	if cl.Router() != firstRouter {
		t.Fatal("expected Router() to remain unchanged after failed Reload")
	}
}

// TestRouter_ThreadSafe calls Router() and Reload() concurrently to verify
// there are no data races (intended for -race flag).
func TestRouter_ThreadSafe(t *testing.T) {
	path := writeTempYAML(t, minimalYAML)
	defer os.Remove(path)

	cl, err := infrawf.NewConfigLoader(path)
	if err != nil {
		t.Fatalf("initial load: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			if n%2 == 0 {
				_ = cl.Router()
			} else {
				_ = cl.Reload()
			}
		}(i)
	}

	wg.Wait()
}

// TestWatchSIGHUP_CancelStops verifies that WatchSIGHUP returns when the
// context is cancelled (no hang).
func TestWatchSIGHUP_CancelStops(t *testing.T) {
	path := writeTempYAML(t, minimalYAML)
	defer os.Remove(path)

	cl, err := infrawf.NewConfigLoader(path)
	if err != nil {
		t.Fatalf("initial load: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		cl.WatchSIGHUP(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// success: goroutine exited promptly
	case <-time.After(2 * time.Second):
		t.Fatal("WatchSIGHUP did not return after context cancellation")
	}
}
