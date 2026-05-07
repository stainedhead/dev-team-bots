package httpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
	httpserver "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/http"
)

func makeTestIndex(registryName string) domain.RegistryIndex {
	return domain.RegistryIndex{
		Registry:    registryName,
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		Plugins: []domain.RegistryEntry{
			{
				Name:          "test-tool",
				Description:   "A test plugin",
				Author:        "tester",
				LatestVersion: "1.0.0",
				Versions:      []string{"1.0.0"},
				ManifestURL:   "https://example.com/manifest.yaml",
				DownloadURL:   "https://example.com/test-tool-1.0.0.tar.gz",
			},
		},
	}
}

func TestHTTPRegistryManager_FetchIndex_ReturnsIndex(t *testing.T) {
	idx := makeTestIndex("test-registry")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(idx)
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)
	result, err := mgr.FetchIndex(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("FetchIndex: %v", err)
	}
	if result.Registry != idx.Registry {
		t.Errorf("got registry %q, want %q", result.Registry, idx.Registry)
	}
	if len(result.Plugins) != 1 {
		t.Errorf("got %d plugins, want 1", len(result.Plugins))
	}
}

func TestHTTPRegistryManager_FetchIndex_UsesCache(t *testing.T) {
	callCount := 0
	idx := makeTestIndex("cached-registry")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(idx)
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)

	// First call should hit the server.
	_, err := mgr.FetchIndex(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("first FetchIndex: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 HTTP call, got %d", callCount)
	}

	// Second call without force should use cache.
	_, err = mgr.FetchIndex(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("second FetchIndex: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 HTTP call after cached fetch, got %d", callCount)
	}
}

func TestHTTPRegistryManager_FetchIndex_ForceBypassesCache(t *testing.T) {
	callCount := 0
	idx := makeTestIndex("forced-registry")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(idx)
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)

	_, _ = mgr.FetchIndex(context.Background(), srv.URL, false)
	_, err := mgr.FetchIndex(context.Background(), srv.URL, true)
	if err != nil {
		t.Fatalf("forced FetchIndex: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls with force=true, got %d", callCount)
	}
}

func TestHTTPRegistryManager_FetchIndex_Timeout(t *testing.T) {
	// Slow server that never responds within the timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client times out.
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
		http.Error(w, "timeout", http.StatusGatewayTimeout)
	}))
	defer srv.Close()

	mgr := httpserver.NewHTTPRegistryManagerWithTimeout(t.TempDir(), nil, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := mgr.FetchIndex(ctx, srv.URL, false)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestHTTPRegistryManager_Add_HTTPURLRejected(t *testing.T) {
	mgr := httpserver.NewHTTPRegistryManager(t.TempDir(), nil)
	reg := domain.PluginRegistry{
		Name:    "insecure",
		URL:     "http://example.com",
		Trusted: false,
	}
	err := mgr.Add(context.Background(), reg)
	if err == nil {
		t.Fatal("expected error for HTTP URL, got nil")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("expected error about HTTPS requirement, got: %v", err)
	}
}

func TestHTTPRegistryManager_Add_HTTPS_Persisted(t *testing.T) {
	dir := t.TempDir()
	mgr := httpserver.NewHTTPRegistryManager(dir, nil)

	reg := domain.PluginRegistry{
		Name:    "my-registry",
		URL:     "https://example.com/registry",
		Trusted: true,
	}
	if err := mgr.Add(context.Background(), reg); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Should be retrievable via List.
	regs, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, r := range regs {
		if r.Name == reg.Name {
			found = true
			if r.URL != reg.URL {
				t.Errorf("URL mismatch: got %q, want %q", r.URL, reg.URL)
			}
		}
	}
	if !found {
		t.Errorf("registry %q not found in list", reg.Name)
	}

	// Create a new manager from the same dir — registry should be persisted.
	mgr2 := httpserver.NewHTTPRegistryManager(dir, nil)
	regs2, err := mgr2.List(context.Background())
	if err != nil {
		t.Fatalf("List after reload: %v", err)
	}
	found2 := false
	for _, r := range regs2 {
		if r.Name == reg.Name {
			found2 = true
		}
	}
	if !found2 {
		t.Errorf("registry %q not persisted after reload", reg.Name)
	}
}
